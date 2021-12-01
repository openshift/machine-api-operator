package webhooks

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestMachineCreation(t *testing.T) {
	g := NewWithT(t)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machine-creation-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	testCases := []struct {
		name          string
		expectedError string
		machine       *machinev1.Machine
		expectedLabel string
	}{
		{
			name: "test defaulting webhook functionality",
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
				},
			},
			expectedLabel: "machine-creation-test-label",
		},
		{
			name: "test validating webhook functionality",
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-wrong-name",
					Namespace: namespace.Name,
				},
			},
			expectedError: "name: Required value: a value must be provided",
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

			gs.Expect(RegisterWebhooks(&MAPIWebhookConfig{
				Mgr:     mgr,
				Port:    testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
				ValidatingFn: func(m *machinev1.Machine, client client.Client) (bool, []string, utilerrors.Aggregate) {
					if m.Name == "some-wrong-name" {
						errs := []error{field.Required(field.NewPath("name"), "a value must be provided")}
						return false, []string{}, utilerrors.NewAggregate(errs)
					}
					return true, []string{}, nil
				},
				DefaultingFn: func(m *machinev1.Machine, client client.Client) (bool, []string, utilerrors.Aggregate) {
					m.Labels = map[string]string{"machine-creation-test-label": "true"}
					return true, []string{}, nil
				},
			}))

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

			err = c.Create(ctx, tc.machine)
			if err == nil {
				defer func() {
					gs.Expect(c.Delete(ctx, tc.machine)).To(Succeed())
				}()
			}

			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			}

			if tc.expectedLabel != "" {
				gs.Expect(tc.machine.Labels).To(HaveKey(tc.expectedLabel))
			}
		})
	}
}

func TestMachineUpdate(t *testing.T) {
	g := NewWithT(t)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machine-update-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	preDrainHook := machinev1.LifecycleHook{
		Name:  "pre-drain",
		Owner: "pre-drain-owner",
	}

	testCases := []struct {
		name                      string
		expectedError             string
		baseMachineLifecycleHooks machinev1.LifecycleHooks
		updateAfterDelete         bool
		updateMachine             func(m *machinev1.Machine)
	}{
		{
			name: "when adding a lifecycle hook",
			updateMachine: func(m *machinev1.Machine) {
				m.Spec.LifecycleHooks.PreDrain = []machinev1.LifecycleHook{preDrainHook}
			},
		},
		{
			name:              "when adding a lifecycle hook after the machine has been deleted",
			updateAfterDelete: true,
			updateMachine: func(m *machinev1.Machine) {
				m.Spec.LifecycleHooks.PreDrain = []machinev1.LifecycleHook{preDrainHook}
			},
			expectedError: "spec.lifecycleHooks.preDrain: Forbidden: pre-drain hooks are immutable when machine is marked for deletion: the following hooks are new or changed: [{Name:pre-drain Owner:pre-drain-owner}]",
		},
		{
			name: "when removing a lifecycle hook after the machine has been deleted",
			baseMachineLifecycleHooks: machinev1.LifecycleHooks{
				PreDrain: []machinev1.LifecycleHook{preDrainHook},
			},
			updateAfterDelete: true,
			updateMachine: func(m *machinev1.Machine) {
				m.Spec.LifecycleHooks = machinev1.LifecycleHooks{}
			},
		},
		{
			name: "when duplicating a lifecycle hook",
			updateMachine: func(m *machinev1.Machine) {
				m.Spec.LifecycleHooks.PreDrain = []machinev1.LifecycleHook{preDrainHook, preDrainHook}
			},
			expectedError: "spec.lifecycleHooks.preDrain[1].name: Forbidden: hook names must be unique within a lifecycle stage, the following hook name is already set: pre-drain",
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

			gs.Expect(RegisterWebhooks(&MAPIWebhookConfig{
				Mgr:     mgr,
				Port:    testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
				ValidatingFn: func(m *machinev1.Machine, client client.Client) (bool, []string, utilerrors.Aggregate) {
					return true, []string{}, nil
				},
				DefaultingFn: func(m *machinev1.Machine, client client.Client) (bool, []string, utilerrors.Aggregate) {
					return true, []string{}, nil
				},
			}))

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

			m := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
					Finalizers: []string{
						"machine-test",
					},
				},
				Spec: machinev1.MachineSpec{
					LifecycleHooks: tc.baseMachineLifecycleHooks,
				},
			}
			err = c.Create(ctx, m)
			gs.Expect(err).ToNot(HaveOccurred())
			if tc.updateAfterDelete {
				gs.Expect(c.Delete(ctx, m)).To(Succeed())
			} else {
				defer func() {
					gs.Expect(c.Delete(ctx, m)).To(Succeed())
				}()
			}

			key := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
			defer func() {
				mc := &machinev1.Machine{}
				gs.Expect(c.Get(ctx, key, mc)).To(Succeed())
				mc.Finalizers = []string{}
				gs.Expect(c.Update(ctx, mc)).To(Succeed())
			}()

			gs.Expect(c.Get(ctx, key, m)).To(Succeed())
			if tc.updateMachine != nil {
				tc.updateMachine(m)
			}
			err = c.Update(ctx, m)
			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}
