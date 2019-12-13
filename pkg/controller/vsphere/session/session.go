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
	"net/url"
	"sync"

	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/soap"
	"k8s.io/klog"
)

var sessionCache = map[string]Session{}
var sessionMU sync.Mutex

// Session is a vSphere session with a configured Finder.
type Session struct {
	*govmomi.Client
	Finder     *find.Finder
	Datacenter *object.Datacenter
}

// GetOrCreate gets a cached session or creates a new one if one does not
// already exist.
func GetOrCreate(
	ctx context.Context,
	server, datacenter, username, password string) (*Session, error) {

	sessionMU.Lock()
	defer sessionMU.Unlock()

	sessionKey := server + username + datacenter
	if session, ok := sessionCache[sessionKey]; ok {
		if ok, _ := session.SessionManager.SessionIsActive(ctx); ok {
			return &session, nil
		}
	}

	soapURL, err := soap.ParseURL(server)
	if err != nil {
		return nil, errors.Wrapf(err, "error parsing vSphere URL %q", server)
	}
	if soapURL == nil {
		return nil, errors.Errorf("error parsing vSphere URL %q", server)
	}

	soapURL.User = url.UserPassword(username, password)

	// TODO: drop insecure flag
	client, err := govmomi.NewClient(ctx, soapURL, true)
	if err != nil {
		return nil, errors.Wrapf(err, "error setting up new vSphere SOAP client")
	}

	session := Session{Client: client}
	session.UserAgent = "machineAPIvSphereProvider"
	session.Finder = find.NewFinder(session.Client.Client, false)

	dc, err := session.Finder.DatacenterOrDefault(ctx, datacenter)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find datacenter %q", datacenter)
	}
	session.Datacenter = dc
	session.Finder.SetDatacenter(dc)

	// Cache the session.
	sessionCache[sessionKey] = session

	return &session, nil
}

func (s *Session) FindVM(ctx context.Context, ID string) (*object.VirtualMachine, error) {
	if !isValidUUID(ID) {
		klog.V(3).Infof("Find template by instance uuid %v", ID)
		ref, err := s.FindRefByInstanceUUID(ctx, ID)
		if err != nil {
			return nil, errors.Wrap(err, "error querying template by instance UUID")
		}
		if ref != nil {
			return object.NewVirtualMachine(s.Client.Client, ref.Reference()), nil
		}
	}

	klog.V(3).Infof("Find template by name %v", ID)
	return s.findVMByName(ctx, ID)
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
		return nil, errors.Wrapf(err, "error finding object by uuid %q", UUID)
	}
	return ref, nil
}

func (s *Session) findVMByName(ctx context.Context, ID string) (*object.VirtualMachine, error) {
	tpl, err := s.Finder.VirtualMachine(ctx, ID)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to find template by name %q", ID)
	}
	return tpl, nil
}

func isValidUUID(str string) bool {
	_, err := uuid.Parse(str)
	return err == nil
}
