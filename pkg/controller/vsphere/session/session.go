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
	"net/url"
	"sync"
	"time"

	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

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
// This implementation is inspired by cluster-api-provider-vsphere's session caching pattern
// to avoid excessive vCenter login/logout cycles for REST API operations.
// Reference: https://github.com/kubernetes-sigs/cluster-api-provider-vsphere/blob/main/pkg/session/session.go
type Session struct {
	*govmomi.Client
	Finder     *find.Finder
	Datacenter *object.Datacenter
	TagManager *tags.Manager

	username string
	password string

	sessionKey string
}

func newClientWithTimeout(ctx context.Context, u *url.URL, insecure bool, timeout time.Duration) (*govmomi.Client, error) {
	clientCreateCtx, clientCreateCtxCancel := context.WithTimeout(ctx, timeout)
	defer clientCreateCtxCancel()
	// It makes call to vcenter during new client creation, so pass context with timeout there.
	client, err := govmomi.NewClient(clientCreateCtx, u, insecure)
	if err != nil {
		return nil, err
	}
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
		// Check both SOAP and REST session validity before reusing cached session.
		// This prevents reusing sessions where one connection type has expired.
		// Pattern adapted from cluster-api-provider-vsphere:
		// https://github.com/kubernetes-sigs/cluster-api-provider-vsphere/blob/main/pkg/session/session.go#L132-L149
		sessionActive, err := session.SessionManager.SessionIsActive(ctx)
		if err != nil {
			klog.Errorf("Error performing SOAP session check request to vSphere: %v", err)
		}

		var restSessionActive bool
		if session.TagManager != nil {
			restSession, err := session.TagManager.Session(ctx)
			if err != nil {
				klog.Errorf("Error performing REST session check request to vSphere: %v", err)
			}
			restSessionActive = restSession != nil
		}

		if sessionActive && restSessionActive {
			klog.V(3).Infof("Found active cached vSphere session with valid SOAP and REST connections")
			return &session, nil
		}

		// If either session is invalid, logout both to clean up
		if session.TagManager != nil {
			klog.Infof("Logging out inactive REST session")
			if err := session.TagManager.Logout(ctx); err != nil {
				klog.Errorf("Failed to logout REST session: %v", err)
			}
		}
		klog.Infof("Logging out inactive SOAP session")
		if err := session.Client.Logout(ctx); err != nil {
			klog.Errorf("Failed to logout SOAP session: %v", err)
		}
	}
	klog.Infof("No existing vCenter soap session found, creating new session")

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

	// Create and cache REST client for tag operations.
	// This prevents creating a new REST session on every tag operation.
	// Pattern adapted from cluster-api-provider-vsphere:
	// https://github.com/kubernetes-sigs/cluster-api-provider-vsphere/blob/main/pkg/session/session.go#L196-L205
	restClient := rest.NewClient(session.Client.Client)
	if err := restClient.Login(ctx, url.UserPassword(username, password)); err != nil {
		// Cleanup SOAP session on REST login failure
		if logoutErr := client.Logout(ctx); logoutErr != nil {
			klog.Errorf("Failed to logout SOAP session after REST login failure: %v", logoutErr)
		}
		return nil, fmt.Errorf("unable to login REST client to vCenter: %w", err)
	}
	session.TagManager = tags.NewManager(restClient)

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

// GetCachingTagsManager returns a CachingTagsManager that wraps the cached TagManager.
// This replaces the previous WithCachingTagsManager pattern which created new sessions
// on every call. The returned manager uses the session's cached REST client.
func (s *Session) GetCachingTagsManager() *CachingTagsManager {
	return newTagsCachingClient(s.TagManager, s.sessionKey)
}

// WithRestClient is deprecated. Use s.TagManager directly instead.
// This function is maintained for backward compatibility but creates excessive
// vCenter login/logout cycles. Migration path: replace callback pattern with
// direct access to s.TagManager.
//
// Deprecated: Use s.TagManager for direct REST client access.
func (s *Session) WithRestClient(ctx context.Context, f func(c *rest.Client) error) error {
	klog.Warning("WithRestClient is deprecated and causes excessive vCenter logouts. Use s.TagManager directly instead.")
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

// WithCachingTagsManager is deprecated. Use s.GetCachingTagsManager() instead.
// This function is maintained for backward compatibility but creates excessive
// vCenter login/logout cycles. Migration path: replace callback pattern with
// direct call to s.GetCachingTagsManager().
//
// Deprecated: Use s.GetCachingTagsManager() for cached tag manager access.
func (s *Session) WithCachingTagsManager(ctx context.Context, f func(m *CachingTagsManager) error) error {
	klog.Warning("WithCachingTagsManager is deprecated and causes excessive vCenter logouts. Use s.GetCachingTagsManager() instead.")
	c := rest.NewClient(s.Client.Client)

	klog.Infof("No existing vCenter rest session found, creating new session")
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
