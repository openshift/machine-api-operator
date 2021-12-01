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
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestMachineSetUpdate(t *testing.T) {
	g := NewWithT(t)

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machineset-update-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	testCases := []struct {
		name                     string
		expectedError            string
		updatedProviderSpecValue func() *runtime.RawExtension
		updateMachineSet         func(ms *machinev1.MachineSet)
	}{
		{
			name: "with a modification to the selector",
			updateMachineSet: func(ms *machinev1.MachineSet) {
				ms.Spec.Selector.MatchLabels["foo"] = "bar"
			},
			expectedError: "[spec.selector: Forbidden: selector is immutable, spec.template.metadata.labels: Invalid value: map[string]string{\"machineset-name\":\"machineset-update-abcd\"}: `selector` does not match template `labels`]",
		},
		{
			name: "with an incompatible template labels",
			updateMachineSet: func(ms *machinev1.MachineSet) {
				ms.Spec.Template.ObjectMeta.Labels = map[string]string{
					"foo": "bar",
				}
			},
			expectedError: "spec.template.metadata.labels: Invalid value: map[string]string{\"foo\":\"bar\"}: `selector` does not match template `labels`",
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
					},
				},
			}
			err = c.Create(ctx, ms)
			gs.Expect(err).ToNot(HaveOccurred())
			defer func() {
				gs.Expect(c.Delete(ctx, ms)).To(Succeed())
			}()

			if tc.updatedProviderSpecValue != nil {
				ms.Spec.Template.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			}
			if tc.updateMachineSet != nil {
				tc.updateMachineSet(ms)
			}
			err = c.Update(ctx, ms)
			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}
