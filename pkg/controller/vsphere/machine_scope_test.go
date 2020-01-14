package vsphere

import (
	"bytes"
	"context"
	"testing"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	apivsphere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	vsphereapi "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
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
	ignitionSecretName := "vsphere-ignition"

	defaultProviderSpec := &apivsphere.VSphereMachineProviderSpec{
		IgnitionSecret: &corev1.LocalObjectReference{
			Name: ignitionSecretName,
		},
	}

	testCases := []struct {
		testCase         string
		ignitionSecret   *corev1.Secret
		providerSpec     *apivsphere.VSphereMachineProviderSpec
		expectedIgnition []byte
		expectError      bool
	}{
		{
			testCase: "all good",
			ignitionSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ignitionSecretName,
					Namespace: TestNamespace,
				},
				Data: map[string][]byte{
					ignitionSecretKey: []byte("{}"),
				},
			},
			providerSpec:     defaultProviderSpec,
			expectedIgnition: []byte("{}"),
			expectError:      false,
		},
		{
			testCase:       "missing secret",
			ignitionSecret: nil,
			providerSpec:   defaultProviderSpec,
			expectError:    true,
		},
		{
			testCase: "missing key in secret",
			ignitionSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ignitionSecretName,
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
			ignitionSecret:   nil,
			providerSpec:     nil,
			expectError:      false,
			expectedIgnition: nil,
		},
		{
			testCase:         "no user-data in provider spec",
			ignitionSecret:   nil,
			providerSpec:     &apivsphere.VSphereMachineProviderSpec{},
			expectError:      false,
			expectedIgnition: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			clientObjs := []runtime.Object{}

			if tc.ignitionSecret != nil {
				clientObjs = append(clientObjs, tc.ignitionSecret)
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

			ignition, err := ms.GetIgnitionData()
			if !tc.expectError && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if bytes.Compare(ignition, tc.expectedIgnition) != 0 {
				t.Errorf("Got: %q, Want: %q", ignition, tc.expectedIgnition)
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
