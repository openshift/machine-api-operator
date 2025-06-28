/*
Copyright 2019 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/vmware/govmomi/session"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	"github.com/vmware/govmomi/vim25/xml"

	"github.com/google/uuid"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/soap"
	"k8s.io/klog/v2"
)

var sessionCache = map[string]Session{}
var sessionMU sync.Mutex

const (
	managedObjectTypeTask = "Task"
	clientTimeout         = 15 * time.Second
)

// Session is a vSphere session with a configured Finder.
type Session struct {
	*govmomi.Client
	Finder     *find.Finder
	Datacenter *object.Datacenter

	username string
	password string

	sessionKey string
}

// #### Start: This section was added by cursor

// SOAPResponse represents the structure of SOAP responses
type SOAPResponse struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		XMLName xml.Name `xml:"Body"`
		Fault   *struct {
			XMLName xml.Name `xml:"Fault"`
			Code    struct {
				XMLName xml.Name `xml:"faultcode"`
				Value   string   `xml:",chardata"`
			} `xml:"faultcode"`
			Reason struct {
				XMLName xml.Name `xml:"faultstring"`
				Value   string   `xml:",chardata"`
			} `xml:"faultstring"`
			Detail struct {
				XMLName xml.Name `xml:"detail"`
				Content string   `xml:",chardata"`
			} `xml:"detail"`
		} `xml:"Fault,omitempty"`
	} `xml:"Body"`
}

// CustomTransport wraps the default transport to intercept SOAP responses
type CustomTransport struct {
	http.RoundTripper
}

func (t *CustomTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Call the original transport
	resp, err := t.RoundTripper.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp, err
	}
	resp.Body.Close()

	// Check if it's a SOAP response
	if strings.Contains(string(body), "<?xml") && strings.Contains(string(body), "Envelope") {
		// Parse SOAP response for privilege errors
		var soapResp SOAPResponse
		if err := xml.Unmarshal(body, &soapResp); err == nil {
			if soapResp.Body.Fault != nil {
				klog.Error("=== PRIVILEGE ERROR DETECTED ===")
				klog.Errorf("Fault Code: %s\n", soapResp.Body.Fault.Code.Value)
				klog.Errorf("Fault Reason: %s\n", soapResp.Body.Fault.Reason.Value)
				klog.Errorf("Fault Detail: %s\n", soapResp.Body.Fault.Detail.Content)
				klog.Error("================================\n")
			}
		}

		// Check for privilege-related error messages in the response
		bodyStr := string(body)
		privilegeKeywords := []string{
			"privilege", "permission", "access denied", "unauthorized", "forbidden",
			"NoPermission", "InvalidLogin", "InvalidPrivilege",
		}
		for _, keyword := range privilegeKeywords {
			if strings.Contains(strings.ToLower(bodyStr), strings.ToLower(keyword)) {
				klog.Errorf("=== POTENTIAL PRIVILEGE ISSUE DETECTED (keyword: %s) ===\n", keyword)
				klog.Error("Response contains privilege-related content\n")
				klog.Error("==================================================")
				break
			}
		}
		fmt.Println("=== End SOAP Response ===\n")
	}

	// Create a new response with the body
	resp.Body = io.NopCloser(strings.NewReader(string(body)))
	return resp, nil
}

// #### End: This section was added by cursor

func newClientWithTimeout(ctx context.Context, u *url.URL, insecure bool, timeout time.Duration) (*govmomi.Client, error) {
	clientCreateCtx, clientCreateCtxCancel := context.WithTimeout(ctx, timeout)
	defer clientCreateCtxCancel()
	// It makes call to vcenter during new client creation, so pass context with timeout there.
	/*
		client, err := govmomi.NewClient(clientCreateCtx, u, insecure)
		if err != nil {
			return nil, err
		}

	*/

	customTransport := &CustomTransport{
		RoundTripper: http.DefaultTransport,
	}

	soapClient := soap.NewClient(u, insecure)
	soapClient.Transport = customTransport

	// Create vim25 client
	vimClient, err := vim25.NewClient(clientCreateCtx, soapClient)
	if err != nil {
		log.Fatalf("Failed to create vim25 client: %v", err)
	}

	// Create govmomi client
	client := &govmomi.Client{
		Client:         vimClient,
		SessionManager: session.NewManager(vimClient),
	}

	// Login to vSphere
	err = client.Login(ctx, u.User)
	if err != nil {
		log.Fatalf("Failed to login to vSphere: %v", err)
	}
	defer client.Logout(clientCreateCtx)

	// Create SOAP client with custom transport
	//client.Transport = customTransport

	client.Timeout = timeout
	return client, nil
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(
	ctx context.Context,
	server, datacenter, username, password string, insecure bool) (*Session, error) {

	sessionMU.Lock()
	defer sessionMU.Unlock()

	sessionKey := server + username + datacenter
	if session, ok := sessionCache[sessionKey]; ok {
		sessionActive, err := session.SessionManager.SessionIsActive(ctx)
		if err != nil {
			klog.Errorf("Error performing session check request to vSphere: %v", err)
		}
		if sessionActive {
			return &session, nil
		}
	}
	klog.Infof("No existing vCenter session found, creating new session")

	soapURL, err := soap.ParseURL(server)
	if err != nil {
		return nil, fmt.Errorf("error parsing vSphere URL %q: %w", server, err)
	}
	if soapURL == nil {
		return nil, fmt.Errorf("error parsing vSphere URL %q", server)
	}

	// Set user to nil there for prevent login during client creation.
	// See https://github.com/vmware/govmomi/blob/master/client.go#L91
	soapURL.User = nil
	client, err := newClientWithTimeout(ctx, soapURL, insecure, clientTimeout)
	if err != nil {
		return nil, fmt.Errorf("error setting up new vSphere SOAP client: %w", err)
	}
	// Set up user agent before login for being able to track mapi component in vcenter sessions list
	client.UserAgent = "machineAPIvSphereProvider"
	if err := client.Login(ctx, url.UserPassword(username, password)); err != nil {
		return nil, fmt.Errorf("unable to login to vCenter: %w", err)
	}

	session := Session{
		Client:     client,
		username:   username,
		password:   password,
		sessionKey: sessionKey,
	}

	session.Finder = find.NewFinder(session.Client.Client, false)

	dc, err := session.Finder.DatacenterOrDefault(ctx, datacenter)
	if err != nil {
		return nil, fmt.Errorf("unable to find datacenter %q: %w", datacenter, err)
	}
	session.Datacenter = dc
	session.Finder.SetDatacenter(dc)

	// Cache the session.
	sessionCache[sessionKey] = session

	return &session, nil
}

func (s *Session) FindVM(ctx context.Context, UUID, name string) (*object.VirtualMachine, error) {
	if !isValidUUID(UUID) {
		klog.V(3).Infof("Invalid UUID for VM %q: %s, trying to find by name", name, UUID)
		return s.findVMByName(ctx, name)
	}
	klog.V(3).Infof("Find template by instance uuid: %s", UUID)
	ref, err := s.FindRefByInstanceUUID(ctx, UUID)
	if ref != nil && err == nil {
		return object.NewVirtualMachine(s.Client.Client, ref.Reference()), nil
	}
	if err != nil {
		klog.V(3).Infof("Instance not found by UUID: %s, trying to find by name %q", err, name)
	}
	return s.findVMByName(ctx, name)
}

// FindByInstanceUUID finds an object by its instance UUID.
func (s *Session) FindRefByInstanceUUID(ctx context.Context, UUID string) (object.Reference, error) {
	return s.findRefByUUID(ctx, UUID, true)
}

func (s *Session) findRefByUUID(ctx context.Context, UUID string, findByInstanceUUID bool) (object.Reference, error) {
	if s.Client == nil {
		return nil, errors.New("vSphere client is not initialized")
	}
	si := object.NewSearchIndex(s.Client.Client)
	ref, err := si.FindByUuid(ctx, s.Datacenter, UUID, true, &findByInstanceUUID)
	if err != nil {
		return nil, fmt.Errorf("error finding object by uuid %q: %w", UUID, err)
	}
	return ref, nil
}

func (s *Session) findVMByName(ctx context.Context, ID string) (*object.VirtualMachine, error) {
	tpl, err := s.Finder.VirtualMachine(ctx, ID)
	if err != nil {
		if isNotFound(err) {
			return nil, err
		}
		return nil, fmt.Errorf("unable to find template by name %q: %w", ID, err)
	}
	return tpl, nil
}

func isNotFound(err error) bool {
	switch err.(type) {
	case *find.NotFoundError:
		return true
	default:
		return false
	}
}

func isValidUUID(str string) bool {
	_, err := uuid.Parse(str)
	return err == nil
}

func (s *Session) GetTask(ctx context.Context, taskRef string) (*mo.Task, error) {
	if taskRef == "" {
		return nil, errors.New("taskRef can't be empty")
	}
	var obj mo.Task
	moRef := types.ManagedObjectReference{
		Type:  managedObjectTypeTask,
		Value: taskRef,
	}
	if err := s.RetrieveOne(ctx, moRef, []string{"info"}, &obj); err != nil {
		return nil, err
	}
	return &obj, nil
}

func (s *Session) WithRestClient(ctx context.Context, f func(c *rest.Client) error) error {
	c := rest.NewClient(s.Client.Client)

	user := url.UserPassword(s.username, s.password)
	if err := c.Login(ctx, user); err != nil {
		return err
	}

	defer func() {
		if err := c.Logout(ctx); err != nil {
			klog.Errorf("Failed to logout: %v", err)
		}
	}()

	return f(c)
}

func (s *Session) WithCachingTagsManager(ctx context.Context, f func(m *CachingTagsManager) error) error {
	c := rest.NewClient(s.Client.Client)

	user := url.UserPassword(s.username, s.password)
	if err := c.Login(ctx, user); err != nil {
		return err
	}

	defer func() {
		if err := c.Logout(ctx); err != nil {
			klog.Errorf("Failed to logout: %v", err)
		}
	}()

	m := newTagsCachingClient(tags.NewManager(c), s.sessionKey)

	return f(m)
}
