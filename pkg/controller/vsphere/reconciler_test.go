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
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"reflect"
	"testing"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	vsphereapi "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere/session"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	_ "github.com/vmware/govmomi/vapi/simulator"
)

const poweredOnState = "poweredOn"

func init() {
	// Add types to scheme
	configv1.AddToScheme(scheme.Scheme)
}

type simulatorModelOption func(m *simulator.Model)

func initSimulator(t *testing.T, opts ...simulatorModelOption) (*simulator.Model, *session.Session, *simulator.Server) {
	model := simulator.VPX()
	model.Host = 0

	for _, opt := range opts {
		opt(model)
	}
	err := model.Create()
	if err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)
	model.Service.RegisterEndpoints = true

	server := model.Service.NewServer()
	pass, _ := server.URL.User.Password()

	authSession, err := session.GetOrCreate(
		context.TODO(),
		server.URL.Host, "",
		server.URL.User.Username(), pass, true)
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
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	credentialsSecretUsername := fmt.Sprintf("%s.username", server.URL.Host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", server.URL.Host)

	password, _ := server.URL.User.Password()
	namespace := "test"
	defaultSizeGiB := int32(5)
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)

	machine := object.NewVirtualMachine(session.Client.Client, vm.Reference())
	devices, err := machine.Device(context.TODO())
	if err != nil {
		t.Fatalf("Failed to obtain vm devices: %v", err)
	}
	disks := devices.SelectByType((*types.VirtualDisk)(nil))
	if len(disks) < 1 {
		t.Fatal("Unable to find attached disk for resize")
	}
	disk := disks[0].(*types.VirtualDisk)
	disk.CapacityInKB = int64(defaultSizeGiB) * 1024 * 1024 // GiB
	if err := machine.EditDevice(context.TODO(), disk); err != nil {
		t.Fatalf("Can't resize disk for specified size")
	}

	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	userDataSecretName := "vsphere-ignition"
	userDataSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userDataSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			userDataSecretKey: []byte("{}"),
		},
	}

	testCases := []struct {
		testCase              string
		cloneVM               bool
		expectedError         error
		setupFailureCondition func() error
		providerSpec          vsphereapi.VSphereMachineProviderSpec
		machineName           string
	}{
		{
			testCase: "clone machine from default values",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  defaultSizeGiB,
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			cloneVM:     true,
			machineName: "test0",
		},
		{
			testCase: "clone machine in specific folder",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server: server.URL.Host,
					Folder: "custom-folder",
				},
				DiskGiB:  defaultSizeGiB,
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			cloneVM:     true,
			machineName: "test1",
		},
		{
			testCase: "clone machine and increase disk",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  defaultSizeGiB + 1,
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			cloneVM:     true,
			machineName: "test0",
		},
		{
			testCase: "fail on disc resize down",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  defaultSizeGiB - 1,
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("error getting disk spec for \"\": can't resize template disk down, initial capacity is larger: 5242880KiB > 4194304KiB"),
		},
		{
			testCase: "fail on invalid resource pool",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server:       server.URL.Host,
					ResourcePool: "invalid",
				},
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("resource pool not found, specify valid value"),
		},
		{
			testCase: "fail on multiple resource pools",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server:       server.URL.Host,
					ResourcePool: "/DC0/host/DC0_C0/Resources/...",
				},
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("multiple resource pools found, specify one in config"),
			setupFailureCondition: func() error {
				// Create resource pools
				defaultResourcePool, err := session.Finder.ResourcePool(context.Background(), "/DC0/host/DC0_C0/Resources")
				if err != nil {
					return err
				}
				spec := types.DefaultResourceConfigSpec()
				_, err = defaultResourcePool.Create(context.Background(), "resourcePool1", spec)
				if err != nil {
					return err
				}
				_, err = defaultResourcePool.Create(context.Background(), "resourcePool2", spec)
				if err != nil {
					return err
				}
				return nil
			},
		},
		{
			testCase:      "fail on invalid folder",
			expectedError: errors.New("folder not found, specify valid value"),
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server: server.URL.Host,
					Folder: "invalid",
				},
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
		},
		{
			testCase: "fail on multiple folders",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server: server.URL.Host,
					Folder: "/DC0/vm/...",
				},
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("multiple folders found, specify one in config"),
			setupFailureCondition: func() error {
				// Create folders
				defaultFolder, err := session.Finder.Folder(context.Background(), "/DC0/vm")
				if err != nil {
					return err
				}
				_, err = defaultFolder.CreateFolder(context.Background(), "folder1")
				if err != nil {
					return err
				}
				_, err = defaultFolder.CreateFolder(context.Background(), "folder2")
				if err != nil {
					return err
				}
				return nil
			},
		},
		{
			testCase: "fail on invalid datastore",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server:    server.URL.Host,
					Datastore: "invalid",
				},
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("datastore not found, specify valid value"),
		},
		{
			testCase: "fail on multiple datastores",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &vsphereapi.Workspace{
					Server:    server.URL.Host,
					Datastore: "/DC0/...",
				},
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("multiple datastores found, specify one in config"),
			setupFailureCondition: func() error {
				// Create datastores
				hostSystem, err := session.Finder.HostSystem(context.Background(), "/DC0/host/DC0_C0/DC0_C0_H0")
				if err != nil {
					return err
				}
				dss, err := hostSystem.ConfigManager().DatastoreSystem(context.Background())
				if err != nil {
					return err
				}
				dir, err := ioutil.TempDir("", fmt.Sprintf("tmpdir"))
				if err != nil {
					return err
				}
				_, err = dss.CreateLocalDatastore(context.Background(), "datastore1", dir)
				if err != nil {
					return err
				}
				_, err = dss.CreateLocalDatastore(context.Background(), "datastore2", dir)
				if err != nil {
					return err
				}
				return nil
			},
		},
		{
			testCase: "fail on invalid template",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				Template: "invalid",
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: errors.New("template not found, specify valid value"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			if tc.setupFailureCondition != nil {
				if err := tc.setupFailureCondition(); err != nil {
					t.Fatal(err)
				}
			}

			machineScope := machineScope{
				Context: context.TODO(),
				machine: &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
						Labels: map[string]string{
							machinev1.MachineClusterIDLabel: "CLUSTERID",
						},
					},
				},
				providerSpec:   &tc.providerSpec,
				session:        session,
				providerStatus: &vsphereapi.VSphereMachineProviderStatus{},
				client:         fake.NewFakeClientWithScheme(scheme.Scheme, &credentialsSecret, &userDataSecret),
			}

			if tc.machineName != "" {
				machineScope.machine.Name = tc.machineName
			}

			taskRef, err := clone(&machineScope)

			if tc.expectedError != nil {
				if taskRef != "" {
					t.Fatalf("task reference was expected to be empty, got: %s", taskRef)
				}
				if err == nil {
					t.Fatal("clone() was expected to return error")
				}
				if err.Error() != tc.expectedError.Error() {
					t.Fatalf("expected: %v, got %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("clone() was not expected to return error: %v", err)
				}
			}

			if tc.cloneVM {
				if taskRef == "" {
					t.Fatal("task reference was not expected to be empty")
				}
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

func TestTaskIsFinished(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	obj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	// Validate VM is powered on
	if obj.Runtime.PowerState != poweredOnState {
		t.Fatal(obj.Runtime.PowerState)
	}
	vm := object.NewVirtualMachine(session.Client.Client, obj.Reference())
	task, err := vm.PowerOff(context.TODO())
	if err != nil {
		t.Fatal(err)
	}
	var moTask mo.Task
	moRef := types.ManagedObjectReference{
		Type:  "Task",
		Value: task.Reference().Value,
	}
	if err := session.RetrieveOne(context.TODO(), moRef, []string{"info"}, &moTask); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		testCase    string
		moTask      func() *mo.Task
		expectError bool
		finished    bool
	}{
		{
			testCase: "existing taskRef",
			moTask: func() *mo.Task {
				return &moTask
			},
			expectError: false,
			finished:    true,
		},
		{
			testCase: "nil task",
			moTask: func() *mo.Task {
				return nil
			},
			expectError: false,
			finished:    true,
		},
		{
			testCase: "task succeeded is finished",
			moTask: func() *mo.Task {
				moTask.Info.State = types.TaskInfoStateSuccess
				return &moTask
			},
			expectError: false,
			finished:    true,
		},
		{
			testCase: "task error is finished",
			moTask: func() *mo.Task {
				moTask.Info.State = types.TaskInfoStateError
				moTask.Info.Error = &types.LocalizedMethodFault{}
				return &moTask
			},
			expectError: true,
			finished:    true,
		},
		{
			testCase: "task running is not finished",
			moTask: func() *mo.Task {
				moTask.Info.State = types.TaskInfoStateRunning
				return &moTask
			},
			expectError: false,
			finished:    false,
		},
		{
			testCase: "task with unknown state errors",
			moTask: func() *mo.Task {
				moTask.Info.State = types.TaskInfoState("unknown")
				return &moTask
			},
			expectError: true,
			finished:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			finished, err := taskIsFinished(tc.moTask())
			if (err != nil) != tc.expectError {
				t.Errorf("Expected error: %v, got: %v", tc.expectError, err)
			}
			if finished != tc.finished {
				t.Errorf("Expected finished: %v, got: %v", tc.finished, finished)
			}
		})
	}
}

func TestGetNetworkDevices(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	objVM := object.NewVirtualMachine(session.Client.Client, managedObj.Reference())

	devices, err := objVM.Device(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	// checking network has been created by default
	_, err = session.Finder.Network(context.TODO(), "VM Network")
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		testCase     string
		providerSpec *vsphereapi.VSphereMachineProviderSpec
		expected     func(gotDevices []types.BaseVirtualDeviceConfigSpec) bool
	}{
		{
			testCase:     "no Network",
			providerSpec: &vsphereapi.VSphereMachineProviderSpec{},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec) bool {
				if len(gotDevices) != 1 {
					return false
				}
				if gotDevices[0].GetVirtualDeviceConfigSpec().Operation != types.VirtualDeviceConfigSpecOperationRemove {
					return false
				}
				return true
			},
		},
		{
			testCase: "one Network",
			providerSpec: &vsphereapi.VSphereMachineProviderSpec{
				Network: vsphereapi.NetworkSpec{
					Devices: []vsphereapi.NetworkDeviceSpec{
						{
							NetworkName: "VM Network",
						},
					},
				},
			},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec) bool {
				if len(gotDevices) != 2 {
					return false
				}
				if gotDevices[0].GetVirtualDeviceConfigSpec().Operation != types.VirtualDeviceConfigSpecOperationRemove {
					return false
				}
				if gotDevices[1].GetVirtualDeviceConfigSpec().Operation != types.VirtualDeviceConfigSpecOperationAdd {
					return false
				}
				return true
			},
		},
	}
	// TODO: verify GetVirtualDeviceConfigSpec().Device values

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			machineScope := &machineScope{
				Context:      context.TODO(),
				providerSpec: tc.providerSpec,
				session:      session,
			}
			networkDevices, err := getNetworkDevices(machineScope, devices)
			if err != nil {
				t.Fatal(err)
			}
			if !tc.expected(networkDevices) {
				t.Errorf("Got unexpected networkDevices len (%v) or operations (%v)",
					len(networkDevices),
					printOperations(networkDevices))
			}
		})
	}
}

func TestGetDiskSpec(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	objVM := object.NewVirtualMachine(session.Client.Client, managedObj.Reference())

	testCases := []struct {
		name                 string
		expectedError        error
		devices              func() object.VirtualDeviceList
		diskSize             int32
		expectedCapacityInKB int64
	}{
		{
			name: "Succefully get disk spec with disk size 1",
			devices: func() object.VirtualDeviceList {
				devices, err := objVM.Device(context.TODO())
				if err != nil {
					t.Fatal(err)
				}
				return devices
			},
			diskSize:             1,
			expectedCapacityInKB: 1048576,
		},
		{
			name: "Succefully get disk spec with disk size 3",
			devices: func() object.VirtualDeviceList {
				devices, err := objVM.Device(context.TODO())
				if err != nil {
					t.Fatal(err)
				}
				return devices
			},
			diskSize:             3,
			expectedCapacityInKB: 3145728,
		},
		{
			name: "Fail on invalid disk count",
			devices: func() object.VirtualDeviceList {
				devices, err := objVM.Device(context.TODO())
				if err != nil {
					t.Fatal(err)
				}
				devices = append(devices, &types.VirtualDisk{})
				return devices
			},
			expectedError:        errors.New("invalid disk count: 2"),
			diskSize:             1,
			expectedCapacityInKB: 1048576,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			machineScope := &machineScope{
				Context: context.TODO(),
				providerSpec: &vsphereapi.VSphereMachineProviderSpec{
					DiskGiB: tc.diskSize,
				},
				session: session,
			}
			diskSpec, err := getDiskSpec(machineScope, tc.devices())

			if tc.expectedError == nil {
				if err != nil {
					t.Fatal(err)
				}

				virtualDeviceConfigSpec := diskSpec.(*types.VirtualDeviceConfigSpec)
				disk := virtualDeviceConfigSpec.Device.(*types.VirtualDisk)

				if disk.CapacityInKB != tc.expectedCapacityInKB {
					t.Fatalf("Expected disk capacity to be %v, got %v", disk.CapacityInKB, tc.expectedCapacityInKB)
				}

				if diskSpec.GetVirtualDeviceConfigSpec().Operation != types.VirtualDeviceConfigSpecOperationEdit {
					t.Fatalf("Expected operation type to be %s, got %v", types.VirtualDeviceConfigSpecOperationEdit, diskSpec.GetVirtualDeviceConfigSpec().Operation)
				}
			} else {
				if err == nil {
					t.Fatal("getDiskSpec was expected to return an error")
				}
				if tc.expectedError.Error() != err.Error() {
					t.Fatalf("Expected error %v , got %v", tc.expectedError, err)
				}
			}
		})
	}
}

func printOperations(networkDevices []types.BaseVirtualDeviceConfigSpec) string {
	var output string
	for i := range networkDevices {
		output += fmt.Sprintf("device: %v has operation: %v, ", i, string(networkDevices[i].GetVirtualDeviceConfigSpec().Operation))
	}
	return output
}

func TestGetNetworkStatusList(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	defaultFakeIPs := []string{"127.0.0.1"}
	managedObj.Guest.Net[0].IpAddress = defaultFakeIPs
	managedObjRef := object.NewVirtualMachine(session.Client.Client, managedObj.Reference()).Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     object.NewVirtualMachine(session.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	defaultFakeMAC := "00:0c:29:33:34:38"
	expectedNetworkStatusList := []NetworkStatus{
		{
			IPAddrs:   defaultFakeIPs,
			Connected: true,
			MACAddr:   defaultFakeMAC,
		},
	}

	// validations
	networkStatusList, err := vm.getNetworkStatusList(session.Client.Client)
	if err != nil {
		t.Fatal(err)
	}

	if len(networkStatusList) != 1 {
		t.Errorf("Expected networkStatusList len to be 1, got %v", len(networkStatusList))
	}
	if !reflect.DeepEqual(networkStatusList, expectedNetworkStatusList) {
		t.Errorf("Expected: %v, got: %v", networkStatusList, expectedNetworkStatusList)
	}
	// TODO: add more cases by adding network devices to the NewVirtualMachine() object
}

func TestReconcileNetwork(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObj.Guest.Net[0].IpAddress = []string{"127.0.0.1"}
	managedObjRef := object.NewVirtualMachine(session.Client.Client, managedObj.Reference()).Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     object.NewVirtualMachine(session.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	vmName, err := vm.Obj.ObjectName(vm.Context)
	if err != nil {
		t.Fatal(err)
	}

	expectedAddresses := []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalIP,
			Address: "127.0.0.1",
		},
		{
			Type:    corev1.NodeInternalDNS,
			Address: vmName,
		},
	}
	r := &Reconciler{
		machineScope: &machineScope{
			Context: context.TODO(),
			session: session,
			machine: &machinev1.Machine{
				Status: machinev1.MachineStatus{},
			},
			providerSpec: &vsphereapi.VSphereMachineProviderSpec{
				Network: vsphereapi.NetworkSpec{
					Devices: []vsphereapi.NetworkDeviceSpec{
						{
							NetworkName: "dummy",
						},
					},
				},
			},
		},
	}
	if err := r.reconcileNetwork(vm); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(expectedAddresses, r.machineScope.machine.Status.Addresses) {
		t.Errorf("Expected: %v, got: %v", expectedAddresses, r.machineScope.machine.Status.Addresses)
	}
	// TODO: add more cases by adding network devices to the NewVirtualMachine() object
}

func TestReconcileTags(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := object.NewVirtualMachine(session.Client.Client, managedObj.Reference()).Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     object.NewVirtualMachine(session.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	tagName := "CLUSTERID"

	testCases := []struct {
		name          string
		expectedError bool
		attachTag     bool
		testCondition func()
	}{
		{
			name:          "Don't fail when tag doesn't exist",
			expectedError: false,
		},
		{
			name:          "Successfully attach a tag",
			expectedError: false,
			attachTag:     true,
			testCondition: func() {
				createTagAndCategory(session, "CLUSTERID_CATEGORY", tagName)
			},
		},
		{
			name:          "Fail on vSphere API error",
			expectedError: false,
			testCondition: func() {
				session.Logout(context.Background())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.testCondition != nil {
				tc.testCondition()
			}

			err := vm.reconcileTags(context.TODO(), session, &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "machine",
					Labels: map[string]string{machinev1.MachineClusterIDLabel: tagName},
				},
			})

			if tc.expectedError {
				if err == nil {
					t.Fatal("Expected error")
				}
			} else {
				if err != nil {
					t.Fatalf("Not expected error %v", err)
				}

				if tc.attachTag {
					if err := session.WithRestClient(context.TODO(), func(c *rest.Client) error {
						tagsMgr := tags.NewManager(c)

						tags, err := tagsMgr.GetAttachedTags(context.TODO(), managedObjRef)
						if err != nil {
							return err
						}

						if tags[0].Name != tagName {
							t.Fatalf("Expected tag %s, got %s", tagName, tags[0].Name)
						}

						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
			}

		})
	}
}

func TestCheckAttachedTag(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := object.NewVirtualMachine(session.Client.Client, managedObj.Reference()).Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     object.NewVirtualMachine(session.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	tagName := "CLUSTERID"
	nonAttachedTagName := "nonAttachedTag"

	if err := session.WithRestClient(context.TODO(), func(c *rest.Client) error {
		tagsMgr := tags.NewManager(c)

		id, err := tagsMgr.CreateCategory(context.TODO(), &tags.Category{
			AssociableTypes: []string{"VirtualMachine"},
			Cardinality:     "SINGLE",
			Name:            "CLUSTERID_CATEGORY",
		})
		if err != nil {
			return err
		}

		_, err = tagsMgr.CreateTag(context.TODO(), &tags.Tag{
			CategoryID: id,
			Name:       tagName,
		})
		if err != nil {
			return err
		}

		if err := tagsMgr.AttachTag(context.TODO(), tagName, vm.Ref); err != nil {
			return err
		}

		_, err = tagsMgr.CreateTag(context.TODO(), &tags.Tag{
			CategoryID: id,
			Name:       nonAttachedTagName,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		name    string
		findTag bool
		tagName string
	}{
		{
			name:    "Successfully find a tag",
			findTag: true,
			tagName: tagName,
		},
		{
			name:    "Return true if a tag doesn't exist",
			tagName: "non existent tag",
			findTag: true,
		},
		{
			name:    "Fail to find a tag",
			tagName: nonAttachedTagName,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if err := session.WithRestClient(context.TODO(), func(c *rest.Client) error {
				tagsMgr := tags.NewManager(c)

				attached, err := vm.checkAttachedTag(context.TODO(), tc.tagName, tagsMgr)
				if err != nil {
					return fmt.Errorf("Not expected error %v", err)
				}

				if attached != tc.findTag {
					return fmt.Errorf("Failed to find attached tag: got %v, expected %v", attached, tc.findTag)
				}

				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestIgnitionConfig(t *testing.T) {
	optionsForData := func(data []byte) []types.BaseOptionValue {
		return []types.BaseOptionValue{
			&types.OptionValue{
				Key:   GuestInfoIgnitionData,
				Value: base64.StdEncoding.EncodeToString(data),
			},
			&types.OptionValue{
				Key:   GuestInfoIgnitionEncoding,
				Value: "base64",
			},
		}
	}

	testCases := []struct {
		testCase string
		data     []byte
		expected []types.BaseOptionValue
	}{
		{
			testCase: "nil data",
			data:     nil,
			expected: nil,
		},
		{
			testCase: "empty data",
			data:     []byte(""),
			expected: nil,
		},
		{
			testCase: "plain-text data",
			data:     []byte("{}"),
			expected: optionsForData([]byte("{}")),
		},
		{
			testCase: "base64 data",
			data:     []byte("e30="),
			expected: optionsForData([]byte("{}")),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			options := IgnitionConfig(tc.data)

			if len(options) != len(tc.expected) {
				t.Errorf("Got: %q, Want: %q", options, tc.expected)
			}

			for i := range options {
				got := options[i].GetOptionValue()
				want := tc.expected[i].GetOptionValue()

				if got.Key != want.Key || got.Value != want.Value {
					t.Errorf("%q does not match expected %q", want, got)
				}
			}
		})
	}
}

func TestReconcileProviderID(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	objectVM := object.NewVirtualMachine(session.Client.Client, managedObj.Reference())
	managedObjRef := objectVM.Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     objectVM,
		Ref:     managedObjRef,
	}

	r := &Reconciler{
		machineScope: &machineScope{
			Context: context.TODO(),
			session: session,
			machine: &machinev1.Machine{
				Status: machinev1.MachineStatus{},
			},
		},
	}

	if err := r.reconcileProviderID(vm); err != nil {
		t.Errorf("unexpected error")
	}

	if *r.machine.Spec.ProviderID != providerIDPrefix+vm.Obj.UUID(context.TODO()) {
		t.Errorf("failed to match expected providerID pattern, expected: %v, got: %v", providerIDPrefix+vm.Obj.UUID(context.TODO()), *r.machine.Spec.ProviderID)
	}
}

func TestConvertUUIDToProviderID(t *testing.T) {
	validUUID := "f7c371d6-2003-5a48-9859-3bc9a8b08908"
	testCases := []struct {
		testCase string
		UUID     string
		expected string
	}{
		{
			testCase: "valid",
			UUID:     validUUID,
			expected: providerIDPrefix + validUUID,
		},
		{
			testCase: "invalid",
			UUID:     "f7c371d6",
			expected: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			got, err := convertUUIDToProviderID(tc.UUID)
			if got != tc.expected {
				t.Errorf("expected: %v, got: %v", tc.expected, got)
			}
			if tc.expected == "" && err == nil {
				t.Errorf("expected error, got %v", err)
			}
		})
	}
}

func TestDelete(t *testing.T) {
	withMoreVms := func(vmsCount int) simulatorModelOption {
		return func(m *simulator.Model) {
			m.Machine = vmsCount
		}
	}

	model, _, server := initSimulator(t, withMoreVms(5))
	defer model.Remove()
	defer server.Close()
	host, port, err := net.SplitHostPort(server.URL.Host)
	if err != nil {
		t.Fatal(err)
	}

	credentialsSecretUsername := fmt.Sprintf("%s.username", host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", host)

	password, _ := server.URL.User.Password()
	namespace := "test"

	instanceUUID := "a5764857-ae35-34dc-8f25-a9c9e73aa898"

	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testConfig := fmt.Sprintf(testConfigFmt, port)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: openshiftConfigNamespace,
		},
		Data: map[string]string{
			"testKey": testConfig,
		},
	}

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
		Spec: configv1.InfrastructureSpec{
			CloudConfig: configv1.ConfigMapFileReference{
				Name: "testName",
				Key:  "testKey",
			},
		},
	}

	nodeName := "somenodename"

	getMachineWithStatus := func(t *testing.T, status machinev1.MachineStatus) *machinev1.Machine {
		providerSpec := vsphereapi.VSphereMachineProviderSpec{
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: "test",
			},
			Workspace: &vsphereapi.Workspace{
				Server: host,
			},
		}
		raw, err := vsphereapi.RawExtensionFromProviderSpec(&providerSpec)
		if err != nil {
			t.Fatal(err)
		}
		return &machinev1.Machine{
			TypeMeta: metav1.TypeMeta{
				Kind: "Machine",
			},
			ObjectMeta: metav1.ObjectMeta{
				UID:       apimachinerytypes.UID(instanceUUID),
				Name:      "defaultFolder",
				Namespace: namespace,
			},
			Spec: machinev1.MachineSpec{
				ProviderSpec: machinev1.ProviderSpec{
					Value: raw,
				},
			},
			Status: status,
		}
	}

	getNodeWithConditions := func(conditions []corev1.NodeCondition) *corev1.Node {
		return &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nodeName,
				Namespace: metav1.NamespaceNone,
			},
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			Status: corev1.NodeStatus{
				Conditions: conditions,
			},
		}
	}

	testCases := []struct {
		testCase string
		machine  func(t *testing.T) *machinev1.Machine
		node     func(t *testing.T) *corev1.Node
	}{
		{
			testCase: "all good deletion",
			machine: func(t *testing.T) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				})
			},
			node: func(t *testing.T) *corev1.Node {
				return getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				})
			},
		},
		{
			testCase: "all good, no node linked",
			machine: func(t *testing.T) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{})
			},
			node: func(t *testing.T) *corev1.Node {
				return &corev1.Node{}
			},
		},
		{
			testCase: "all good, node unreachable",
			machine: func(t *testing.T) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				})
			},
			node: func(t *testing.T) *corev1.Node {
				return getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionUnknown,
					},
				})
			},
		},
		{
			testCase: "all good, node not found",
			machine: func(t *testing.T) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: "not-exists",
					},
				})
			},
			node: func(t *testing.T) *corev1.Node {
				return &corev1.Node{}
			},
		},
	}

	machinev1.AddToScheme(scheme.Scheme)
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
			vm.Config.InstanceUuid = instanceUUID

			client := fake.NewFakeClientWithScheme(scheme.Scheme,
				&credentialsSecret,
				tc.machine(t),
				configMap,
				infra,
				tc.node(t))
			machineScope, err := newMachineScope(machineScopeParams{
				client:    client,
				Context:   context.Background(),
				machine:   tc.machine(t),
				apiReader: client,
			})
			if err != nil {
				t.Fatal(err)
			}
			reconciler := newReconciler(machineScope)

			// expect the first call to delete to make the vSphere destroy request
			// and always return error to let it reconcile and monitor the destroy tasks until completion
			if err := reconciler.delete(); err == nil {
				t.Errorf("expected error on the first call to delete")
			}

			moTask, err := reconciler.session.GetTask(reconciler.Context, reconciler.providerStatus.TaskRef)
			if err != nil {
				if !isRetrieveMONotFound(reconciler.providerStatus.TaskRef, err) {
					t.Fatal(err)
				}
			}
			if moTask.Info.DescriptionId != "VirtualMachine.destroy" {
				t.Errorf("task description expected: VirtualMachine.destroy, got: %v", moTask.Info.DescriptionId)
			}

			// expect the second call to not find the vm and succeed
			if err := reconciler.delete(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			model.Machine--
			if model.Machine != model.Count().Machine {
				t.Errorf("Unexpected number of machines. Expected: %v, got: %v", model.Machine, model.Count().Machine)
			}
		})
	}
}

func TestCreate(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	host, port, err := net.SplitHostPort(server.URL.Host)
	if err != nil {
		t.Fatal(err)
	}

	credentialsSecretUsername := fmt.Sprintf("%s.username", host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", host)
	password, _ := server.URL.User.Password()
	vmName := "testName"
	namespace := "test"
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	vm.Name = vmName

	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testConfig := fmt.Sprintf(testConfigFmt, port)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: openshiftConfigNamespace,
		},
		Data: map[string]string{
			"testKey": testConfig,
		},
	}

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
		Spec: configv1.InfrastructureSpec{
			CloudConfig: configv1.ConfigMapFileReference{
				Name: "testName",
				Key:  "testKey",
			},
		},
	}

	userDataSecretName := "vsphere-ignition"
	userDataSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userDataSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			userDataSecretKey: []byte("{}"),
		},
	}

	cases := []struct {
		name                  string
		expectedError         error
		providerSpec          vsphereapi.VSphereMachineProviderSpec
		labels                map[string]string
		notConnectedToVCenter bool
	}{
		{
			name: "Successfully create machine",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &vsphereapi.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 1,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
		},
		{
			name: "Fail on invalid missing machine label",
			labels: map[string]string{
				machinev1.MachineClusterIDLabel: "",
			},
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &vsphereapi.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
			},
			expectedError: errors.New("test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label"),
		},
		{
			name: "Fail on not connected to vCenter",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &vsphereapi.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
			},
			expectedError:         errors.New("test: not connected to a vCenter"),
			notConnectedToVCenter: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			labels := map[string]string{
				machinev1.MachineClusterIDLabel: "CLUSTERID",
			}

			if tc.labels != nil {
				labels = tc.labels
			}

			if tc.notConnectedToVCenter {
				session.Client.ServiceContent.About.ApiType = ""
			}

			client := fake.NewFakeClientWithScheme(scheme.Scheme,
				credentialsSecret,
				configMap,
				infra,
				userDataSecret)

			rawProviderSpec, err := vsphereapi.RawExtensionFromProviderSpec(&tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}

			machineScope, err := newMachineScope(machineScopeParams{
				client:  client,
				Context: context.Background(),
				machine: &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
						Labels:    labels,
					},
					Spec: machinev1.MachineSpec{
						ProviderSpec: machinev1.ProviderSpec{
							Value: rawProviderSpec,
						},
					},
				},
				apiReader: client,
			})
			if err != nil {
				t.Fatal(err)
			}

			reconciler := newReconciler(machineScope)

			err = reconciler.create()

			if tc.expectedError != nil {
				if err == nil {
					t.Fatal("reconciler was expected to return error")
				}
				if err.Error() != tc.expectedError.Error() {
					t.Fatalf("Expected: %v, got %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("reconciler was not expected to return error: %v", err)
				}
			}
		})
	}
}

func TestUpdate(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	host, port, err := net.SplitHostPort(server.URL.Host)
	if err != nil {
		t.Fatal(err)
	}

	credentialsSecretUsername := fmt.Sprintf("%s.username", host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", host)

	password, _ := server.URL.User.Password()
	namespace := "test"
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	instanceUUID := "a5764857-ae35-34dc-8f25-a9c9e73aa898"
	vm.Config.InstanceUuid = instanceUUID

	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testConfig := fmt.Sprintf(testConfigFmt, port)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: openshiftConfigNamespace,
		},
		Data: map[string]string{
			"testKey": testConfig,
		},
	}

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
		Spec: configv1.InfrastructureSpec{
			CloudConfig: configv1.ConfigMapFileReference{
				Name: "testName",
				Key:  "testKey",
			},
		},
	}

	createTagAndCategory(session, "CLUSTERID_CATEGORY", "CLUSTERID")

	cases := []struct {
		name          string
		expectedError error
		providerSpec  vsphereapi.VSphereMachineProviderSpec
		labels        map[string]string
	}{
		{
			name: "Successfully update machine",
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				Workspace: &vsphereapi.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Template: vm.Name,
				Network: vsphereapi.NetworkSpec{
					Devices: []vsphereapi.NetworkDeviceSpec{
						{
							NetworkName: "test",
						},
					},
				},
			},
		},
		{
			name: "Fail on invalid missing machine label",
			labels: map[string]string{
				machinev1.MachineClusterIDLabel: "",
			},
			providerSpec: vsphereapi.VSphereMachineProviderSpec{
				Workspace: &vsphereapi.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Template: vm.Name,
				Network: vsphereapi.NetworkSpec{
					Devices: []vsphereapi.NetworkDeviceSpec{
						{
							NetworkName: "test",
						},
					},
				},
			},
			expectedError: errors.New("test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			labels := map[string]string{
				machinev1.MachineClusterIDLabel: "CLUSTERID",
			}

			if tc.labels != nil {
				labels = tc.labels
			}

			client := fake.NewFakeClientWithScheme(scheme.Scheme,
				credentialsSecret,
				configMap,
				infra)

			rawProviderSpec, err := vsphereapi.RawExtensionFromProviderSpec(&tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}

			machineScope, err := newMachineScope(machineScopeParams{
				client:  client,
				Context: context.Background(),
				machine: &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
						Labels:    labels,
						UID:       apimachinerytypes.UID(instanceUUID),
					},
					Spec: machinev1.MachineSpec{
						ProviderSpec: machinev1.ProviderSpec{
							Value: rawProviderSpec,
						},
					},
				},
				apiReader: client,
			})
			if err != nil {
				t.Fatal(err)
			}

			reconciler := newReconciler(machineScope)

			err = reconciler.update()

			if tc.expectedError != nil {
				if err == nil {
					t.Fatal("reconciler was expected to return error")
				}
				if err.Error() != tc.expectedError.Error() {
					t.Fatalf("Expected: %v, got %v", tc.expectedError, err)
				}
			} else {
				if err != nil {
					t.Fatalf("reconciler was not expected to return error: %v", err)
				}
			}
		})
	}
}

func TestExists(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	credentialsSecretUsername := fmt.Sprintf("%s.username", server.URL.Host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", server.URL.Host)

	password, _ := server.URL.User.Password()
	namespace := "test"
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	instanceUUID := "a5764857-ae35-34dc-8f25-a9c9e73aa898"
	vm.Config.InstanceUuid = instanceUUID

	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	vmObj := object.NewVirtualMachine(session.Client.Client, vm.Reference())
	task, err := vmObj.PowerOn(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		exists bool
	}{
		{
			name:   "VM doesn't exist",
			exists: false,
		},
		{
			name:   "VM already exists",
			exists: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			machineScope := machineScope{
				Context: context.TODO(),
				machine: &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "test",
						Labels: map[string]string{
							machinev1.MachineClusterIDLabel: "CLUSTERID",
						},
					},
				},
				providerSpec: &vsphereapi.VSphereMachineProviderSpec{
					Template: vm.Name,
				},
				session: session,
				providerStatus: &vsphereapi.VSphereMachineProviderStatus{
					TaskRef: task.Reference().Value,
				},
				client: fake.NewFakeClientWithScheme(scheme.Scheme, &credentialsSecret),
			}

			reconciler := newReconciler(&machineScope)

			if tc.exists {
				reconciler.machine.UID = apimachinerytypes.UID(instanceUUID)
			}

			exists, err := reconciler.exists()
			if err != nil {
				t.Fatalf("reconciler was not expected to return error: %v", err)
			}

			if tc.exists != exists {
				t.Fatalf("Expected: %v, got %v", tc.exists, exists)
			}
		})
	}
}

func TestReconcileMachineWithCloudState(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	instanceUUID := "a5764857-ae35-34dc-8f25-a9c9e73aa898"
	vm.Config.InstanceUuid = instanceUUID

	vmObj := object.NewVirtualMachine(session.Client.Client, vm.Reference())
	task, err := vmObj.PowerOn(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	createTagAndCategory(session, zoneKey, testZone)
	createTagAndCategory(session, regionKey, testRegion)

	if err := session.WithRestClient(context.TODO(), func(c *rest.Client) error {
		tagsMgr := tags.NewManager(c)

		err = tagsMgr.AttachTag(context.TODO(), testZone, vmObj.Reference())
		if err != nil {
			return err
		}

		err = tagsMgr.AttachTag(context.TODO(), testRegion, vmObj.Reference())
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	machineScope := machineScope{
		Context: context.TODO(),
		machine: &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "test",
				Labels: map[string]string{
					machinev1.MachineClusterIDLabel: "CLUSTERID",
				},
			},
		},
		providerSpec: &vsphereapi.VSphereMachineProviderSpec{
			Template: vm.Name,
			Network: vsphereapi.NetworkSpec{
				Devices: []vsphereapi.NetworkDeviceSpec{
					{
						NetworkName: "test",
					},
				},
			},
		},
		session: session,
		providerStatus: &vsphereapi.VSphereMachineProviderStatus{
			TaskRef: task.Reference().Value,
		},
		vSphereConfig: &vSphereConfig{
			Labels: Labels{
				Zone:   zoneKey,
				Region: regionKey,
			},
		},
	}

	vmWrapper := &virtualMachine{
		Context: machineScope.Context,
		Obj:     object.NewVirtualMachine(machineScope.session.Client.Client, vmObj.Reference()),
		Ref:     vmObj.Reference(),
	}

	reconciler := newReconciler(&machineScope)
	if err := reconciler.reconcileMachineWithCloudState(vmWrapper, task.Reference().Value); err != nil {
		t.Fatalf("reconciler was not expected to return error: %v", err)
	}

	expectedProviderID, err := convertUUIDToProviderID(vmWrapper.Obj.UUID(vmWrapper.Context))
	if err != nil {
		t.Fatal(err)
	}

	if expectedProviderID != *reconciler.machine.Spec.ProviderID {
		t.Errorf("Expected providerId: %s, got: %s", expectedProviderID, *reconciler.machine.Spec.ProviderID)
	}

	actualPowerState := reconciler.machine.Annotations[machinecontroller.MachineInstanceStateAnnotationName]
	if poweredOnState != actualPowerState {
		t.Errorf("Expected power state annotation: %s, got: %s", poweredOnState, actualPowerState)
	}

	labels := reconciler.machine.Labels
	if labels == nil {
		t.Error("Machine is expected to have labels")
	}

	if testZone != labels[machinecontroller.MachineAZLabelName] {
		t.Errorf("Expected zone name: %s, got: %s", testZone, labels[machinecontroller.MachineAZLabelName])
	}

	if testRegion != labels[machinecontroller.MachineRegionLabelName] {
		t.Errorf("Expected region name: %s, got: %s", testRegion, labels[machinecontroller.MachineRegionLabelName])
	}
}

func createTagAndCategory(session *session.Session, categoryName, tagName string) error {
	if err := session.WithRestClient(context.TODO(), func(c *rest.Client) error {
		tagsMgr := tags.NewManager(c)

		id, err := tagsMgr.CreateCategory(context.TODO(), &tags.Category{
			AssociableTypes: []string{"VirtualMachine"},
			Cardinality:     "SINGLE",
			Name:            categoryName,
		})
		if err != nil {
			return err
		}

		_, err = tagsMgr.CreateTag(context.TODO(), &tags.Tag{
			CategoryID: id,
			Name:       tagName,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return err
	}

	return nil
}

func TestVmDisksManipulation(t *testing.T) {
	ctx := context.Background()

	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := simulatorVM.VirtualMachine.Reference()
	vmObj := object.NewVirtualMachine(session.Client.Client, managedObjRef)

	addDiskToVm := func(vm *object.VirtualMachine, diskNum int, diskMode string, gmgAssert *GomegaWithT) {
		devices, err := vmObj.Device(ctx)
		gmgAssert.Expect(err).ToNot(HaveOccurred())
		scsi := devices.SelectByType((*types.VirtualSCSIController)(nil))[0]

		additionalDisk := &types.VirtualDisk{
			VirtualDevice: types.VirtualDevice{
				Backing: &types.VirtualDiskFlatVer2BackingInfo{
					DiskMode:        diskMode,
					ThinProvisioned: types.NewBool(true),
					VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
						FileName:  fmt.Sprintf("[LocalDS_0] %s/disk%d.vmdk", simulatorVM.Name, diskNum),
						Datastore: &simulatorVM.Datastore[0],
					},
				},
			},
		}
		additionalDisk.CapacityInKB = 1024
		devices.AssignController(additionalDisk, scsi.(types.BaseVirtualController))

		err = vmObj.AddDevice(ctx, additionalDisk)
		gmgAssert.Expect(err).ToNot(HaveOccurred())
	}

	vm := &virtualMachine{
		Context: ctx,
		Obj:     vmObj,
		Ref:     managedObjRef,
	}

	t.Run("Test getAttachedDisks", func(t *testing.T) {
		g := NewWithT(t)

		disks, err := vm.getAttachedDisks()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(disks[0].fileName).Should(ContainSubstring("disk1.vmdk"))
		g.Expect(disks[0].diskMode).Should(Equal("persistent"))
	})

	t.Run("detachPVDisks should detach only non persistent disks", func(t *testing.T) {
		g := NewWithT(t)

		addDiskToVm(vmObj, 2, string(types.VirtualDiskModeIndependent_persistent), g)
		addDiskToVm(vmObj, 3, string(types.VirtualDiskModeNonpersistent), g)
		addDiskToVm(vmObj, 4, string(types.VirtualDiskModeUndoable), g)

		disks, err := vm.getAttachedDisks()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(disks)).Should(Equal(4))
		g.Expect(disks[0].fileName).Should(ContainSubstring("disk1.vmdk"))
		g.Expect(disks[0].diskMode).Should(Equal("persistent"))

		g.Expect(disks[1].fileName).Should(ContainSubstring("disk2.vmdk"))

		g.Expect(disks[1].diskMode).Should(Equal("independent_persistent"))
		g.Expect(disks[2].diskMode).Should(Equal("nonpersistent"))
		g.Expect(disks[3].diskMode).Should(Equal("undoable"))

		err = vm.detachPVDisks()
		g.Expect(err).ToNot(HaveOccurred())
		disks, err = vm.getAttachedDisks()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(disks)).Should(Equal(1))
	})

	t.Run("detachPVDisks do not detach persistent disks", func(t *testing.T) {
		// Do not touch persistent disks
		g := NewWithT(t)

		addDiskToVm(vmObj, 5, string(types.VirtualDiskModePersistent), g)
		disks, err := vm.getAttachedDisks()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(disks)).Should(Equal(2))
		err = vm.detachPVDisks()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(disks)).Should(Equal(2))
	})
}

func TestReconcilePowerStateAnnontation(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := simulatorVM.VirtualMachine.Reference()
	vmObj := object.NewVirtualMachine(session.Client.Client, simulatorVM.Reference())
	_, err := vmObj.PowerOn(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	vm := &virtualMachine{
		Context: context.Background(),
		Obj:     object.NewVirtualMachine(session.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	testCases := []struct {
		name          string
		vm            *virtualMachine
		expectedError bool
	}{
		{
			name: "Succefully reconcile annotation",
			vm:   vm,
		},
		{
			name:          "Error on nil VM",
			vm:            nil,
			expectedError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := &Reconciler{
				machineScope: &machineScope{
					machine: &machinev1.Machine{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{},
						},
					},
				},
			}

			err := r.reconcilePowerStateAnnontation(tc.vm)

			if tc.expectedError {
				if err == nil {
					t.Errorf("reconcilePowerStateAnnontation is expected to return an error")
				}

				actualPowerState := r.machine.Annotations[machinecontroller.MachineInstanceStateAnnotationName]
				if actualPowerState != "" {
					t.Errorf("Expected power state annotation to be empty, got: %s", actualPowerState)
				}
			} else {
				if err != nil {
					t.Errorf("reconcilePowerStateAnnontation is not expected to return an error")
				}

				actualPowerState := r.machine.Annotations[machinecontroller.MachineInstanceStateAnnotationName]
				if poweredOnState != actualPowerState {
					t.Errorf("Expected power state annotation: %s, got: %s", poweredOnState, actualPowerState)
				}
			}
		})
	}
}

// See https://github.com/vmware/govmomi/blob/master/simulator/example_extend_test.go#L33:6 for extending behaviour example
