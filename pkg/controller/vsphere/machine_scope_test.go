package vsphere

import (
	"testing"

	apivsphere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetCredentialsSecret(t *testing.T) {
	namespace := "test"
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
					Namespace: namespace,
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
					Namespace: namespace,
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
					Namespace: namespace,
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
					Namespace: namespace,
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
					Namespace: namespace,
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
			gotUser, gotPassword, err := getCredentialsSecret(client, namespace, *tc.providerSpec)
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
