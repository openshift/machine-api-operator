package vsphere

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const TestNamespace = "vsphere-test"

func MachineWithSpec(spec *machinev1.VSphereMachineProviderSpec) *machinev1.Machine {
	rawSpec, err := RawExtensionFromProviderSpec(spec)
	if err != nil {
		panic("Failed to encode raw extension from provider spec")
	}

	return &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vsphere-test",
			Namespace: TestNamespace,
		},
		Spec: machinev1.MachineSpec{
			ProviderSpec: machinev1.ProviderSpec{
				Value: rawSpec,
			},
		},
	}
}

func TestGetUserData(t *testing.T) {
	userDataSecretName := "vsphere-ignition"

	defaultProviderSpec := &machinev1.VSphereMachineProviderSpec{
		UserDataSecret: &corev1.LocalObjectReference{
			Name: userDataSecretName,
		},
	}

	testCases := []struct {
		testCase         string
		userDataSecret   *corev1.Secret
		providerSpec     *machinev1.VSphereMachineProviderSpec
		expectedUserdata []byte
		expectError      bool
	}{
		{
			testCase: "all good",
			userDataSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      userDataSecretName,
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					userDataSecretKey: []byte("{}"),
				},
			},
			providerSpec:     defaultProviderSpec,
			expectedUserdata: []byte("{}"),
			expectError:      false,
		},
		{
			testCase:       "missing secret",
			userDataSecret: nil,
			providerSpec:   defaultProviderSpec,
			expectError:    true,
		},
		{
			testCase: "missing key in secret",
			userDataSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      userDataSecretName,
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					"badKey": []byte("{}"),
				},
			},
			providerSpec: defaultProviderSpec,
			expectError:  true,
		},
		{
			testCase:         "no provider spec",
			userDataSecret:   nil,
			providerSpec:     nil,
			expectError:      true,
			expectedUserdata: nil,
		},
		{
			testCase:         "no user-data in provider spec",
			userDataSecret:   nil,
			providerSpec:     &machinev1.VSphereMachineProviderSpec{},
			expectError:      true,
			expectedUserdata: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			clientObjs := []runtime.Object{}

			if tc.userDataSecret != nil {
				clientObjs = append(clientObjs, tc.userDataSecret)
			}

			client := fake.NewClientBuilder().WithRuntimeObjects(clientObjs...).Build()

			// Can't use newMachineScope because it tries to create an API
			// session, and other things unrelated to these tests.
			ms := &machineScope{
				Context:      context.Background(),
				client:       client,
				machine:      MachineWithSpec(tc.providerSpec),
				providerSpec: tc.providerSpec,
			}

			userData, err := ms.GetUserData()
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if !bytes.Equal(userData, tc.expectedUserdata) {
				t.Errorf("Got: %q, Want: %q", userData, tc.expectedUserdata)
			}
		})
	}
}

func TestGetCredentialsSecret(t *testing.T) {
	expectedUser := "user"
	expectedPassword := "password"
	expectedServer := "test-server"
	expectedCredentialsSecretUsername := fmt.Sprintf("%s.username", expectedServer)
	expectedCredentialsSecretPassword := fmt.Sprintf("%s.password", expectedServer)
	testCases := []struct {
		testCase          string
		secret            *corev1.Secret
		providerSpec      *machinev1.VSphereMachineProviderSpec
		expectError       bool
		expectCredentials bool
	}{
		{
			testCase: "all good",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					expectedCredentialsSecretUsername: []byte(expectedUser),
					expectedCredentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: expectedServer,
				},
			},
			expectCredentials: true,
		},
		{
			testCase: "secret does not exist",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					expectedCredentialsSecretUsername: []byte(expectedUser),
					expectedCredentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "secret-does-not-exist",
				},
				Workspace: &machinev1.Workspace{
					Server: expectedServer,
				},
			},
			expectError: true,
		},
		{
			testCase: "bad user secret data key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					"badUserKey":                      []byte(expectedUser),
					expectedCredentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: expectedServer,
				},
			},
			expectError: true,
		},
		{
			testCase: "bad password secret data key",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					expectedCredentialsSecretUsername: []byte(expectedUser),
					"badPasswordKey":                  []byte(expectedPassword),
				},
			},
			providerSpec: &machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				Workspace: &machinev1.Workspace{
					Server: expectedServer,
				},
			},
			expectError: true,
		},
		{
			testCase: "no credentials secret ref",
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					expectedCredentialsSecretUsername: []byte(expectedUser),
					expectedCredentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec:      &machinev1.VSphereMachineProviderSpec{},
			expectError:       false,
			expectCredentials: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tc.secret).Build()
			gotUser, gotPassword, err := getCredentialsSecret(client, TestNamespace, *tc.providerSpec)
			if (err != nil) != tc.expectError {
				t.Errorf("Expected error: %v, got %v", tc.expectError, err)
			}

			if !tc.expectError && tc.expectCredentials {
				if gotUser != expectedUser {
					t.Errorf("Expected user: %v, got %v", expectedUser, gotUser)
				}
				if gotPassword != expectedPassword {
					t.Errorf("Expected password: %v, got %v", expectedPassword, gotPassword)
				}
			}
		})
	}
}

func TestPatchMachine(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1")},
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	k8sClient, err := client.New(cfg, client.Options{})
	g.Expect(err).ToNot(HaveOccurred())

	model, _, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()
	host, port, err := net.SplitHostPort(server.URL.Host)
	if err != nil {
		t.Fatal(err)
	}

	credentialsSecretUsername := fmt.Sprintf("%s.username", host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", host)

	// fake objects for newMachineScope()
	password, _ := server.URL.User.Password()

	configNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: openshiftConfigNamespace,
		},
	}
	g.Expect(k8sClient.Create(ctx, configNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, configNamespace)).To(Succeed())
	}()

	testNamespaceName := "test"

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespaceName,
		},
	}
	g.Expect(k8sClient.Create(ctx, testNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, testNamespace)).To(Succeed())
	}()

	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: testNamespaceName,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	g.Expect(k8sClient.Create(ctx, credentialsSecret)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, credentialsSecret)).To(Succeed())
	}()

	testConfig := fmt.Sprintf(testConfigFmt, port)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testname",
			Namespace: openshiftConfigNamespace,
		},
		Data: map[string]string{
			"testkey": testConfig,
		},
	}
	g.Expect(k8sClient.Create(ctx, configMap)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, configMap)).To(Succeed())
	}()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
		Spec: configv1.InfrastructureSpec{
			CloudConfig: configv1.ConfigMapFileReference{
				Name: "testname",
				Key:  "testkey",
			},
		},
	}
	g.Expect(k8sClient.Create(ctx, infra)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, infra)).To(Succeed())
	}()

	failedPhase := "Failed"

	providerStatus := &machinev1.VSphereMachineProviderStatus{}

	machineName := "test"
	machineKey := types.NamespacedName{Namespace: testNamespaceName, Name: machineName}

	testCases := []struct {
		name   string
		mutate func(*machinev1.Machine)
		expect func(*machinev1.Machine) error
	}{
		{
			name: "Test changing labels",
			mutate: func(m *machinev1.Machine) {
				if m.Labels == nil {
					m.Labels = map[string]string{}
				}
				m.Labels["testlabel"] = "test"
			},
			expect: func(m *machinev1.Machine) error {
				if m.Labels["testlabel"] != "test" {
					return fmt.Errorf("label \"testlabel\" %q not equal expected \"test\"", m.ObjectMeta.Labels["test"])
				}
				return nil
			},
		},
		{
			name: "Test setting phase",
			mutate: func(m *machinev1.Machine) {
				m.Status.Phase = &failedPhase
			},
			expect: func(m *machinev1.Machine) error {
				if m.Status.Phase != nil && *m.Status.Phase == failedPhase {
					return nil
				}
				return fmt.Errorf("phase is nil or not equal expected \"Failed\"")
			},
		},
		{
			name: "Test setting provider status",
			mutate: func(m *machinev1.Machine) {
				instanceID := "123"
				instanceState := "running"
				providerStatus.InstanceID = &instanceID
				providerStatus.InstanceState = &instanceState
			},
			expect: func(m *machinev1.Machine) error {
				providerStatus, err := ProviderStatusFromRawExtension(m.Status.ProviderStatus)
				if err != nil {
					return fmt.Errorf("unable to get provider status: %v", err)
				}

				if providerStatus.InstanceID == nil || *providerStatus.InstanceID != "123" {
					return fmt.Errorf("instanceID is nil or not equal expected \"123\"")
				}

				if providerStatus.InstanceState == nil || *providerStatus.InstanceState != "running" {
					return fmt.Errorf("instanceState is nil or not equal expected \"running\"")
				}

				return nil
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)
			timeout := 10 * time.Second

			// original objects
			originalProviderSpec := machinev1.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},

				Workspace: &machinev1.Workspace{
					Server: host,
					Folder: "test",
				},
			}
			rawProviderSpec, err := RawExtensionFromProviderSpec(&originalProviderSpec)
			gs.Expect(err).ToNot(HaveOccurred())
			originalProviderStatus := &machinev1.VSphereMachineProviderStatus{
				TaskRef: "test",
			}
			rawProviderStatus, err := RawExtensionFromProviderStatus(originalProviderStatus)
			gs.Expect(err).ToNot(HaveOccurred())

			machine := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      machineName,
					Namespace: testNamespaceName,
					Labels:    map[string]string{},
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "Machine",
					APIVersion: "machine.openshift.io/v1beta1",
				},
				Spec: machinev1.MachineSpec{
					ProviderSpec: machinev1.ProviderSpec{
						Value: rawProviderSpec,
					},
				},
				Status: machinev1.MachineStatus{
					ProviderStatus: rawProviderStatus,
				},
			}

			// Create the machine
			gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
			defer func() {
				gs.Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
			}()

			// Ensure the machine has synced to the cache
			getMachine := func() error {

				return k8sClient.Get(ctx, machineKey, machine)
			}
			gs.Eventually(getMachine, timeout).Should(Succeed())

			machineScope, err := newMachineScope(machineScopeParams{
				client:    k8sClient,
				machine:   machine,
				apiReader: k8sClient,
				Context:   ctx,
			})

			gs.Expect(err).ToNot(HaveOccurred())

			tc.mutate(machineScope.machine)

			machineScope.providerStatus = providerStatus

			// Patch the machine and check the expectation from the test case
			gs.Expect(machineScope.PatchMachine()).To(Succeed())
			checkExpectation := func() error {
				if err := getMachine(); err != nil {
					return err
				}
				return tc.expect(machine)
			}
			gs.Eventually(checkExpectation, timeout).Should(Succeed())

			// Check that resource version doesn't change if we call patchMachine() again
			machineResourceVersion := machine.ResourceVersion

			gs.Expect(machineScope.PatchMachine()).To(Succeed())
			gs.Eventually(getMachine, timeout).Should(Succeed())
			gs.Expect(machine.ResourceVersion).To(Equal(machineResourceVersion))
		})
	}
}

func TestNodeGetter(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1")},
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	k8sClient, err := client.New(cfg, client.Options{})
	g.Expect(err).ToNot(HaveOccurred())

	nodeName := "somenodename"
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nodeName,
			Namespace: metav1.NamespaceNone,
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
	g.Expect(k8sClient.Create(ctx, node)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, node)).To(Succeed())
	}()

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: TestNamespace,
		},
	}
	g.Expect(k8sClient.Create(ctx, testNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, testNamespace)).To(Succeed())
	}()

	machineName := "somemachine"
	machineKey := types.NamespacedName{Namespace: TestNamespace, Name: machineName}
	machine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName,
			Namespace: TestNamespace,
			Labels:    map[string]string{},
		},
		TypeMeta: metav1.TypeMeta{
			Kind:       "Machine",
			APIVersion: "machine.openshift.io/v1beta1",
		},
	}
	g.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
	}()

	// Ensure the machine has synced to the cache
	getMachine := func() error {
		return k8sClient.Get(ctx, machineKey, machine)
	}
	g.Eventually(getMachine, 10*time.Second).Should(Succeed())

	machineScope := &machineScope{
		Context:      ctx,
		client:       k8sClient,
		apiReader:    k8sClient,
		machine:      machine,
		providerSpec: &machinev1.VSphereMachineProviderSpec{},
	}

	resetStatuses := func(node *corev1.Node, machine *machinev1.Machine) {
		machine.Status = machinev1.MachineStatus{}
		node.Status = corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
		}
	}

	////////////// getNode test cases

	nodeGetterTestCases := []struct {
		name        string
		err         error
		node        *corev1.Node
		setStatuses func(node *corev1.Node, machine *machinev1.Machine)
	}{
		{
			name:        "getNode: No node linked",
			err:         fmt.Errorf("NodeRef empty, unable to get related Node"),
			node:        nil,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {},
		},
		{
			name: "getNode: Node linked",
			err:  nil,
			node: node,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {
				machine.Status = machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}
			},
		},
	}
	for _, tc := range nodeGetterTestCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)
			tc.setStatuses(node, machine)
			defer resetStatuses(node, machine)

			node, err := machineScope.getNode()
			if tc.err != nil {
				gs.Expect(err).To(Equal(tc.err))
			} else {
				gs.Expect(err).To(BeNil())
			}
			gs.Expect(node).To(Equal(tc.node))
		})
	}

	////////////// isNodeLinked test cases

	isNodeLinkedTestCases := []struct {
		name        string
		expected    bool
		setStatuses func(node *corev1.Node, machine *machinev1.Machine)
	}{
		{
			name:        "isNodeLinked: No node linked",
			expected:    false,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {},
		},
		{
			name:     "isNodeLinked: Node name empty",
			expected: false,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {
				machine.Status = machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: "",
					},
				}
			},
		},
		{
			name:     "isNodeLinked: Node ref filled",
			expected: true,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {
				machine.Status = machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}
			},
		},
	}
	for _, tc := range isNodeLinkedTestCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			tc.setStatuses(node, machine)
			defer resetStatuses(node, machine)

			isNodeLinked := machineScope.isNodeLinked()

			gs.Expect(isNodeLinked).To(Equal(tc.expected))
		})
	}

	////////////// checkNodeReachable test cases

	checkNodeReachableTestCases := []struct {
		name        string
		err         error
		expected    bool
		setStatuses func(node *corev1.Node, machine *machinev1.Machine)
	}{
		{
			name:        "checkNodeReachable: node not linked",
			err:         fmt.Errorf("NodeRef empty, unable to get related Node"),
			expected:    false,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {},
		},
		{
			name:     "checkNodeReachable: node reachable",
			err:      nil,
			expected: true,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {
				machine.Status = machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}
				node.Status = corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
			},
		},
		{
			name:     "checkNodeReachable: node unreachable",
			err:      nil,
			expected: false,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {
				machine.Status = machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: nodeName,
					},
				}
				node.Status = corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionUnknown,
						},
					},
				}
				g.Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
			},
		},
		{
			name:     "checkNodeReachable: node not found",
			err:      nil,
			expected: false,
			setStatuses: func(node *corev1.Node, machine *machinev1.Machine) {
				machine.Status = machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: "not-exists",
					},
				}
			},
		},
	}
	for _, tc := range checkNodeReachableTestCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)
			tc.setStatuses(node, machine)
			defer resetStatuses(node, machine)

			isNodeAvailable, err := machineScope.checkNodeReachable()

			if tc.err != nil {
				gs.Expect(err).To(Equal(tc.err))
			} else {
				gs.Expect(err).To(BeNil())
			}
			gs.Expect(isNodeAvailable).To(Equal(tc.expected))
		})
	}
}
