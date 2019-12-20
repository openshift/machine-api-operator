/*
Copyright 2018 The Kubernetes Authors.

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

package vsphere

import (
	"context"
	"crypto/tls"
	"testing"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	vsphereapi "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere/session"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func initSimulator(t *testing.T) (*simulator.Model, *session.Session, *simulator.Server) {
	model := simulator.VPX()
	model.Host = 0
	err := model.Create()
	if err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)

	server := model.Service.NewServer()
	pass, _ := server.URL.User.Password()

	authSession, err := session.GetOrCreate(
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

func TestClone(t *testing.T) {
	model, _, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	password, _ := server.URL.User.Password()
	namespace := "test"
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUser:     []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testCases := []struct {
		testCase    string
		machine     func(t *testing.T) *machinev1.Machine
		expectError bool
		cloneVM     bool
	}{
		{
			testCase: "clone machine from default values",
			machine: func(t *testing.T) *machinev1.Machine {
				providerSpec := vsphereapi.VSphereMachineProviderSpec{
					CredentialsSecret: &corev1.LocalObjectReference{
						Name: "test",
					},
					Workspace: &vsphereapi.Workspace{
						Server: server.URL.Host,
					},
				}
				raw, err := vsphereapi.RawExtensionFromProviderSpec(&providerSpec)
				if err != nil {
					t.Fatal(err)
				}
				return &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "1",
						Name:      "defaultFolder",
						Namespace: namespace,
					},
					Spec: machinev1.MachineSpec{
						ProviderSpec: machinev1.ProviderSpec{
							Value: raw,
						},
					},
				}
			},
			expectError: false,
			cloneVM:     true,
		},
		{
			testCase: "does not clone machine if folder does not exist",
			machine: func(t *testing.T) *machinev1.Machine {
				providerSpec := vsphereapi.VSphereMachineProviderSpec{
					CredentialsSecret: &corev1.LocalObjectReference{
						Name: "test",
					},
					Workspace: &vsphereapi.Workspace{
						Server: server.URL.Host,
						Folder: "does-not-exists",
					},
				}
				raw, err := vsphereapi.RawExtensionFromProviderSpec(&providerSpec)
				if err != nil {
					t.Fatal(err)
				}
				return &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "2",
						Name:      "missingFolder",
						Namespace: namespace,
					},
					Spec: machinev1.MachineSpec{
						ProviderSpec: machinev1.ProviderSpec{
							Value: raw,
						},
					},
				}
			},
			expectError: true,
		},
		{
			testCase: "clone machine in specific folder",
			machine: func(t *testing.T) *machinev1.Machine {
				providerSpec := vsphereapi.VSphereMachineProviderSpec{
					CredentialsSecret: &corev1.LocalObjectReference{
						Name: "test",
					},

					Workspace: &vsphereapi.Workspace{
						Server: server.URL.Host,
						Folder: "custom-folder",
					},
				}
				raw, err := vsphereapi.RawExtensionFromProviderSpec(&providerSpec)
				if err != nil {
					t.Fatal(err)
				}
				return &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						UID:       "3",
						Name:      "customFolder",
						Namespace: namespace,
					},
					Spec: machinev1.MachineSpec{
						ProviderSpec: machinev1.ProviderSpec{
							Value: raw,
						},
					},
				}
			},
			expectError: false,
			cloneVM:     true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			machineScope, err := newMachineScope(machineScopeParams{
				client:  fake.NewFakeClientWithScheme(scheme.Scheme, &credentialsSecret),
				Context: context.TODO(),
				machine: tc.machine(t),
			})
			if err != nil {
				t.Fatal(err)
			}
			machineScope.providerSpec.Template = vm.Name

			if err := clone(machineScope); (err != nil) != tc.expectError {
				t.Errorf("Got: %v. Expected: %v", err, tc.expectError)
			}
			if tc.cloneVM {
				model.Machine++
			}
			if model.Machine != model.Count().Machine {
				t.Errorf("Unexpected number of machines. Expected: %v, got: %v", model.Machine, model.Count().Machine)
			}
		})
	}
}

func TestGetPowerState(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	ref := simulatorVM.VirtualMachine.Reference()

	testCases := []struct {
		testCase string
		vm       func(t *testing.T) *virtualMachine
		expected types.VirtualMachinePowerState
	}{
		{
			testCase: "powered off",
			vm: func(t *testing.T) *virtualMachine {
				vm := &virtualMachine{
					Context: context.TODO(),
					Obj:     object.NewVirtualMachine(session.Client.Client, ref),
					Ref:     ref,
				}
				_, err := vm.Obj.PowerOff(vm.Context)
				if err != nil {
					t.Fatal(err)
				}
				return vm
			},
			expected: types.VirtualMachinePowerStatePoweredOff,
		},
		{
			testCase: "powered on",
			vm: func(t *testing.T) *virtualMachine {
				vm := &virtualMachine{
					Context: context.TODO(),
					Obj:     object.NewVirtualMachine(session.Client.Client, ref),
					Ref:     ref,
				}
				_, err := vm.Obj.PowerOn(vm.Context)
				if err != nil {
					t.Fatal(err)
				}
				return vm
			},

			expected: types.VirtualMachinePowerStatePoweredOn,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			got, err := tc.vm(t).getPowerState()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("Got: %v, expected: %v", got, tc.expected)
			}
		})
	}
}

// TODO TestCreate()
// TODO TestUpdate()
// TODO TestExist()
// TODO TestReconcileNetwork()
// TODO TestReconcileMachineWithCloudState()
// TODO TestGetNetworkStatus()
