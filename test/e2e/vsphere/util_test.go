package vsphere

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetVSphereCredentialsSecret(t *testing.T) {
	t.Parallel()

	const (
		wantName      = "vsphere-cloud-credentials"
		wantNamespace = "openshift-machine-api"
	)

	want := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      wantName,
			Namespace: wantNamespace,
		},
	}
	client := fake.NewSimpleClientset(want)

	got, err := getVSphereCredentialsSecret(context.Background(), client)
	if err != nil {
		t.Fatalf("unexpected error getting vSphere credentials Secret: %v", err)
	}
	if got.Name != wantName {
		t.Errorf("got Secret name %q, want %q", got.Name, wantName)
	}
	if got.Namespace != wantNamespace {
		t.Errorf("got Secret namespace %q, want %q", got.Namespace, wantNamespace)
	}
}

func TestGetVSphereCredentialsSecretNotFound(t *testing.T) {
	t.Parallel()

	client := fake.NewSimpleClientset()
	_, err := getVSphereCredentialsSecret(context.Background(), client)
	if err == nil {
		t.Fatal("expected an error when the vSphere credentials Secret does not exist")
	}
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected a NotFound error, got %v", err)
	}
}

func TestGetCredentialsForVCenter(t *testing.T) {
	t.Parallel()

	const server = "vcenter.example.com"
	usernameKey := server + ".username"
	passwordKey := server + ".password"
	vCenter := configv1.VSpherePlatformVCenterSpec{Server: server}

	testCases := []struct {
		name        string
		data        map[string][]byte
		wantUser    string
		wantPass    string
		wantErrText string
	}{
		{
			name: "server-qualified keys",
			data: map[string][]byte{
				usernameKey: []byte("username-value"),
				passwordKey: []byte("password-value"),
			},
			wantUser: "username-value",
			wantPass: "password-value",
		},
		{
			name: "missing server-qualified username key",
			data: map[string][]byte{
				passwordKey: []byte("password-value"),
			},
			wantErrText: "unable to find username in secret",
		},
		{
			name: "missing server-qualified password key",
			data: map[string][]byte{
				usernameKey: []byte("username-value"),
			},
			wantErrText: "unable to find password in secret",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			secret := &corev1.Secret{Data: tc.data}
			gotUser, gotPass, err := getCredentialsForVCenter(context.Background(), secret, vCenter)
			if tc.wantErrText != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErrText)
				}
				if err.Error() != tc.wantErrText {
					t.Errorf("got error %q, want %q", err, tc.wantErrText)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error parsing vSphere credentials: %v", err)
			}
			if gotUser != tc.wantUser {
				t.Errorf("got username %q, want %q", gotUser, tc.wantUser)
			}
			if gotPass != tc.wantPass {
				t.Errorf("got password %q, want %q", gotPass, tc.wantPass)
			}
		})
	}
}
