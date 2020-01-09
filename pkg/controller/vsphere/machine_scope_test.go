package vsphere

import (
	"bytes"
	"context"
	"testing"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	apivsphere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	vsphereapi "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const TestNamespace = "vsphere-test"

func MachineWithSpec(spec *apivsphere.VSphereMachineProviderSpec) *machinev1.Machine {
	rawSpec, err := vsphereapi.RawExtensionFromProviderSpec(spec)
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

	defaultProviderSpec := &apivsphere.VSphereMachineProviderSpec{
		UserDataSecret: &corev1.LocalObjectReference{
			Name: userDataSecretName,
		},
	}

	testCases := []struct {
		testCase         string
		userDataSecret   *corev1.Secret
		providerSpec     *apivsphere.VSphereMachineProviderSpec
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
			expectError:      false,
			expectedUserdata: nil,
		},
		{
			testCase:         "no user-data in provider spec",
			userDataSecret:   nil,
			providerSpec:     &apivsphere.VSphereMachineProviderSpec{},
			expectError:      false,
			expectedUserdata: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			clientObjs := []runtime.Object{}

			if tc.userDataSecret != nil {
				clientObjs = append(clientObjs, tc.userDataSecret)
			}

			client := fake.NewFakeClient(clientObjs...)

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

			if bytes.Compare(userData, tc.expectedUserdata) != 0 {
				t.Errorf("Got: %q, Want: %q", userData, tc.expectedUserdata)
			}
		})
	}
}

func TestGetCredentialsSecret(t *testing.T) {
	expectedUser := "user"
	expectedPassword := "password"
	testCases := []struct {
		testCase          string
		secret            *corev1.Secret
		providerSpec      *apivsphere.VSphereMachineProviderSpec
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
					credentialsSecretUser:     []byte(expectedUser),
					credentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec: &apivsphere.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
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
					credentialsSecretUser:     []byte(expectedUser),
					credentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec: &apivsphere.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "secret-does-not-exist",
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
					"badUserKey":              []byte(expectedUser),
					credentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec: &apivsphere.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
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
					credentialsSecretUser: []byte(expectedUser),
					"badPasswordKey":      []byte(expectedPassword),
				},
			},
			providerSpec: &apivsphere.VSphereMachineProviderSpec{
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
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
					credentialsSecretUser:     []byte(expectedUser),
					credentialsSecretPassword: []byte(expectedPassword),
				},
			},
			providerSpec:      &apivsphere.VSphereMachineProviderSpec{},
			expectError:       false,
			expectCredentials: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			client := fake.NewFakeClientWithScheme(scheme.Scheme, tc.secret)
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
	model, _, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	// fake objects for newMachineScope()
	password, _ := server.URL.User.Password()
	namespace := "test"
	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUser:     []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	// original objects
	originalProviderSpec := apivsphere.VSphereMachineProviderSpec{
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: "test",
		},

		Workspace: &apivsphere.Workspace{
			Server: server.URL.Host,
			Folder: "test",
		},
	}
	rawProviderSpec, err := apivsphere.RawExtensionFromProviderSpec(&originalProviderSpec)
	if err != nil {
		t.Fatal(err)
	}

	originalProviderStatus := &apivsphere.VSphereMachineProviderStatus{
		TaskRef: "test",
	}
	rawProviderStatus, err := apivsphere.RawExtensionFromProviderStatus(originalProviderStatus)
	if err != nil {
		t.Fatal(err)
	}

	originalMachine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
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

	// expected objects
	expectedMachine := originalMachine.DeepCopy()
	providerID := "mutated"
	expectedMachine.Spec.ProviderID = &providerID
	expectedMachine.Status.Addresses = []corev1.NodeAddress{
		{
			Type:    corev1.NodeInternalDNS,
			Address: "127.0.0.1",
		},
	}
	expectedProviderStatus := &apivsphere.VSphereMachineProviderStatus{
		TaskRef: "mutated",
	}
	rawProviderStatus, err = apivsphere.RawExtensionFromProviderStatus(expectedProviderStatus)
	if err != nil {
		t.Fatal(err)
	}
	expectedMachine.Status.ProviderStatus = rawProviderStatus

	// machineScope
	if err := machinev1.AddToScheme(scheme.Scheme); err != nil {
		t.Fatal(err)
	}
	fakeClient := fake.NewFakeClientWithScheme(scheme.Scheme, credentialsSecret, originalMachine)
	machineScope, err := newMachineScope(machineScopeParams{
		client:  fakeClient,
		Context: context.TODO(),
		machine: originalMachine,
	})
	if err != nil {
		t.Fatal(err)
	}

	// mutations
	machineScope.machine.Spec.ProviderID = expectedMachine.Spec.ProviderID
	machineScope.machine.Status.Addresses = expectedMachine.Status.Addresses
	machineScope.providerStatus = expectedProviderStatus

	if err := machineScope.PatchMachine(); err != nil {
		t.Errorf("unexpected error")
	}
	gotMachine := &machinev1.Machine{}
	if err := machineScope.client.Get(context.TODO(), runtimeclient.ObjectKey{Name: "test", Namespace: namespace}, gotMachine); err != nil {
		t.Fatal(err)
	}

	if !equality.Semantic.DeepEqual(gotMachine, expectedMachine) {
		t.Errorf("expected: %+v, got: %+v", expectedMachine, gotMachine)
	}
}
