package operator

import (
	"context"
	"fmt"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
	"k8s.io/utils/pointer"
)

func TestWaitForDeploymentRollout(t *testing.T) {
	testCases := []struct {
		name       string
		deployment *appsv1.Deployment
		expected   error
	}{
		{
			name: "Deployment is available for more than deploymentMinimumAvailabilityTime min",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-deploymentMinimumAvailabilityTime - 1*time.Second)},
						},
					},
				},
			},
			expected: nil,
		},
		{
			name: "Deployment is available for less than deploymentMinimumAvailabilityTime min",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Second)},
						},
					},
				},
			},
			expected: fmt.Errorf("deployment test has been available for less than 3 min"),
		},
		{
			name: "Deployment has unavailable replicas",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 1,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Second)},
						},
					},
				},
			},
			expected: fmt.Errorf("deployment test is not ready. status: (replicas: 1, updated: 1, ready: 1, unavailable: 1)"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			optr := newFakeOperator([]runtime.Object{tc.deployment}, nil, make(<-chan struct{}))

			got := optr.waitForDeploymentRollout(tc.deployment, 1*time.Second, 3*time.Second)
			if tc.expected != nil {
				if tc.expected.Error() != got.Error() {
					t.Errorf("Got: %v, expected: %v", got, tc.expected)
				}
			} else if tc.expected != got {
				t.Errorf("Got: %v, expected: %v", got, tc.expected)
			}
		})
	}
}

func TestSyncValidatingWebhooks(t *testing.T) {
	defaultConfiguration := mapiv1.NewValidatingWebhookConfiguration()

	withCABundle := defaultConfiguration.DeepCopy()
	for i, webhook := range withCABundle.Webhooks {
		webhook.ClientConfig.CABundle = []byte("test")
		webhook.TimeoutSeconds = pointer.Int32Ptr(10)
		withCABundle.Webhooks[i] = webhook
	}

	fail := admissionregistrationv1.Fail
	withExtraWebhook := withCABundle.DeepCopy()
	withExtraWebhook.Webhooks = append(withExtraWebhook.Webhooks, admissionregistrationv1.ValidatingWebhook{
		Name:          "extra.webhook",
		FailurePolicy: &fail,
	})

	withChangedFields := withCABundle.DeepCopy()
	for i, webhook := range withChangedFields.Webhooks {
		fail := admissionregistrationv1.Fail
		webhook.FailurePolicy = &fail

		webhook.ClientConfig.Service.Name = "wrong.service.name"
		webhook.Rules = append(webhook.Rules, webhook.Rules...)
		withChangedFields.Webhooks[i] = webhook
	}

	cases := []struct {
		name            string
		existingWebhook *admissionregistrationv1.ValidatingWebhookConfiguration
		expectedWebhook *admissionregistrationv1.ValidatingWebhookConfiguration
	}{
		{
			name:            "It should create the configuration if it does not exist",
			expectedWebhook: defaultConfiguration.DeepCopy(),
		},
		{
			name:            "It should not overwrite the cabundle or defaulted fields once populated",
			expectedWebhook: withCABundle.DeepCopy(),
			existingWebhook: withCABundle.DeepCopy(),
		},
		{
			name:            "It should drop any extra webhooks present",
			expectedWebhook: withCABundle.DeepCopy(),
			existingWebhook: withExtraWebhook.DeepCopy(),
		},
		{
			name:            "It should overwrite any fields that have been changed",
			expectedWebhook: withCABundle.DeepCopy(),
			existingWebhook: withChangedFields.DeepCopy(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			stop := make(chan struct{})
			defer close(stop)

			kubeObjs := []runtime.Object{}
			if tc.existingWebhook != nil {
				kubeObjs = append(kubeObjs, tc.existingWebhook)
			}

			optr := newFakeOperator(kubeObjs, nil, stop)

			err := optr.syncValidatingWebhook()
			g.Expect(err).ToNot(HaveOccurred())

			if tc.expectedWebhook == nil {
				// Nothing to check
				return
			}

			client := optr.kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations()
			gotWebhook, err := client.Get(context.Background(), tc.expectedWebhook.Name, metav1.GetOptions{})
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(gotWebhook).To(Equal(tc.expectedWebhook))
		})
	}
}

func TestSyncMutatingWebhooks(t *testing.T) {
	defaultConfiguration := mapiv1.NewMutatingWebhookConfiguration()

	withCABundle := defaultConfiguration.DeepCopy()
	for i, webhook := range withCABundle.Webhooks {
		webhook.ClientConfig.CABundle = []byte("test")
		webhook.TimeoutSeconds = pointer.Int32Ptr(10)
		never := admissionregistrationv1.NeverReinvocationPolicy
		webhook.ReinvocationPolicy = &never
		withCABundle.Webhooks[i] = webhook
	}

	fail := admissionregistrationv1.Fail
	withExtraWebhook := withCABundle.DeepCopy()
	withExtraWebhook.Webhooks = append(withExtraWebhook.Webhooks, admissionregistrationv1.MutatingWebhook{
		Name:          "extra.webhook",
		FailurePolicy: &fail,
	})

	withChangedFields := withCABundle.DeepCopy()
	for i, webhook := range withChangedFields.Webhooks {
		fail := admissionregistrationv1.Fail
		webhook.FailurePolicy = &fail

		webhook.ClientConfig.Service.Name = "wrong.service.name"
		webhook.Rules = append(webhook.Rules, webhook.Rules...)
		withChangedFields.Webhooks[i] = webhook
	}

	cases := []struct {
		name            string
		existingWebhook *admissionregistrationv1.MutatingWebhookConfiguration
		expectedWebhook *admissionregistrationv1.MutatingWebhookConfiguration
	}{
		{
			name:            "It should create the configuration if it does not exist",
			expectedWebhook: defaultConfiguration.DeepCopy(),
		},
		{
			name:            "It should not overwrite the cabundle or defaulted fields once populated",
			expectedWebhook: withCABundle.DeepCopy(),
			existingWebhook: withCABundle.DeepCopy(),
		},
		{
			name:            "It should drop any extra webhooks present",
			expectedWebhook: withCABundle.DeepCopy(),
			existingWebhook: withExtraWebhook.DeepCopy(),
		},
		{
			name:            "It should overwrite any fields that have been changed",
			expectedWebhook: withCABundle.DeepCopy(),
			existingWebhook: withChangedFields.DeepCopy(),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			stop := make(chan struct{})
			defer close(stop)

			kubeObjs := []runtime.Object{}
			if tc.existingWebhook != nil {
				kubeObjs = append(kubeObjs, tc.existingWebhook)
			}

			optr := newFakeOperator(kubeObjs, nil, stop)

			err := optr.syncMutatingWebhook()
			g.Expect(err).ToNot(HaveOccurred())

			if tc.expectedWebhook == nil {
				// Nothing to check
				return
			}

			client := optr.kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations()
			gotWebhook, err := client.Get(context.Background(), tc.expectedWebhook.Name, metav1.GetOptions{})
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(gotWebhook).To(Equal(tc.expectedWebhook))
		})
	}
}

func Test_ensureDependecyAnnotations(t *testing.T) {
	cases := []struct {
		name string

		input       *appsv1.Deployment
		inputHashes map[string]string

		expected *appsv1.Deployment
	}{{
		name:        "no previous hash tag",
		input:       &appsv1.Deployment{},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-1"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
	}, {
		name: "changed in on of the dependencies",
		input: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-2"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-2",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-2",
						},
					},
				},
			},
		},
	}, {
		name: "no change in dependencies",
		input: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-1"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
	}}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := test.input.DeepCopy()
			ensureDependecyAnnotations(test.inputHashes, input)
			if !equality.Semantic.DeepEqual(test.expected, input) {
				t.Fatalf("unexpected: %s", diff.ObjectDiff(test.expected, input))
			}
		})
	}
}
