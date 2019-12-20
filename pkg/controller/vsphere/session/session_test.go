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
	"crypto/tls"
	"testing"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
)

func initSimulator(t *testing.T) (*simulator.Model, *Session, *simulator.Server) {
	model := simulator.VPX()
	model.Host = 0
	err := model.Create()
	if err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)

	server := model.Service.NewServer()
	pass, _ := server.URL.User.Password()

	authSession, err := GetOrCreate(
		context.TODO(),
		server.URL.Host, "",
		server.URL.User.Username(), pass)
	if err != nil {
		t.Fatal(err)
	}
	// create folder
	folders, err := authSession.Datacenter.Folders(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	_, err = folders.VmFolder.CreateFolder(context.TODO(), "custom-folder")
	if err != nil {
		t.Fatal(err)
	}

	return model, authSession, server
}

func TestFindVMByName(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)

	testCases := []struct {
		testCase    string
		ID          string
		expectError bool
		found       bool
	}{
		{
			testCase:    "found",
			ID:          simulatorVM.Name,
			expectError: false,
			found:       true,
		},
		{
			testCase:    "not found",
			ID:          "notFound",
			expectError: true,
			found:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			vm, err := session.findVMByName(context.TODO(), tc.ID)
			if (err != nil) != tc.expectError {
				t.Errorf("Expected error: %v, got: %v", tc.expectError, err)
			}
			if (vm != nil) != tc.found {
				t.Errorf("Expected found: %v, got: %v", tc.found, vm)
			}
		})
	}
}

func TestFindRefByInstanceUUID(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	instanceUUID := "instanceUUID"
	simulatorVM.Config.InstanceUuid = instanceUUID

	testCases := []struct {
		testCase string
		ID       string
		found    bool
	}{
		{
			testCase: "found",
			ID:       instanceUUID,
			found:    true,
		},
		{
			testCase: "not found",
			ID:       "notFound",
			found:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			ref, err := session.FindRefByInstanceUUID(context.TODO(), tc.ID)
			if err != nil {
				t.Fatal(err)
			}
			if (ref != nil) != tc.found {
				t.Errorf("Expected found: %v, got: %v", tc.found, ref)
			}
		})
	}
}

func TestFindVM(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	instanceUUID := "instanceUUID"
	simulatorVM.Config.InstanceUuid = instanceUUID

	testCases := []struct {
		testCase    string
		ID          string
		expectError bool
		found       bool
	}{
		{
			testCase: "found by instanceUUID",
			ID:       instanceUUID,
			found:    true,
		},
		{
			testCase: "found by name",
			ID:       simulatorVM.Name,
			found:    true,
		},
		{
			testCase:    "not found",
			ID:          "notFound",
			expectError: true,
			found:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			ref, err := session.FindVM(context.TODO(), tc.ID)
			if (err != nil) != tc.expectError {
				t.Errorf("Expected error: %v, got: %v", tc.expectError, err)
			}
			if (ref != nil) != tc.found {
				t.Errorf("Expected found: %v, got: %v", tc.found, ref)
			}
		})
	}
}

func TestGetTask(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	obj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	// Validate VM is powered on
	if obj.Runtime.PowerState != "poweredOn" {
		t.Fatal(obj.Runtime.PowerState)
	}
	vm := object.NewVirtualMachine(session.Client.Client, obj.Reference())
	task, err := vm.PowerOff(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		testCase    string
		taskRef     string
		expectError bool
		found       bool
	}{
		{
			testCase:    "existing taskRef",
			taskRef:     task.Reference().Value,
			expectError: false,
			found:       true,
		},
		{
			testCase:    "empty string",
			taskRef:     "",
			expectError: false,
			found:       false,
		},
		{
			testCase:    "non existing taskRef",
			taskRef:     "foo",
			expectError: true,
			found:       false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			ref, err := session.GetTask(context.TODO(), tc.taskRef)
			if (err != nil) != tc.expectError {
				t.Errorf("Expected error: %v, got: %v", tc.expectError, err)
			}
			if (ref != nil) != tc.found {
				t.Errorf("Expected found: %v, got: %v", tc.found, ref)
			}
		})
	}
}
