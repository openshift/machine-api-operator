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
	"net"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	"github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	vsphere "k8s.io/cloud-provider-vsphere/pkg/common/config"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/features"
	machinev1 "github.com/openshift/api/machine/v1beta1"

	ipamv1beta1 "sigs.k8s.io/cluster-api/exp/ipam/api/v1beta1"

	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere/session"
	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"

	_ "github.com/vmware/govmomi/vapi/simulator"
)

const (
	poweredOnState         = "poweredOn"
	minimumHWVersionString = "vmx-15"
)

func init() {
	// Add types to scheme
	if err := configv1.Install(scheme.Scheme); err != nil {
		panic(fmt.Sprintf("cannot add scheme: %v", err))
	}
}

type simulatorModelOption func(m *simulator.Model)

func initSimulator(t *testing.T) (*simulator.Model, *session.Session, *simulator.Server) {
	model := simulator.VPX()
	model.Host = 0

	err := model.Create()
	if err != nil {
		t.Fatal(err)
	}
	model.Service.TLS = new(tls.Config)
	model.Service.RegisterEndpoints = true

	server := model.Service.NewServer()
	session := getSimulatorSession(t, server)

	return model, session, server
}

func initSimulatorCustom(t *testing.T, opts ...simulatorModelOption) (*simulator.Model, *simulator.Server) {
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

	return model, server
}

func getSimulatorSession(t *testing.T, server *simulator.Server) *session.Session {
	return getSimulatorSessionWithDC(t, server, "")
}

func getSimulatorSessionWithDC(t *testing.T, server *simulator.Server, dc string) *session.Session {
	pass, _ := server.URL.User.Password()

	authSession, err := session.GetOrCreate(
		context.TODO(),
		server.URL.Host, dc,
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

	return authSession
}

func TestClone(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	credentialsSecretUsername := fmt.Sprintf("%s.username", server.URL.Host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", server.URL.Host)

	password, _ := server.URL.User.Password()
	namespace := "test"
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	vm.Config.Version = minimumHWVersionString

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
	diskSize := int32(disk.CapacityInBytes / 1024 / 1024 / 1024)

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

	getMachineScope := func(providerSpec *machinev1.VSphereMachineProviderSpec) *machineScope {
		gates, _ := testutils.NewDefaultMutableFeatureGate()
		return &machineScope{
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
			providerSpec:   providerSpec,
			session:        session,
			providerStatus: &machinev1.VSphereMachineProviderStatus{},
			client:         fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(&credentialsSecret, &userDataSecret).Build(),
			featureGates:   gates,
		}
	}

	testCases := []struct {
		testCase              string
		cloneVM               bool
		expectedError         error
		setupFailureCondition func() error
		providerSpec          machinev1.VSphereMachineProviderSpec
		machineName           string
	}{
		{
			testCase: "clone machine from default values",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  diskSize,
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: server.URL.Host,
					Folder: "custom-folder",
				},
				DiskGiB:  diskSize,
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  diskSize + 1,
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  diskSize - 1,
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			expectedError: fmt.Errorf("error getting disk spec for \"\": can't resize template disk down, initial capacity is larger: %vKiB > %vKiB", diskSize*1024*1024, (diskSize-1)*1024*1024),
		},
		{
			testCase: "fail on invalid resource pool",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
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
				dir, err := os.MkdirTemp("", "tmpdir")
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
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
			machineScope := getMachineScope(&tc.providerSpec)
			if tc.machineName != "" {
				machineScope.machine.Name = tc.machineName
			}

			taskRef, err := clone(machineScope)

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

	t.Run("Test template HW version handling", func(t *testing.T) {
		getMinimalProviderSpec := func() *machinev1.VSphereMachineProviderSpec {
			return &machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: server.URL.Host,
				},
				DiskGiB:  diskSize,
				Template: vm.Name,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			}
		}

		cases := []struct {
			name              string
			errMsg            string
			VmHWVersionString string
		}{
			{
				name:              "Clone ok",
				errMsg:            "",
				VmHWVersionString: minimumHWVersionString,
			}, {
				name:              "HW version lower than minimal: 2",
				errMsg:            "Hardware lower than 15 is not supported, clone stopped. Detected machine template version is 2.",
				VmHWVersionString: "vmx-2",
			}, {
				name:              "HW version lower than minimal: 13",
				errMsg:            "Hardware lower than 15 is not supported, clone stopped. Detected machine template version is 13.",
				VmHWVersionString: "vmx-13",
			}, {
				name:              "HW version not recognized",
				errMsg:            "Unable to detect machine template HW version for machine 'test': can not extract hardware version from version string: foobar-123, format unknown",
				VmHWVersionString: "foobar-123",
			},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				g := NewWithT(t)
				// change vm template hw version for single test
				vm.Config.Version = tc.VmHWVersionString
				defer func() {
					vm.Config.Version = minimumHWVersionString
				}()

				scope := getMachineScope(getMinimalProviderSpec())
				_, err := clone(scope)
				if tc.errMsg != "" {
					g.Expect(err).Should(HaveOccurred())
					g.Expect(err.Error()).Should(ContainSubstring(tc.errMsg))
				}
			})
		}
	})
}

// https://github.com/openshift/installer/tree/master/pkg/infrastructure/vsphere/clusterapi/ (group.go)

func createVMGroup(ctx context.Context, session *session.Session, cluster, vmGroup string) error {
	clusterObj, err := session.Finder.ClusterComputeResource(ctx, cluster)
	if err != nil {
		return err
	}

	clusterConfigSpec := &types.ClusterConfigSpecEx{
		GroupSpec: []types.ClusterGroupSpec{
			{
				ArrayUpdateSpec: types.ArrayUpdateSpec{
					Operation: types.ArrayUpdateOperation("add"),
				},
				Info: &types.ClusterVmGroup{
					ClusterGroupInfo: types.ClusterGroupInfo{
						Name: vmGroup,
					},
				},
			},
		},
	}

	task, err := clusterObj.Reconfigure(ctx, clusterConfigSpec, true)
	if err != nil {
		return fmt.Errorf("error reconfiguring simulator cluster object: %w", err)
	}

	return task.Wait(ctx)
}

func TestPowerOn(t *testing.T) {
	model, simSession, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	getMinimalProviderSpec := func() *machinev1.VSphereMachineProviderSpec {
		return &machinev1.VSphereMachineProviderSpec{
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: "test",
			},
			Workspace: &machinev1.Workspace{
				Server: server.URL.Host,
			},
			DiskGiB:  int32(5),
			Template: "template",
			UserDataSecret: &corev1.LocalObjectReference{
				Name: "foo",
			},
		}
	}

	getMachineScope := func(providerSpec *machinev1.VSphereMachineProviderSpec, name string) *machineScope {
		return &machineScope{
			Context: context.TODO(),
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: "test",
					Labels: map[string]string{
						machinev1.MachineClusterIDLabel: "CLUSTERID",
					},
				},
			},
			providerSpec:   providerSpec,
			session:        simSession,
			providerStatus: &machinev1.VSphereMachineProviderStatus{},
			client:         fake.NewClientBuilder().WithScheme(scheme.Scheme).Build(),
		}
	}

	t.Run("powerOn should fail if there is no machine found", func(t *testing.T) {
		g := NewWithT(t)

		scope := getMachineScope(getMinimalProviderSpec(), "test")
		taskId, err := powerOn(scope)

		g.Expect(err).Should(HaveOccurred())
		g.Expect(taskId).To(BeEmpty())
		g.Expect(err.Error()).Should(ContainSubstring("vm not found during creation for powering on"))
	})

	t.Run("Test powering on vm with RDS (via datacenter)", func(t *testing.T) {
		g := NewWithT(t)
		vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
		object.NewVirtualMachine(simSession.Client.Client, vm.Reference())

		scope := getMachineScope(getMinimalProviderSpec(), vm.Name)
		taskId, err := powerOn(scope)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(taskId).NotTo(BeEmpty())

		task, err := simSession.GetTask(context.TODO(), taskId)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(task.Info.DescriptionId).Should(BeEquivalentTo("Datacenter.powerOnMultiVM"))
	})

	t.Run("Test powering on vm without a datacenter", func(t *testing.T) {
		g := NewWithT(t)
		vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
		object.NewVirtualMachine(simSession.Client.Client, vm.Reference())

		scope := getMachineScope(getMinimalProviderSpec(), vm.Name)
		scope.session.Datacenter = nil
		taskId, err := powerOn(scope)

		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(taskId).NotTo(BeEmpty())

		task, err := simSession.GetTask(context.TODO(), taskId)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(task.Info.DescriptionId).Should(BeEquivalentTo("VirtualMachine.powerOn"))
	})
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
	err = task.Wait(context.TODO())
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

func TestStaticIPs(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	testStaticIPsWithSimulator(t, model, server, session)
}

func TestGetNetworkDevicesSingleDatacenter(t *testing.T) {
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	testGetNetworkDevicesWithSimulator(t, model, server, session)
}

func TestGetNetworkDevicesMultiDatacenter(t *testing.T) {
	withMultiDatacenter := func(m *simulator.Model) {
		m.OpaqueNetwork = 2
		m.Datacenter = 2
		m.Cluster = 2
	}

	model, server := initSimulatorCustom(t, withMultiDatacenter)
	session := getSimulatorSessionWithDC(t, server, "DC0")
	defer model.Remove()
	defer server.Close()

	testGetNetworkDevicesWithSimulator(t, model, server, session)
}

func testStaticIPsWithSimulator(t *testing.T, model *simulator.Model, server *simulator.Server, session *session.Session) {
	ipv4Static := []machinev1.NetworkDeviceSpec{
		{
			Gateway:     "192.168.1.1",
			IPAddrs:     []string{"192.168.1.2/24"},
			Nameservers: []string{"192.168.1.100"},
		},
	}
	ipv6Static := []machinev1.NetworkDeviceSpec{
		{
			Gateway:     "2001::1",
			IPAddrs:     []string{"2001::2/64"},
			Nameservers: []string{"2001::100"},
		},
	}
	dualStackStatic := []machinev1.NetworkDeviceSpec{
		{
			Gateway:     "192.168.1.1",
			IPAddrs:     []string{"192.168.1.2/24", "2001::2/64"},
			Nameservers: []string{"192.168.1.100"},
		},
	}

	poolGroup := "ipam.test.io"
	addressClaim := []machinev1.NetworkDeviceSpec{
		{
			AddressesFromPools: []machinev1.AddressesFromPool{
				{
					Name:     "test-pool",
					Group:    poolGroup,
					Resource: "ippools",
				},
			},
			Nameservers: []string{
				"192.168.1.100",
			},
		},
	}
	addressClaimAndIpPool := []machinev1.NetworkDeviceSpec{
		{
			AddressesFromPools: []machinev1.AddressesFromPool{
				{
					Name:     "test-pool",
					Group:    poolGroup,
					Resource: "ippools",
				},
			},
			Gateway:     "192.168.1.1",
			IPAddrs:     []string{"192.168.1.2/24"},
			Nameservers: []string{"192.168.1.100"},
		},
	}

	ipAddressClaim := ipamv1beta1.IPAddressClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim-0-0",
			Namespace: "openshift-machine-api",
		},
		Spec: ipamv1beta1.IPAddressClaimSpec{
			PoolRef: corev1.TypedLocalObjectReference{
				Name:     "test-pool",
				APIGroup: &poolGroup,
				Kind:     "ippools",
			},
		},
		Status: ipamv1beta1.IPAddressClaimStatus{
			AddressRef: corev1.LocalObjectReference{
				Name: "test-test-0",
			},
		},
	}

	ipAddress := ipamv1beta1.IPAddress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-test-0",
			Namespace: "openshift-machine-api",
		},
		Spec: ipamv1beta1.IPAddressSpec{
			ClaimRef: corev1.LocalObjectReference{
				Name: "test-test-0",
			},
			PoolRef: corev1.TypedLocalObjectReference{
				Name: "test-pool",
			},
			Address: "192.168.1.11",
			Prefix:  24,
			Gateway: "192.168.1.1",
		},
	}
	testCases := []struct {
		testCase    string
		networkSpec []machinev1.NetworkDeviceSpec
		expected    string
		err         string
	}{
		{
			testCase:    "Valid IPv4 Static IP",
			networkSpec: ipv4Static,
			expected:    "ip=192.168.1.2::192.168.1.1:255.255.255.0:::none nameserver=192.168.1.100",
		},
		{
			testCase:    "Valid IPv6 Static IP",
			networkSpec: ipv6Static,
			expected:    "ip=[2001::2]::[2001::1]:64:::none nameserver=[2001::100]",
		},
		{
			testCase:    "Valid dual stack",
			networkSpec: dualStackStatic,
			expected:    "ip=192.168.1.2::192.168.1.1:255.255.255.0:::none ip=[2001::2]:::64:::none nameserver=192.168.1.100",
		},
		{
			testCase:    "IPAM Allocated address",
			networkSpec: addressClaim,
			expected:    "ip=192.168.1.11::192.168.1.1:255.255.255.0:::none nameserver=192.168.1.100",
		},
		{
			testCase:    "IPAM Allocated address and ipAddrs",
			networkSpec: addressClaimAndIpPool,
			expected:    "ip=192.168.1.2::192.168.1.1:255.255.255.0:::none ip=192.168.1.11::192.168.1.1:255.255.255.0:::none nameserver=192.168.1.100",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
				&ipAddressClaim,
				&ipAddress,
			).Build()

			machineScope := machineScope{
				Context: context.Background(),
				machine: &machinev1.Machine{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: "openshift-machine-api",
					},
				},
				providerSpec: &machinev1.VSphereMachineProviderSpec{
					Network: machinev1.NetworkSpec{
						Devices: tc.networkSpec,
					},
				},
				session:   session,
				apiReader: client,
				client:    client,
			}

			kargs, err := constructKargsFromNetworkConfig(&machineScope)
			if err != nil {
				if len(tc.err) > 0 {
					if tc.err != err.Error() {
						t.Errorf("error %s did not match expected %s", err.Error(), tc.err)
					}
				} else {
					t.Error(err)
				}
			} else {
				if strings.TrimSpace(kargs) != tc.expected {
					t.Errorf("kargs %s did not match expected %s", kargs, tc.expected)
				}
			}
		})
	}
}

func testGetNetworkDevicesWithSimulator(t *testing.T, model *simulator.Model, server *simulator.Server, session *session.Session) {

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

	resourcePool, err := session.Finder.ResourcePool(context.TODO(), "/DC0/host/DC0_C0/Resources")
	if err != nil {
		t.Fatal(err)
	}

	testCases := []struct {
		testCase     string
		providerSpec *machinev1.VSphereMachineProviderSpec
		expected     func(gotDevices []types.BaseVirtualDeviceConfigSpec, err error) bool
	}{
		{
			testCase:     "no Network",
			providerSpec: &machinev1.VSphereMachineProviderSpec{},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec, err error) bool {
				if err != nil {
					t.Fatal(err)
					return false
				}
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
			testCase: "wrong Network",
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
						{
							NetworkName: "Wrong Network",
						},
					},
				},
			},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec, err error) bool {
				if len(gotDevices) != 0 {
					return false
				}
				if err == nil {
					return false
				}
				return true
			},
		},
		{
			testCase: "one Network",
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
						{
							NetworkName: "VM Network",
						},
					},
				},
			},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec, err error) bool {
				if err != nil {
					t.Fatal(err)
					return false
				}
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
		{
			testCase: "two Networks with non existed first one",
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
						{
							NetworkName: "Not Existed",
						},
						{
							NetworkName: "VM Network",
						},
					},
				},
			},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec, err error) bool {
				if err == nil {
					t.Fatal("Error expected")
					return false
				}
				return true
			},
		},
		{
			testCase: "two Networks with non existed second one",
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
						{
							NetworkName: "VM Network",
						},
						{
							NetworkName: "Non Existed",
						},
					},
				},
			},
			expected: func(gotDevices []types.BaseVirtualDeviceConfigSpec, err error) bool {
				if err == nil {
					t.Fatal("Error expected")
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
			networkDevices, err := getNetworkDevices(machineScope, resourcePool, devices)

			if !tc.expected(networkDevices, err) {
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
		diskCount            int32
		expectedCapacityInKB int64
	}{
		{
			name: "Successfully get disk spec with disk size 1",
			devices: func() object.VirtualDeviceList {
				devices, err := objVM.Device(context.TODO())
				if err != nil {
					t.Fatal(err)
				}
				return devices
			},
			diskSize:             10,
			diskCount:            1,
			expectedCapacityInKB: 10485760,
		},
		{
			name: "Successfully get disk spec with disk size 3",
			devices: func() object.VirtualDeviceList {
				devices, err := objVM.Device(context.TODO())
				if err != nil {
					t.Fatal(err)
				}
				return devices
			},
			diskSize:             30,
			diskCount:            1,
			expectedCapacityInKB: 31457280,
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
			diskCount:            1,
			expectedCapacityInKB: 1048576,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			machineScope := &machineScope{
				Context: context.TODO(),
				providerSpec: &machinev1.VSphereMachineProviderSpec{
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

func TestCreateDataDisks(t *testing.T) {
	model, session, server := initSimulator(t)
	t.Cleanup(model.Remove)
	t.Cleanup(server.Close)
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	machine := object.NewVirtualMachine(session.Client.Client, vm.Reference())

	deviceList, err := machine.Device(context.TODO())
	if err != nil {
		t.Fatalf("Failed to obtain vm devices: %v", err)
	}

	// Find primary disk and get controller
	disks := deviceList.SelectByType((*types.VirtualDisk)(nil))
	primaryDisk := disks[0].(*types.VirtualDisk)
	controller, ok := deviceList.FindByKey(primaryDisk.ControllerKey).(types.BaseVirtualController)
	if !ok {
		t.Fatalf("unable to get controller for test")
	}

	getMachineScope := func(providerSpec *machinev1.VSphereMachineProviderSpec) *machineScope {
		gates, _ := testutils.NewDefaultMutableFeatureGate()
		return &machineScope{
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
			providerSpec:   providerSpec,
			session:        session,
			providerStatus: &machinev1.VSphereMachineProviderStatus{},
			featureGates:   gates,
		}
	}

	testCases := []struct {
		name               string
		devices            object.VirtualDeviceList
		controller         types.BaseVirtualController
		dataDisks          []machinev1.VSphereDisk
		expectedUnitNumber []int
		err                string
	}{
		{
			name:               "Add data disk with 1 ova disk",
			devices:            deviceList,
			controller:         controller,
			dataDisks:          createDataDiskDefinitions(1),
			expectedUnitNumber: []int{1},
		},
		{
			name:               "Add data disk with 2 ova disk",
			devices:            createAdditionalDisks(deviceList, controller, 1),
			controller:         controller,
			dataDisks:          createDataDiskDefinitions(1),
			expectedUnitNumber: []int{2},
		},
		{
			name:               "Add multiple data disk with 1 ova disk",
			devices:            deviceList,
			controller:         controller,
			dataDisks:          createDataDiskDefinitions(2),
			expectedUnitNumber: []int{1, 2},
		},
		{
			name:       "Add too many data disks with 1 ova disk",
			devices:    deviceList,
			controller: controller,
			dataDisks:  createDataDiskDefinitions(30),
			err:        "all unit numbers are already in-use",
		},
		{
			name:       "Add data disk with no ova disk",
			devices:    nil,
			controller: nil,
			dataDisks:  createDataDiskDefinitions(1),
			err:        "invalid disk count: 0",
		},
		{
			name:       "Add too many data disks with 1 ova disk",
			devices:    deviceList,
			controller: controller,
			dataDisks:  createDataDiskDefinitions(40),
			err:        "all unit numbers are already in-use",
		},
	}

	for _, test := range testCases {
		tc := test
		t.Run(tc.name, func(t *testing.T) {
			var funcError error
			scope := getMachineScope(&machinev1.VSphereMachineProviderSpec{
				DataDisks: tc.dataDisks,
			})

			// Create the data disks
			newDisks, funcError := createDataDisks(scope, tc.devices)
			if (tc.err != "" && funcError == nil) || (tc.err == "" && funcError != nil) || (funcError != nil && tc.err != funcError.Error()) {
				t.Fatalf("Expected to get '%v' error from assignUnitNumber, got: '%v'", tc.err, funcError)
			}

			if tc.err == "" && funcError == nil {
				// Check number of disks present
				if len(newDisks) != len(tc.dataDisks) {
					t.Fatalf("Expected device count to be %v, but found %v", len(tc.dataDisks), len(newDisks))
				}

				// Validate the configs of new data disks
				for index, disk := range newDisks {
					// Check disk size matches original request
					vd := disk.GetVirtualDeviceConfigSpec().Device.(*types.VirtualDisk)
					expectedSize := int64(tc.dataDisks[index].SizeGiB * 1024 * 1024)
					if vd.CapacityInKB != expectedSize {
						t.Fatalf("Expected disk size (KB) %d to match %d", vd.CapacityInKB, expectedSize)
					}

					// Check unit number
					unitNumber := *disk.GetVirtualDeviceConfigSpec().Device.GetVirtualDevice().UnitNumber
					if tc.err == "" && unitNumber != int32(tc.expectedUnitNumber[index]) {
						t.Fatalf("Expected to get unitNumber '%d' error from assignUnitNumber, got: '%d'", tc.expectedUnitNumber[index], unitNumber)
					}
				}
			}
		})
	}
}

func createAdditionalDisks(devices object.VirtualDeviceList, controller types.BaseVirtualController, numOfDisks int) object.VirtualDeviceList {
	deviceList := devices
	disks := devices.SelectByType((*types.VirtualDisk)(nil))
	primaryDisk := disks[0].(*types.VirtualDisk)

	for i := 0; i < numOfDisks; i++ {
		newDevice := createVirtualDisk(primaryDisk.ControllerKey+1, controller, 10)
		newUnitNumber := *primaryDisk.UnitNumber + int32(i+1)
		newDevice.UnitNumber = &newUnitNumber
		deviceList = append(deviceList, newDevice)
	}
	return deviceList
}

func createVirtualDisk(key int32, controller types.BaseVirtualController, diskSize int32) *types.VirtualDisk {
	dev := &types.VirtualDisk{
		VirtualDevice: types.VirtualDevice{
			Key: key,
			Backing: &types.VirtualDiskFlatVer2BackingInfo{
				DiskMode:        string(types.VirtualDiskModePersistent),
				ThinProvisioned: types.NewBool(true),
				VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
					FileName: "",
				},
			},
		},
		CapacityInKB: int64(diskSize) * 1024 * 1024,
	}

	if controller != nil {
		dev.VirtualDevice.ControllerKey = controller.GetVirtualController().Key
	}
	return dev
}

func createDataDiskDefinitions(numOfDataDisks int) []machinev1.VSphereDisk {
	disks := []machinev1.VSphereDisk{}

	for i := 0; i < numOfDataDisks; i++ {
		disk := machinev1.VSphereDisk{
			Name:    fmt.Sprintf("disk_%d", i),
			SizeGiB: 10 * int32(i),
		}
		disks = append(disks, disk)
	}
	return disks
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

	defaultFakeMAC := "00:0c:29:00:00:00"
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

	// Test if the MAC address belongs to VMware
	if !strings.HasPrefix(networkStatusList[0].MACAddr, "00:0c:29") {
		t.Errorf("Expected MACAddr to start with 00:0c:29, got %v", networkStatusList[0].MACAddr)
	}
	networkStatusList[0].MACAddr = defaultFakeMAC // The simulator generates a random MAC address, so we need to set it to the expected value
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
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
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
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := object.NewVirtualMachine(sessionObj.Client.Client, managedObj.Reference()).Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     object.NewVirtualMachine(sessionObj.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	testCases := []struct {
		name          string
		expectedError bool
		attachTag     bool
		testCondition func(tagName string) (string, error)
		tagName       string
	}{
		{
			name:          "Don't fail when tag doesn't exist",
			expectedError: false,
			tagName:       "FOOOOOOOOO",
		},
		{
			name:          "Successfully attach a tag",
			expectedError: false,
			attachTag:     true,
			tagName:       "BAAAAAAR",
			testCondition: func(tagName string) (string, error) {
				_, err := createTagAndCategory(sessionObj, tagToCategoryName(tagName), tagName)
				return "", err
			},
		},
		{
			name:          "Successfully attach additional tags",
			expectedError: false,
			attachTag:     true,
			tagName:       "BAAAAAAR2",
			testCondition: func(tagName string) (string, error) {
				if _, err := createTagAndCategory(sessionObj, tagToCategoryName(tagName), tagName); err != nil {
					return "", err
				}

				additionalTag := "test-tag"
				if additionalTagID, err := createTagAndCategory(sessionObj, tagToCategoryName(additionalTag), additionalTag); err != nil {
					return "", err
				} else {
					return additionalTagID, nil
				}

			},
		},
		{
			name:          "Fail on vSphere API error",
			expectedError: false,
			tagName:       "BAAAAAZ",
			testCondition: func(tagName string) (string, error) {
				return "", sessionObj.Logout(context.Background())
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			providerSpec := &machinev1.VSphereMachineProviderSpec{}
			if tc.testCondition != nil {
				additionalTagID, err := tc.testCondition(tc.tagName)
				if err != nil {
					t.Fatalf("Not expected error %v", err)
				}
				if len(additionalTagID) > 0 {
					providerSpec.TagIDs = []string{additionalTagID}
				}
			}

			err := vm.reconcileTags(context.TODO(), sessionObj, &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "machine",
					Labels: map[string]string{machinev1.MachineClusterIDLabel: tc.tagName},
				},
			}, providerSpec)

			if tc.expectedError {
				if err == nil {
					t.Fatal("Expected error")
				}
			} else {
				if err != nil {
					t.Fatalf("Not expected error %v", err)
				}

				if tc.attachTag {
					if err := sessionObj.WithCachingTagsManager(context.TODO(), func(tagMgr *session.CachingTagsManager) error {

						tags, err := tagMgr.GetAttachedTags(context.TODO(), managedObjRef)
						if err != nil {
							return err
						}

						if len(tags) == 0 {
							t.Fatalf("Expected tags to be found")
						}

						expectedTags := []string{tc.tagName}
						if len(providerSpec.TagIDs) > 0 {
							expectedTags = append(expectedTags, providerSpec.TagIDs...)
						}

						for _, expectedTag := range expectedTags {
							gotTag := false
							for _, attachedTag := range tags {
								if session.IsName(expectedTag) {
									if attachedTag.Name == expectedTag {
										gotTag = true
										break
									}
								} else {
									if attachedTag.ID == expectedTag {
										gotTag = true
										break
									}
								}
							}
							if !gotTag {
								t.Fatalf("Expected tag %s to be found", expectedTag)
							}
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
	model, sessionObj, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	managedObj := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := object.NewVirtualMachine(sessionObj.Client.Client, managedObj.Reference()).Reference()

	vm := &virtualMachine{
		Context: context.TODO(),
		Obj:     object.NewVirtualMachine(sessionObj.Client.Client, managedObjRef),
		Ref:     managedObjRef,
	}

	tagName := "CLUSTERID"
	nonAttachedTagName := "nonAttachedTag"

	if err := sessionObj.WithRestClient(context.TODO(), func(c *rest.Client) error {
		tagsMgr := tags.NewManager(c)

		id, err := tagsMgr.CreateCategory(context.TODO(), &tags.Category{
			AssociableTypes: []string{"VirtualMachine"},
			Cardinality:     "SINGLE",
			Name:            tagToCategoryName(tagName),
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

		nonAttachedCategoryId, err := tagsMgr.CreateCategory(context.TODO(), &tags.Category{
			AssociableTypes: []string{"VirtualMachine"},
			Cardinality:     "SINGLE",
			Name:            tagToCategoryName(nonAttachedTagName),
		})

		if err != nil {
			return err
		}

		_, err = tagsMgr.CreateTag(context.TODO(), &tags.Tag{
			CategoryID: nonAttachedCategoryId,
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
			if err := sessionObj.WithCachingTagsManager(context.TODO(), func(c *session.CachingTagsManager) error {

				attached, err := vm.checkAttachedTag(context.TODO(), tc.tagName, c)
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
	type vCenterSimConfig struct {
		infra       *configv1.Infrastructure
		secret      *corev1.Secret
		configMap   *corev1.ConfigMap
		featureGate *configv1.FeatureGate
		host        string
		port        string

		username string
		pwd      string

		simServer *simulator.Server
	}

	withMoreVms := func(vmsCount int) simulatorModelOption {
		return func(m *simulator.Model) {
			m.Machine = vmsCount
		}
	}

	model, server := initSimulatorCustom(t, withMoreVms(5))
	defer model.Remove()
	defer server.Close()

	namespace := "test"

	instanceUUID := "a5764857-ae35-34dc-8f25-a9c9e73aa898"

	getVcenterSimParams := func(server *simulator.Server, ns string) (*vCenterSimConfig, error) {
		host, port, err := net.SplitHostPort(server.URL.Host)
		if err != nil {
			return nil, err
		}
		unameKey := fmt.Sprintf("%s.username", host)
		pwdKey := fmt.Sprintf("%s.password", host)

		password, _ := server.URL.User.Password()

		credentialsSecretName := "test"
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      credentialsSecretName,
				Namespace: ns,
			},
			Data: map[string][]byte{
				unameKey: []byte(server.URL.User.Username()),
				pwdKey:   []byte(password),
			},
		}

		testConfig := fmt.Sprintf(testConfigFmt, port, credentialsSecretName, ns)
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "testName",
				Namespace: openshiftConfigNamespaceForTest,
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

		featureGate := &configv1.FeatureGate{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
		}

		return &vCenterSimConfig{
			infra:       infra,
			secret:      secret,
			configMap:   configMap,
			host:        host,
			port:        port,
			username:    server.URL.User.Username(),
			pwd:         password,
			simServer:   server,
			featureGate: featureGate,
		}, nil
	}

	nodeName := "somenodename"

	getMachineWithStatus := func(t *testing.T, status machinev1.MachineStatus, simHost string) *machinev1.Machine {
		providerSpec := machinev1.VSphereMachineProviderSpec{
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: "test",
			},
			Workspace: &machinev1.Workspace{
				Server: simHost,
			},
		}
		raw, err := RawExtensionFromProviderSpec(&providerSpec)
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
		machine  func(t *testing.T, simServerHost string) *machinev1.Machine
		node     func(t *testing.T) *corev1.Node
	}{
		{
			testCase: "all good deletion",
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
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
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{}, simServerHost)
			},
			node: func(t *testing.T) *corev1.Node {
				return &corev1.Node{}
			},
		},
		{
			testCase: "all good, node unreachable",
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
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
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: "not-exists",
					},
				}, simServerHost)
			},
			node: func(t *testing.T) *corev1.Node {
				return &corev1.Node{}
			},
		},
	}

	if err := machinev1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("cannot add scheme: %v", err)
	}

	if err := ipamv1beta1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatalf("cannot add scheme: %v", err)
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
			vm.Config.InstanceUuid = instanceUUID

			simParams, err := getVcenterSimParams(server, namespace)
			if err != nil {
				t.Fatal(err)
			}

			gates, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
				simParams.secret,
				tc.machine(t, simParams.host),
				simParams.configMap,
				simParams.infra,
				tc.node(t)).Build()
			machineScope, err := newMachineScope(machineScopeParams{
				client:                   client,
				Context:                  context.Background(),
				machine:                  tc.machine(t, simParams.host),
				apiReader:                client,
				openshiftConfigNameSpace: openshiftConfigNamespaceForTest,
				featureGates:             gates,
			})
			if err != nil {
				t.Fatal(err)
			}
			reconciler := newReconciler(machineScope)

			// expect the first call to delete to make the vSphere power off request
			// and always return error to let it reconcile and monitor power off tasks until completion
			if err := reconciler.delete(); err == nil {
				t.Errorf("expected error on the first call to delete")
			}

			powerOffTask, err := reconciler.session.GetTask(reconciler.Context, reconciler.providerStatus.TaskRef)
			if err != nil {
				if !isRetrieveMONotFound(reconciler.providerStatus.TaskRef, err) {
					t.Fatal(err)
				}
			}
			task := object.NewTask(reconciler.session.Client.Client, powerOffTask.Reference())
			err = task.Wait(context.TODO())
			if err != nil {
				t.Fatal(err)
			}

			// first run should schedule power off
			if powerOffTask.Info.DescriptionId != powerOffVmTaskDescriptionId {
				t.Errorf("task description expected: %v, got: %v", powerOffVmTaskDescriptionId, powerOffTask.Info.DescriptionId)
			}

			// expect second 'delete' call to make the vSphere destroy request
			// and return an error to let it reconcile and monitor destroy tasks until completion
			if err := reconciler.delete(); err == nil {
				t.Errorf("expected error on the second call to delete")
			}
			destroyTask, err := reconciler.session.GetTask(reconciler.Context, reconciler.providerStatus.TaskRef)
			if err != nil {
				if !isRetrieveMONotFound(reconciler.providerStatus.TaskRef, err) {
					t.Fatal(err)
				}
			}
			task = object.NewTask(reconciler.session.Client.Client, destroyTask.Reference())
			err = task.Wait(context.TODO())
			if err != nil {
				t.Fatal(err)
			}

			// second run should destroy vm
			if destroyTask.Info.DescriptionId != destroyVmTaskDescriptionId {
				t.Errorf("task description expected: %v, got: %v", destroyVmTaskDescriptionId, destroyTask.Info.DescriptionId)
			}

			// expect the third call to not find the vm and succeed
			if err := reconciler.delete(); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			model.Machine--
			if model.Machine != model.Count().Machine {
				t.Errorf("Unexpected number of machines. Expected: %v, got: %v", model.Machine, model.Count().Machine)
			}
		})
	}

	addDiskToVm := func(ctx context.Context, simVm *simulator.VirtualMachine, diskName string, simClient *vim25.Client) error {
		managedObjRef := simVm.VirtualMachine.Reference()
		vmObj := object.NewVirtualMachine(simClient, managedObjRef)
		devices, err := vmObj.Device(ctx)
		if err != nil {
			return err
		}
		scsi := devices.SelectByType((*types.VirtualSCSIController)(nil))[0]

		additionalDisk := &types.VirtualDisk{
			VirtualDevice: types.VirtualDevice{
				Backing: &types.VirtualDiskFlatVer2BackingInfo{
					DiskMode:        string(types.VirtualDiskModePersistent),
					ThinProvisioned: types.NewBool(true),
					VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
						FileName:  fmt.Sprintf("[LocalDS_0] %s/%s.vmdk", simVm.Name, diskName),
						Datastore: &simVm.Datastore[0],
					},
				},
			},
		}
		additionalDisk.CapacityInKB = 1024
		devices.AssignController(additionalDisk, scsi.(types.BaseVirtualController))

		err = vmObj.AddDevice(ctx, additionalDisk)
		if err != nil {
			return err
		}
		return nil
	}

	extraDisksAttachedTestCases := []struct {
		name        string
		machine     func(t *testing.T, simServerHost string) *machinev1.Machine
		node        func(t *testing.T) *corev1.Node
		attachDisks bool
		errMessage  string
	}{
		{
			name:        "extra disk attached",
			attachDisks: true,
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				return getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
			},
			node: func(t *testing.T) *corev1.Node {
				return getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				})
			},
			errMessage: "additional attached disks detected, block vm destruction and wait for disks to be detached",
		},
		{
			name:        "extra disk attached with no drain annotation",
			attachDisks: true,
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				machine := getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
				machine.ObjectMeta.Annotations = map[string]string{
					machinecontroller.ExcludeNodeDrainingAnnotation: "",
				}
				return machine
			},
			node: func(t *testing.T) *corev1.Node {
				return getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				})
			},
			errMessage: "disks were detached, vm will be attempted to destroy in next reconciliation, requeuing",
		},
		{
			name:        "node status contains attached volumes, node ready",
			attachDisks: true,
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				machine := getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
				return machine
			},
			node: func(t *testing.T) *corev1.Node {
				node := getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				})
				node.Status.VolumesAttached = []corev1.AttachedVolume{
					{
						Name:       "foo",
						DevicePath: "bar",
					},
					{
						Name:       "fizz",
						DevicePath: "bazzz",
					},
				}
				return node
			},
			errMessage: "node is in operational state, won't proceed with pods deletion",
		},
		{
			name:        "node status contains attached volumes, node does not reporting ready",
			attachDisks: true,
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				machine := getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
				return machine
			},
			node: func(t *testing.T) *corev1.Node {
				node := getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionUnknown,
					},
				})
				node.Status.VolumesAttached = []corev1.AttachedVolume{
					{
						Name:       "foo",
						DevicePath: "bar",
					},
					{
						Name:       "fizz",
						DevicePath: "bazzz",
					},
				}
				return node
			},
			errMessage: "node somenodename has attached volumes, requeuing",
		},
		{
			name:        "node status contains attached volumes, but drain skipped",
			attachDisks: true,
			machine: func(t *testing.T, simServerHost string) *machinev1.Machine {
				machine := getMachineWithStatus(t, machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}, simServerHost)
				machine.ObjectMeta.Annotations = map[string]string{
					machinecontroller.ExcludeNodeDrainingAnnotation: "",
				}
				return machine
			},
			node: func(t *testing.T) *corev1.Node {
				node := getNodeWithConditions([]corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionTrue,
					},
				})
				node.Status.VolumesAttached = []corev1.AttachedVolume{
					{
						Name:       "foo",
						DevicePath: "bar",
					},
					{
						Name:       "fizz",
						DevicePath: "bazzz",
					},
				}
				return node
			},
			errMessage: "disks were detached, vm will be attempted to destroy in next reconciliation, requeuing",
		},
	}
	for _, tc := range extraDisksAttachedTestCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			_, sess, srv := initSimulator(t)
			simParams, err := getVcenterSimParams(srv, namespace)
			g.Expect(err).NotTo(HaveOccurred())

			vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
			vm.Config.InstanceUuid = instanceUUID

			if tc.attachDisks {
				simClient := sess.Client.Client
				g.Expect(addDiskToVm(context.TODO(), vm, fmt.Sprintf("%s-%s", vm.Name, tc.name), simClient)).To(Succeed())
			}

			nodeNameIndexExtractor := func(rawObj runtimeclient.Object) []string {
				pod := rawObj.(*corev1.Pod)
				return []string{pod.Spec.NodeName}
			}

			cl := fake.NewClientBuilder().WithScheme(
				scheme.Scheme,
			).WithIndex(&corev1.Pod{}, "spec.nodeName", nodeNameIndexExtractor).WithRuntimeObjects(
				simParams.secret,
				tc.machine(t, simParams.host),
				simParams.configMap,
				simParams.infra,
				tc.node(t),
			).Build()
			mScope, err := newMachineScope(machineScopeParams{
				client:                   cl,
				Context:                  context.Background(),
				machine:                  tc.machine(t, simParams.host),
				apiReader:                cl,
				openshiftConfigNameSpace: openshiftConfigNamespaceForTest,
			})
			g.Expect(err).NotTo(HaveOccurred())

			reconciler := newReconciler(mScope)

			// expect the first call to delete to make the vSphere power off request
			// and always return error to let it reconcile and monitor power off tasks until completion
			g.Expect(reconciler.delete()).To(MatchError(ContainSubstring("powering off vm is in progress, requeuing")))

			// second reconciliation should block vm destruction with an err
			g.Expect(reconciler.delete()).To(MatchError(ContainSubstring(tc.errMessage)))
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
	vmGroup := "testVMGroupName"
	vmName := "testName"
	namespace := "test"
	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	vm.Name = vmName
	vm.Config.Version = minimumHWVersionString

	ctx := context.Background()
	ccrObj, err := session.Finder.ClusterComputeResourceOrDefault(ctx, "/...")
	if err != nil {
		t.Fatalf("error finding simulator cluster object: %v", err)
	}

	resourcePoolInventoryPath := path.Join(ccrObj.InventoryPath, "Resources")

	if err := createVMGroup(ctx, session, ccrObj.Name(), vmGroup); err != nil {
		t.Fatalf("error creating a vmgroup in the simulator: %v", err)
	}

	credentialsSecretName := "test"
	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testConfig := fmt.Sprintf(testConfigFmt, port, credentialsSecretName, namespace)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: openshiftConfigNamespaceForTest,
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

	poolGroup := "ipam.test.io"
	staticIpAddressClaim := []machinev1.NetworkDeviceSpec{
		{
			AddressesFromPools: []machinev1.AddressesFromPool{
				{
					Name:     "test-pool",
					Group:    poolGroup,
					Resource: "ippools",
				},
			},
			Nameservers: []string{
				"192.168.1.100",
			},
			NetworkName: "VM Network",
		},
	}

	staticIpAddressClaimTest3 := []machinev1.NetworkDeviceSpec{
		{
			AddressesFromPools: []machinev1.AddressesFromPool{
				{
					Name:     "test-3-pool",
					Group:    poolGroup,
					Resource: "ippools",
				},
			},
			Nameservers: []string{
				"192.168.1.100",
			},
			NetworkName: "VM Network",
		},
	}
	staticIpAddresses := []machinev1.NetworkDeviceSpec{
		{
			Gateway:     "192.168.1.1",
			IPAddrs:     []string{"192.168.1.2/24"},
			Nameservers: []string{"192.168.1.100"},
			NetworkName: "VM Network",
		},
	}

	ipAddressClaimTest3 := &ipamv1beta1.IPAddressClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-3-claim-0-0",
			Namespace: namespace,
		},
		Spec: ipamv1beta1.IPAddressClaimSpec{
			PoolRef: corev1.TypedLocalObjectReference{
				Name:     "test-3-pool",
				APIGroup: &poolGroup,
				Kind:     "ippools",
			},
		},
		Status: ipamv1beta1.IPAddressClaimStatus{
			AddressRef: corev1.LocalObjectReference{
				Name: "test-test-0",
			},
		},
	}

	ipAddressClaimNoAddress := &ipamv1beta1.IPAddressClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-claim-0-0",
			Namespace: namespace,
		},
		Spec: ipamv1beta1.IPAddressClaimSpec{
			PoolRef: corev1.TypedLocalObjectReference{
				Name:     "test-pool",
				APIGroup: &poolGroup,
				Kind:     "ippools",
			},
		},
	}

	ipAddress := &ipamv1beta1.IPAddress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-test-0",
			Namespace: namespace,
		},
		Spec: ipamv1beta1.IPAddressSpec{
			ClaimRef: corev1.LocalObjectReference{
				Name: "test-test-0",
			},
			PoolRef: corev1.TypedLocalObjectReference{
				Name: "test-pool",
			},
			Address: "192.168.1.11",
			Prefix:  24,
			Gateway: "192.168.1.1",
		},
	}

	cases := []struct {
		name                  string
		machineName           string
		expectedError         error
		providerSpec          machinev1.VSphereMachineProviderSpec
		labels                map[string]string
		notConnectedToVCenter bool
		ipAddressClaim        *ipamv1beta1.IPAddressClaim
		ipAddress             *ipamv1beta1.IPAddress
		featureGatesEnabled   map[string]bool
	}{
		{
			name:        "Successfully create machine",
			machineName: "test-1",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
		},
		{
			name:        "Successfully create machine and assign to vm-host group virtual machine",
			machineName: "test-vmgroup-1",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server:       host,
					VMGroup:      vmGroup,
					ResourcePool: resourcePoolInventoryPath,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereHostVMGroupZonal)] = true
				return fg
			}(),
		},
		{
			name:        "fail to create machine with vm-host group when feature gate is not enabled",
			machineName: "test-vmgroup-2",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server:       host,
					VMGroup:      vmGroup,
					ResourcePool: resourcePoolInventoryPath,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereHostVMGroupZonal)] = false
				return fg
			}(),
			expectedError: errors.New("test-vmgroup-2: vmGroup is only available with the VSphereHostVMGroupZonal feature gate"),
		},
		{
			name:        "fail to create machine with vm-host group when wrong vmGroup is provided",
			machineName: "test-vmgroup-3",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server:       host,
					VMGroup:      "thisgroupdoesnotexist",
					ResourcePool: resourcePoolInventoryPath,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
			},
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereHostVMGroupZonal)] = true
				return fg
			}(),
			expectedError: errors.New("could not update VM Group membership: *types.InvalidArgument"),
		},
		{
			name:        "Fail to create machine with static IP when tech preview not enabled",
			machineName: "test-2",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
				Network: machinev1.NetworkSpec{
					Devices: staticIpAddressClaim,
				},
			},
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereStaticIPs)] = false
				return fg
			}(),
			expectedError: errors.New("test-2: static IP/IPAM configuration is only available with the VSphereStaticIPs feature gate"),
		},
		{
			name:        "Successfully create machine with static IP address claims when tech preview enabled",
			machineName: "test-3",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
				Network: machinev1.NetworkSpec{
					Devices: staticIpAddressClaimTest3,
				},
			},
			ipAddressClaim: ipAddressClaimTest3,
			ipAddress:      ipAddress,
		},
		{
			name:        "Successfully create machine with static IP addresses when tech preview enabled",
			machineName: "test-4",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
				Network: machinev1.NetworkSpec{
					Devices: staticIpAddresses,
				},
			},
		},
		{
			name:        "Failed to create machine with static IP address claim when tech preview enabled due to waiting for IP",
			machineName: "test-5",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				DiskGiB: 10,
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
				Network: machinev1.NetworkSpec{
					Devices: staticIpAddressClaim,
				},
			},
			ipAddressClaim: ipAddressClaimNoAddress,
			expectedError:  errors.New("error getting addresses from IP pool: error retrieving bound IP address: no IPAddress is bound to claim test-5-claim-0-0"),
		},
		{
			name:        "Fail on invalid missing machine label",
			machineName: "test-6",
			labels: map[string]string{
				machinev1.MachineClusterIDLabel: "",
			},
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
			},
			expectedError: errors.New("test-6: failed validating machine provider spec: test-6: missing \"machine.openshift.io/cluster-api-cluster\" label"),
		},
		{
			name:        "Fail on not connected to vCenter",
			machineName: "test-7",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Template: vmName,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
			},
			expectedError:         errors.New("test-7: not connected to a vCenter"),
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

			rawProviderSpec, err := RawExtensionFromProviderSpec(&tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}

			machine := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tc.machineName,
					Namespace: namespace,
					Labels:    labels,
				},
				Spec: machinev1.MachineSpec{
					ProviderSpec: machinev1.ProviderSpec{
						Value: rawProviderSpec,
					},
				},
				Status: machinev1.MachineStatus{},
			}

			builder := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
				credentialsSecret,
				configMap,
				infra,
				userDataSecret,
				machine)
			if tc.ipAddressClaim != nil {
				builder = builder.WithRuntimeObjects(tc.ipAddressClaim)
			}
			if tc.ipAddress != nil {
				builder = builder.WithRuntimeObjects(tc.ipAddress)
			}

			client := builder.Build()

			gates, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			if len(tc.featureGatesEnabled) != 0 {
				if err := gates.SetFromMap(tc.featureGatesEnabled); err != nil {
					t.Errorf("Unexpected error setting feature gates via map: %v", err)
				}
			}

			machineScope, err := newMachineScope(machineScopeParams{
				client:                   client,
				Context:                  context.Background(),
				machine:                  machine,
				apiReader:                client,
				openshiftConfigNameSpace: openshiftConfigNamespaceForTest,
				featureGates:             gates,
			})
			if err != nil {
				t.Fatal(err)
			}

			reconciler := newReconciler(machineScope)

			err = reconciler.create()

			// While debugging the execution of TestCreate it does not
			// get through to powerOn. For vm-host zonal testing
			// we wait for the task to complete and rerun create()
			// which powers on the guest and runs modifyVMGroup.
			// This should only be executed if there is no reconciler error
			if err == nil && tc.providerSpec.Workspace.VMGroup != "" {
				if err := waitForTaskToComplete(session, reconciler); err != nil {
					t.Fatalf("error waiting for simulator task to complete: %v", err)
				}
				err = reconciler.create()
			}

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

				// If IP Claim exists, we need to make sure owner ref is set.
				if tc.ipAddressClaim != nil {
					claimKey := runtimeclient.ObjectKey{
						Namespace: machine.Namespace,
						Name:      tc.ipAddressClaim.Name,
					}
					ipAddressClaim := &ipamv1beta1.IPAddressClaim{}
					err = client.Get(context.Background(), claimKey, ipAddressClaim)
					if err != nil {
						t.Fatal(err)
					} else {
						g := NewWithT(t)

						g.Expect(ipAddressClaim.OwnerReferences).ToNot(BeEmpty())
					}
				}

				// The create task above runs asynchronously so we must wait on it here to prevent early teardown
				// of vCenter simulator.
				if err := waitForTaskToComplete(session, reconciler); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func waitForTaskToComplete(session *session.Session, reconciler *Reconciler) error {
	task, err := session.GetTask(context.TODO(), reconciler.providerStatus.TaskRef)
	if err != nil {
		return err
	}

	taskObj := object.NewTask(session.Client.Client, task.Reference())
	err = taskObj.Wait(context.TODO())
	if err != nil {
		return fmt.Errorf("error waiting for task to complete: %w", err)
	}
	return nil
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

	credentialsSecretName := "test"
	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testConfig := fmt.Sprintf(testConfigFmt, port, credentialsSecretName, namespace)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testName",
			Namespace: openshiftConfigNamespaceForTest,
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

	if _, err := createTagAndCategory(session, tagToCategoryName("CLUSTERID"), "CLUSTERID"); err != nil {
		t.Fatalf("cannot create tag and category: %v", err)
	}

	cases := []struct {
		name          string
		expectedError error
		providerSpec  machinev1.VSphereMachineProviderSpec
		labels        map[string]string
	}{
		{
			name: "Successfully update machine",
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Template: vm.Name,
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
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
			providerSpec: machinev1.VSphereMachineProviderSpec{
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Template: vm.Name,
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
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

			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
				credentialsSecret,
				configMap,
				infra).Build()

			rawProviderSpec, err := RawExtensionFromProviderSpec(&tc.providerSpec)
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
				apiReader:                client,
				openshiftConfigNameSpace: openshiftConfigNamespaceForTest,
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
	withPoweredOffVMS := func() simulatorModelOption {
		return func(m *simulator.Model) {
			m.Autostart = false
		}
	}

	model, server := initSimulatorCustom(t, withPoweredOffVMS())
	session := getSimulatorSession(t, server)
	defer model.Remove()
	defer server.Close()
	credentialsSecretUsername := fmt.Sprintf("%s.username", server.URL.Host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", server.URL.Host)

	password, _ := server.URL.User.Password()
	namespace := "test"
	VMs := simulator.Map.All("VirtualMachine")
	poweredOffVM := VMs[0].(*simulator.VirtualMachine)
	poweredOnVM := VMs[1].(*simulator.VirtualMachine)

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

	vmObj := object.NewVirtualMachine(session.Client.Client, poweredOnVM.Reference())
	task, err := vmObj.PowerOn(context.TODO())
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name          string
		machinePhase  string
		instanceState string
		exists        bool
		vmExists      bool
		vm            *simulator.VirtualMachine
	}{
		{
			name:          "VM doesn't exist",
			machinePhase:  "Provisioning",
			instanceState: "",
			exists:        false,
			vmExists:      false,
		},
		{
			name:          "VM already exists",
			machinePhase:  "Provisioning",
			instanceState: string(types.VirtualMachinePowerStatePoweredOn),
			exists:        true,
			vmExists:      true,
			vm:            poweredOnVM,
		},
		{
			name:          "VM exists but didnt powered on after clone",
			machinePhase:  "Provisioning",
			instanceState: string(types.VirtualMachinePowerStatePoweredOff),
			exists:        false,
			vmExists:      true,
			vm:            poweredOffVM,
		},
		{
			name:          "VM exists, but powered off",
			machinePhase:  "Provisioned",
			instanceState: string(types.VirtualMachinePowerStatePoweredOff),
			exists:        true,
			vmExists:      true,
			vm:            poweredOffVM,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			var name, uuid string
			if tc.vm != nil {
				name = tc.vm.Name
				uuid = tc.vm.Config.InstanceUuid
			}

			machineObj := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						machinev1.MachineClusterIDLabel: "CLUSTERID",
					},
				},
				Status: machinev1.MachineStatus{
					Phase: &tc.machinePhase,
				},
			}

			machineScope := machineScope{
				Context:            context.TODO(),
				machine:            machineObj,
				machineToBePatched: runtimeclient.MergeFrom(machineObj.DeepCopy()),
				providerSpec: &machinev1.VSphereMachineProviderSpec{
					Template: name,
				},
				session: session,
				providerStatus: &machinev1.VSphereMachineProviderStatus{
					TaskRef:       task.Reference().Value,
					InstanceState: &tc.instanceState,
				},
				client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(&credentialsSecret, machineObj).WithStatusSubresource(machineObj).Build(),
			}

			reconciler := newReconciler(&machineScope)

			if tc.vmExists {
				reconciler.machine.UID = apimachinerytypes.UID(uuid)
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

	cluster := simulator.Map.Any("ClusterComputeResource").(*simulator.ClusterComputeResource)
	dc := simulator.Map.Any("Datacenter").(*simulator.Datacenter)

	if _, err := createTagAndCategory(session, zoneKey, testZone); err != nil {
		t.Fatalf("cannot create tag and category: %v", err)
	}

	if _, err := createTagAndCategory(session, regionKey, testRegion); err != nil {
		t.Fatalf("cannot create tag and category: %v", err)
	}

	if err := session.WithRestClient(context.TODO(), func(c *rest.Client) error {
		tagsMgr := tags.NewManager(c)

		err = tagsMgr.AttachTag(context.TODO(), testZone, cluster.Reference())
		if err != nil {
			return err
		}

		err = tagsMgr.AttachTag(context.TODO(), testRegion, dc.Reference())
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
		providerSpec: &machinev1.VSphereMachineProviderSpec{
			Template: vm.Name,
			Network: machinev1.NetworkSpec{
				Devices: []machinev1.NetworkDeviceSpec{
					{
						NetworkName: "test",
					},
				},
			},
		},
		session: session,
		providerStatus: &machinev1.VSphereMachineProviderStatus{
			TaskRef: task.Reference().Value,
		},
		vSphereConfig: &vsphere.Config{
			Labels: vsphere.Labels{
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

func createTagAndCategory(session *session.Session, categoryName, tagName string) (string, error) {
	var tagID string
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

		tagID, err = tagsMgr.CreateTag(context.TODO(), &tags.Tag{
			CategoryID: id,
			Name:       tagName,
		})
		if err != nil {
			return err
		}

		return nil
	}); err != nil {
		return "", err
	}

	return tagID, nil
}

func TestVmDisksManipulation(t *testing.T) {
	ctx := context.Background()

	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	simulatorVM := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)
	managedObjRef := simulatorVM.VirtualMachine.Reference()
	vmObj := object.NewVirtualMachine(session.Client.Client, managedObjRef)
	machineObj := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "myCoolMachine",
		},
	}

	addDiskToVm := func(vm *object.VirtualMachine, diskName string, gmgAssert *GomegaWithT) {
		devices, err := vm.Device(ctx)
		gmgAssert.Expect(err).ToNot(HaveOccurred())
		scsi := devices.SelectByType((*types.VirtualSCSIController)(nil))[0]

		additionalDisk := &types.VirtualDisk{
			VirtualDevice: types.VirtualDevice{
				Backing: &types.VirtualDiskFlatVer2BackingInfo{
					DiskMode:        string(types.VirtualDiskModePersistent),
					ThinProvisioned: types.NewBool(true),
					VirtualDeviceFileBackingInfo: types.VirtualDeviceFileBackingInfo{
						FileName:  fmt.Sprintf("[LocalDS_0] %s/%s.vmdk", simulatorVM.Name, diskName),
						Datastore: &simulatorVM.Datastore[0],
					},
				},
			},
		}
		additionalDisk.CapacityInKB = 1024
		devices.AssignController(additionalDisk, scsi.(types.BaseVirtualController))

		err = vm.AddDevice(ctx, additionalDisk)
		gmgAssert.Expect(err).ToNot(HaveOccurred())
	}

	vm := &virtualMachine{
		Context: ctx,
		Obj:     vmObj,
		Ref:     managedObjRef,
	}

	addDiskToVm(vmObj, machineObj.Name, NewWithT(t))
	addDiskToVm(vmObj, "foo", NewWithT(t))

	t.Run("Test getAttachedDisks", func(t *testing.T) {
		g := NewWithT(t)

		disks, err := vm.getAttachedDisks()
		g.Expect(err).ToNot(HaveOccurred())

		g.Expect(disks[0].fileName).Should(ContainSubstring("disk1.vmdk")) // sim default
		g.Expect(disks[1].fileName).Should(ContainSubstring("myCoolMachine.vmdk"))
		g.Expect(disks[2].fileName).Should(ContainSubstring("foo.vmdk"))
	})

	t.Run("Test filter OS disk", func(t *testing.T) {
		mockDisksTestCases := []struct {
			name              string
			vmdkFilenames     []string
			expectedFilenames []string
		}{
			{
				name: "No disks",
			},
			{
				name: "filename is suffixed with machine name",
				vmdkFilenames: []string{
					fmt.Sprintf("[DS] foo/foo-%s.vmdk", machineObj.Name),
				},
				expectedFilenames: []string{
					fmt.Sprintf("[DS] foo/foo-%s.vmdk", machineObj.Name),
				},
			},
			{
				name: "filename pointing to the DS root",
				vmdkFilenames: []string{
					fmt.Sprintf("[DS] %s.vmdk", machineObj.Name),
				},
				expectedFilenames: []string{
					fmt.Sprintf("[DS] %s.vmdk", machineObj.Name),
				},
			},
			{
				name: "vmdk name equals to machine name should be filtered out",
				vmdkFilenames: []string{
					fmt.Sprintf("[DS] foo/%s.vmdk", machineObj.Name),
				},
			},
			{
				name: "vmdk name equals to machine name should be filtered out",
				vmdkFilenames: []string{
					"[DS] foo.vmdk",
					fmt.Sprintf("[DS] foo/%s.vmdk", machineObj.Name),
					"[DS] bar.vmdk",
					"some nonsense",
				},
				expectedFilenames: []string{
					"[DS] foo.vmdk",
					"[DS] bar.vmdk",
					"some nonsense",
				},
			},
			{
				name: "multiple vmdk names with machine name should be filtered out",
				vmdkFilenames: []string{
					"[DS] foo.vmdk",
					fmt.Sprintf("[DS] foo/%s.vmdk", machineObj.Name),
					fmt.Sprintf("[DS] foo/%s_1.vmdk", machineObj.Name),
					"[DS] bar.vmdk",
					"some nonsense",
				},
				expectedFilenames: []string{
					"[DS] foo.vmdk",
					"[DS] bar.vmdk",
					"some nonsense",
				},
			},
		}
		for _, tc := range mockDisksTestCases {
			t.Run(tc.name, func(t *testing.T) {
				g := NewWithT(t)

				mockedDisksSlice := *new([]attachedDisk)
				for _, filename := range tc.vmdkFilenames {
					mockedDisksSlice = append(mockedDisksSlice, attachedDisk{fileName: filename})
				}
				actualDisksSlice := filterOutVmOsDisk(mockedDisksSlice, machineObj)
				g.Expect(len(actualDisksSlice)).To(Equal(len(tc.expectedFilenames)))

				filteredDisksFilenames := *new([]string)
				for _, mockedDisk := range actualDisksSlice {
					filteredDisksFilenames = append(filteredDisksFilenames, mockedDisk.fileName)
				}
				g.Expect(filteredDisksFilenames).To(BeComparableTo(tc.expectedFilenames))
			})
		}
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
			name: "Successfully reconcile annotation",
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
