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
	maometrics "github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/soap"
	"k8s.io/klog/v2"
)

// sessionValidationTTL is how long we trust a cached session without
// re-running the SOAP SessionIsActive + REST session check pair.
// vCenter SOAP/REST sessions live far longer than this (hours), and
// every operation on a dead session returns an auth error, forcing a
// re-login on the next GetOrCreate anyway.
var sessionValidationTTL = 5 * time.Minute

// sessionEntry wraps a cached Session with the time it was last validated.
type sessionEntry struct {
	session       Session
	lastValidated time.Time
}

// sessionCache is a cache of sessions keyed by server, username, and datacenter.
var sessionCache = map[string]sessionEntry{}
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

	cachingTagManager *CachingTagsManager // per-session caching wrapper around TagManager
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

// dropCachedSession evicts the cached session for key so the next GetOrCreate
// creates a fresh one. It is invoked from the transport layers when an
// operation reports the session was invalidated (auth failure), so a dead
// session is not reused during the validation TTL.
//
// GetOrCreate holds sessionMU across its SOAP/REST validation and login calls,
// so those calls can trigger this very callback while the lock is already held.
// TryLock (not Lock) avoids the resulting deadlock: when the lock is held,
// GetOrCreate is already re-validating and replacing the session, so skipping
// the delete here is safe.
func dropCachedSession(key string) {
	if !sessionMU.TryLock() {
		return
	}
	defer sessionMU.Unlock()
	delete(sessionCache, key)
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(
	ctx context.Context,
	server, datacenter, username, password string, insecure bool) (*Session, error) {

	sessionMU.Lock()
	defer sessionMU.Unlock()

	sessionKey := server + username + datacenter
	if entry, ok := sessionCache[sessionKey]; ok {
		if time.Since(entry.lastValidated) < sessionValidationTTL {
			klog.V(4).Infof("Reusing cached vSphere session within validation TTL")
			return &entry.session, nil
		}
		session := entry.session

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
			entry.lastValidated = time.Now()
			sessionCache[sessionKey] = entry
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
	soapTransport := &metricRoundTripper{
		roundTripper: client.RoundTripper,
		histogram:    maometrics.VsphereRequestDurationSeconds,
	}
	client.RoundTripper = soapTransport
	// Set up user agent before login for being able to track mapi component in vcenter sessions list
	client.UserAgent = "machineAPIvSphereProvider"
	if err := client.Login(ctx, url.UserPassword(username, password)); err != nil {
		return nil, fmt.Errorf("unable to login to vCenter: %w", err)
	}

	session := Session{
		Client: client,
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
	restTransport := &metricHTTPTransport{
		roundTripper: restClient.Transport,
		histogram:    maometrics.VsphereRequestDurationSeconds,
	}
	restClient.Transport = restTransport
	if err := restClient.Login(ctx, url.UserPassword(username, password)); err != nil {
		// Cleanup SOAP session on REST login failure
		if logoutErr := client.Logout(ctx); logoutErr != nil {
			klog.Errorf("Failed to logout SOAP session after REST login failure: %v", logoutErr)
		}
		return nil, fmt.Errorf("unable to login REST client to vCenter: %w", err)
	}
	session.TagManager = tags.NewManager(restClient)
	session.cachingTagManager = newTagsCachingClient(session.TagManager)

	// Arm session-invalidation handling only once the session is fully
	// established. The login and datacenter calls above ran while sessionMU was
	// held, so the callbacks must be nil during them: a re-entrant
	// dropCachedSession would otherwise try to take the lock we already hold.
	soapTransport.onSessionInvalid = func() { dropCachedSession(sessionKey) }
	restTransport.onSessionInvalid = func() { dropCachedSession(sessionKey) }

	// Cache the session.
	sessionCache[sessionKey] = sessionEntry{session: session, lastValidated: time.Now()}

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

// GetCachingTagsManager returns the per-session CachingTagsManager that wraps
// the cached TagManager. It is created once when the session is created,
// so no new vCenter login/logout happens on access.
func (s *Session) GetCachingTagsManager() *CachingTagsManager {
	return s.cachingTagManager
}
