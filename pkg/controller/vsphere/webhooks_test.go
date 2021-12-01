package vsphere

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	mapiwebhooks "github.com/openshift/machine-api-operator/pkg/webhooks"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var (
	clusterID          = "vsphere-cluster"
	insecureHTTPClient = http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
)

func TestWebhooksOnCreate(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1")},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			MutatingWebhooks:   []*admissionregistrationv1.MutatingWebhookConfiguration{mapiwebhooks.NewMutatingWebhookConfiguration()},
			ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{mapiwebhooks.NewValidatingWebhookConfiguration()},
		},
	}

	cfg, err := testEnv.Start()
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	k8sClient, err := client.New(cfg, client.Options{})
	g.Expect(err).ToNot(HaveOccurred())

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creation-test",
		},
	}
	vSphereSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultVSphereCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
	}
	infraPatch := client.MergeFrom(infra.DeepCopy())

	g.Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	g.Expect(k8sClient.Create(ctx, vSphereSecret)).To(Succeed())
	g.Expect(k8sClient.Create(ctx, infra)).To(Succeed())
	infra.Status.InfrastructureName = clusterID
	g.Expect(k8sClient.Status().Patch(ctx, infra, infraPatch)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, vSphereSecret)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, infra)).To(Succeed())
	}()

	testCases := []struct {
		name              string
		expectedError     string
		presetClusterID   bool
		providerSpecValue *runtime.RawExtension
	}{
		{
			name:              "with vSphere and a nil provider spec value",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name: "with vSphere and no fields set",
			providerSpecValue: &runtime.RawExtension{
				Object: &machinev1.VSphereMachineProviderSpec{},
			},
			expectedError: "[providerSpec.template: Required value: template must be provided, providerSpec.workspace: Required value: workspace must be provided, providerSpec.network.devices: Required value: at least 1 network device must be provided]",
		},
		{
			name: "with vSphere and the template, workspace and network devices set",
			providerSpecValue: &runtime.RawExtension{
				Object: &machinev1.VSphereMachineProviderSpec{
					Template: "template",
					Workspace: &machinev1.Workspace{
						Datacenter: "datacenter",
						Server:     "server",
					},
					Network: machinev1.NetworkSpec{
						Devices: []machinev1.NetworkDeviceSpec{
							{
								NetworkName: "networkName",
							},
						},
					},
				},
			},
			presetClusterID: true,
			expectedError:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			mgr, err := manager.New(cfg, manager.Options{
				MetricsBindAddress: "0",
				Port:               testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			gs.Expect(mapiwebhooks.RegisterWebhooks(&mapiwebhooks.MAPIWebhookConfig{
				Mgr:          mgr,
				Port:         testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:      testEnv.WebhookInstallOptions.LocalServingCertDir,
				ValidatingFn: ValidateVSphere,
				DefaultingFn: DefaultVSphere,
			})).To(Succeed())

			mgrCtx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(mgrCtx)).To(Succeed())
				close(stopped)
			}()
			defer func() {
				cancel()
				<-stopped
			}()

			gs.Eventually(func() (bool, error) {
				resp, err := insecureHTTPClient.Get(fmt.Sprintf("https://127.0.0.1:%d", testEnv.WebhookInstallOptions.LocalServingPort))
				if err != nil {
					return false, err
				}
				return resp.StatusCode == 404, nil
			}).Should(BeTrue())

			ms := &machinev1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-creation-",
					Namespace:    namespace.Name,
				},
				Spec: machinev1.MachineSetSpec{
					Template: machinev1.MachineTemplateSpec{
						Spec: machinev1.MachineSpec{
							ProviderSpec: machinev1.ProviderSpec{
								Value: tc.providerSpecValue,
							},
						},
					},
				},
			}

			m := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
				},
				Spec: machinev1.MachineSpec{
					ProviderSpec: machinev1.ProviderSpec{
						Value: tc.providerSpecValue,
					},
				},
			}

			presetClusterID := "anything"
			if tc.presetClusterID {
				m.Labels = make(map[string]string)
				m.Labels[machinev1.MachineClusterIDLabel] = presetClusterID
			}

			createAndCheck := func(obj client.Object) {
				err = k8sClient.Create(ctx, obj)
				if err == nil {
					defer func() {
						gs.Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
					}()
				}

				if tc.expectedError != "" {
					gs.Expect(err).ToNot(BeNil())
					gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
				} else {
					if tc.presetClusterID {
						gs.Expect(m.Labels[machinev1.MachineClusterIDLabel]).To(BeIdenticalTo(presetClusterID))
					} else {
						gs.Expect(m.Labels[machinev1.MachineClusterIDLabel]).To(BeIdenticalTo(clusterID))
					}
					gs.Expect(err).To(BeNil())
				}
			}

			createAndCheck(ms)
			createAndCheck(m)
		})
	}
}

func TestWebhooksOnUpdate(t *testing.T) {
	g := NewWithT(t)
	ctx := context.Background()

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1")},
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			MutatingWebhooks:   []*admissionregistrationv1.MutatingWebhookConfiguration{mapiwebhooks.NewMutatingWebhookConfiguration()},
			ValidatingWebhooks: []*admissionregistrationv1.ValidatingWebhookConfiguration{mapiwebhooks.NewValidatingWebhookConfiguration()},
		},
	}

	cfg, err := testEnv.Start()
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	k8sClient, err := client.New(cfg, client.Options{})
	g.Expect(err).ToNot(HaveOccurred())

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "creation-test",
		},
	}
	vSphereSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultVSphereCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
	}
	infraPatch := client.MergeFrom(infra.DeepCopy())

	g.Expect(k8sClient.Create(ctx, namespace)).To(Succeed())
	g.Expect(k8sClient.Create(ctx, vSphereSecret)).To(Succeed())
	g.Expect(k8sClient.Create(ctx, infra)).To(Succeed())
	infra.Status.InfrastructureName = clusterID
	g.Expect(k8sClient.Status().Patch(ctx, infra, infraPatch)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, vSphereSecret)).To(Succeed())
		g.Expect(k8sClient.Delete(ctx, infra)).To(Succeed())
	}()

	defaultVSphereProviderSpec := &machinev1.VSphereMachineProviderSpec{
		Template: "template",
		Workspace: &machinev1.Workspace{
			Datacenter: "datacenter",
			Server:     "server",
		},
		Network: machinev1.NetworkSpec{
			Devices: []machinev1.NetworkDeviceSpec{
				{
					NetworkName: "networkName",
				},
			},
		},
		UserDataSecret: &corev1.LocalObjectReference{
			Name: mapiwebhooks.DefaultUserDataSecret,
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: defaultVSphereCredentialsSecret,
		},
	}

	testCases := []struct {
		name                     string
		expectedError            string
		updatedProviderSpecValue func() *runtime.RawExtension
	}{
		{
			name: "with a valid VSphere ProviderSpec",
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultVSphereProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name: "with an VSphere ProviderSpec, removing the template",
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Template = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.template: Required value: template must be provided",
		},
		{
			name: "with an VSphere ProviderSpec, removing the workspace server",
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Workspace.Server = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.workspace.server: Required value: server must be provided",
		},
		{
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Network = machinev1.NetworkSpec{}
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			mgr, err := manager.New(cfg, manager.Options{
				MetricsBindAddress: "0",
				Port:               testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			gs.Expect(mapiwebhooks.RegisterWebhooks(&mapiwebhooks.MAPIWebhookConfig{
				Mgr:          mgr,
				Port:         testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:      testEnv.WebhookInstallOptions.LocalServingCertDir,
				ValidatingFn: ValidateVSphere,
				DefaultingFn: DefaultVSphere,
			})).To(Succeed())

			mgrCtx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(mgrCtx)).To(Succeed())
				close(stopped)
			}()
			defer func() {
				cancel()
				<-stopped
			}()

			gs.Eventually(func() (bool, error) {
				resp, err := insecureHTTPClient.Get(fmt.Sprintf("https://127.0.0.1:%d", testEnv.WebhookInstallOptions.LocalServingPort))
				if err != nil {
					return false, err
				}
				return resp.StatusCode == 404, nil
			}).Should(BeTrue())

			msLabel := "machineset-name"
			msLabelValue := "machineset-update-abcd"

			ms := &machinev1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-update-",
					Namespace:    namespace.Name,
				},
				Spec: machinev1.MachineSetSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							msLabel: msLabelValue,
						},
					},
					Template: machinev1.MachineTemplateSpec{
						ObjectMeta: machinev1.ObjectMeta{
							Labels: map[string]string{
								msLabel: msLabelValue,
							},
						},
						Spec: machinev1.MachineSpec{
							ProviderSpec: machinev1.ProviderSpec{
								Value: &runtime.RawExtension{
									Object: defaultVSphereProviderSpec.DeepCopy(),
								},
							},
						},
					},
				},
			}

			m := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
					Finalizers: []string{
						"machine-test",
					},
				},
				Spec: machinev1.MachineSpec{
					ProviderSpec: machinev1.ProviderSpec{
						Value: &runtime.RawExtension{
							Object: defaultVSphereProviderSpec.DeepCopy(),
						},
					},
				},
			}

			gs.Expect(k8sClient.Create(ctx, m)).To(Succeed())
			gs.Expect(k8sClient.Create(ctx, ms)).To(Succeed())
			defer func() {
				gs.Expect(k8sClient.Delete(ctx, m)).To(Succeed())
				gs.Expect(k8sClient.Delete(ctx, ms)).To(Succeed())
			}()

			mKey := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
			msKey := client.ObjectKey{Namespace: ms.Namespace, Name: ms.Name}
			gs.Expect(k8sClient.Get(ctx, mKey, m)).To(Succeed())
			gs.Expect(k8sClient.Get(ctx, msKey, ms)).To(Succeed())
			if tc.updatedProviderSpecValue != nil {
				m.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
				ms.Spec.Template.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			}

			updateAndCheck := func(obj client.Object) {
				err = k8sClient.Update(ctx, obj)
				if tc.expectedError != "" {
					gs.Expect(err).ToNot(BeNil())
					gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
				} else {
					gs.Expect(err).To(BeNil())
				}
			}

			updateAndCheck(m)
			updateAndCheck(ms)
		})
	}
}

func TestValidateVSphereProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vsphere-validation-test",
		},
	}

	testCases := []struct {
		testCase         string
		modifySpec       func(*machinev1.VSphereMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with no template provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Template = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.template: Required value: template must be provided",
		},
		{
			testCase: "with no workspace provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Workspace = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace: Required value: workspace must be provided",
		},
		{
			testCase: "with no workspace server provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Workspace = &machinev1.Workspace{
					Datacenter: "datacenter",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace.server: Required value: server must be provided",
		},
		{
			testCase: "with no workspace datacenter provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Workspace = &machinev1.Workspace{
					Server: "server",
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.workspace.datacenter: datacenter is unset: if more than one datacenter is present, VMs cannot be created"},
		},
		{
			testCase: "with a workspace folder outside of the current datacenter",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Workspace = &machinev1.Workspace{
					Server:     "server",
					Datacenter: "datacenter",
					Folder:     "/foo/vm/folder",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace.folder: Invalid value: \"/foo/vm/folder\": folder must be absolute path: expected prefix \"/datacenter/vm/\"",
		},
		{
			testCase: "with no network devices provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Network = machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
		},
		{
			testCase: "with no network device name provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.Network = machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
						{
							NetworkName: "networkName",
						},
						{},
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.network.devices[1].networkName: Required value: networkName must be provided",
		},
		{
			testCase: "with too few CPUs provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.NumCPUs = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.numCPUs: 1 is missing or less than the minimum value (2): nodes may not boot correctly"},
		},
		{
			testCase: "with too little memory provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.MemoryMiB = 1024
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.memoryMiB: 1024 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with too little disk size provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.DiskGiB = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.diskGiB: 1 is missing or less than the recommended minimum (120): nodes may fail to start if disk size is too low"},
		},
		{
			testCase: "with no user data secret provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.UserDataSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials secret provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no credentials secret name provided",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.CredentialsSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret.name: Required value: name must be provided",
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with numCPUs equal to 0",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.NumCPUs = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.numCPUs: 0 is missing or less than the minimum value (2): nodes may not boot correctly"},
		},
		{
			testCase: "with memoryMiB equal to 0",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.MemoryMiB = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.memoryMiB: 0 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with diskGiB equal to 0",
			modifySpec: func(p *machinev1.VSphereMachineProviderSpec) {
				p.DiskGiB = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.diskGiB: 0 is missing or less than the recommended minimum (120): nodes may fail to start if disk size is too low"},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewFakeClientWithScheme(scheme.Scheme, secret)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &machinev1.VSphereMachineProviderSpec{
				Template: "template",
				Workspace: &machinev1.Workspace{
					Datacenter: "datacenter",
					Server:     "server",
				},
				Network: machinev1.NetworkSpec{
					Devices: []machinev1.NetworkDeviceSpec{
						{
							NetworkName: "networkName",
						},
					},
				},
				UserDataSecret: &corev1.LocalObjectReference{
					Name: "name",
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "name",
				},
				NumCPUs:   minVSphereCPU,
				MemoryMiB: minVSphereMemoryMiB,
				DiskGiB:   minVSphereDiskGiB,
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawProviderSpec, err := RawExtensionFromProviderSpec(providerSpec)
			if err != nil {
				t.Errorf("RawExtensionFromProviderSpec() error = %v", err)
			}
			m.Spec.ProviderSpec.Value = rawProviderSpec

			ok, warnings, err := ValidateVSphere(m, c)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultVSphereProviderSpec(t *testing.T) {
	testCases := []struct {
		testCase         string
		providerSpec     *machinev1.VSphereMachineProviderSpec
		modifyDefault    func(*machinev1.VSphereMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &machinev1.VSphereMachineProviderSpec{},
			expectedOk:    true,
			expectedError: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: globalInfrastuctureName,
				},
				Status: configv1.InfrastructureStatus{
					InfrastructureName: clusterID,
				},
			}
			c := fake.NewFakeClientWithScheme(scheme.Scheme, infra)

			defaultProviderSpec := &machinev1.VSphereMachineProviderSpec{
				UserDataSecret: &corev1.LocalObjectReference{
					Name: mapiwebhooks.DefaultUserDataSecret,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: defaultVSphereCredentialsSecret,
				},
			}
			if tc.modifyDefault != nil {
				tc.modifyDefault(defaultProviderSpec)
			}

			m := &machinev1.Machine{}
			rawProviderSpec, err := RawExtensionFromProviderSpec(tc.providerSpec)
			if err != nil {
				t.Errorf("RawExtensionFromProviderSpec() error = %v", err)
			}
			m.Spec.ProviderSpec.Value = rawProviderSpec

			ok, warnings, err := DefaultVSphere(m, c)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec, err := ProviderSpecFromRawExtension(m.Spec.ProviderSpec.Value)
			if err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}
