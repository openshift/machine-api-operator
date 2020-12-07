package operator

import (
	"fmt"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
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
