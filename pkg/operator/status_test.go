package operator

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	osconfigv1 "github.com/openshift/api/config/v1"
	fakeconfigclientset "github.com/openshift/client-go/config/clientset/versioned/fake"
	"github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
)

func TestPrintOperandVersions(t *testing.T) {
	optr := Operator{
		operandVersions: []osconfigv1.OperandVersion{
			{
				Name:    "operator",
				Version: "1.0",
			},
			{
				Name:    "controller-manager",
				Version: "2.0",
			},
		},
	}
	expectedOutput := "operator: 1.0, controller-manager: 2.0"
	got := optr.printOperandVersions()
	if got != expectedOutput {
		t.Errorf("Expected: %s, got: %s", expectedOutput, got)
	}
}

func TestOperatorStatusProgressing(t *testing.T) {
	type tCase struct {
		currentVersion     []osconfigv1.OperandVersion
		desiredVersion     []osconfigv1.OperandVersion
		expectedConditions []osconfigv1.ClusterOperatorStatusCondition
	}
	tCases := []tCase{
		{
			currentVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			desiredVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			expectedConditions: []osconfigv1.ClusterOperatorStatusCondition{
				newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, osconfigv1.ConditionFalse, string(ReasonSyncing), ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionFalse, "", ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionFalse, "", ""),
				operatorUpgradeable,
			},
		},
		{
			currentVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			desiredVersion: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "2.0",
				},
			},
			expectedConditions: []osconfigv1.ClusterOperatorStatusCondition{
				newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, osconfigv1.ConditionTrue, string(ReasonSyncing), ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionFalse, "", ""),
				newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionFalse, "", ""),
				operatorUpgradeable,
			},
		},
	}

	optr := Operator{eventRecorder: record.NewFakeRecorder(5)}
	for i, tc := range tCases {
		startTime := metav1.Now()

		optr.operandVersions = tc.currentVersion
		co := optr.defaultClusterOperator()
		co.Status.Versions = tc.desiredVersion
		optr.osClient = fakeconfigclientset.NewSimpleClientset(co)

		err := optr.statusProgressing()
		assert.NoError(t, err)

		gotCO, err := optr.getClusterOperator()
		if err != nil {
			t.Fatalf("Failed to fetch ClusterOperator: %v", err)
		}

		var condition osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range gotCO.Status.Conditions {
			assert.True(t, startTime.Before(&coCondition.LastTransitionTime), "test-case %v expected LastTransitionTime for the status condition to be updated", i)
			if coCondition.Type == osconfigv1.OperatorProgressing {
				condition = coCondition
				break
			}
		}

		for _, expectedCondition := range tc.expectedConditions {
			ok := v1helpers.IsStatusConditionPresentAndEqual(
				gotCO.Status.Conditions, expectedCondition.Type, expectedCondition.Status,
			)
			if !ok {
				t.Errorf("wrong status for condition. Expected: %v, got: %v",
					expectedCondition,
					v1helpers.FindStatusCondition(gotCO.Status.Conditions, expectedCondition.Type))
			}
		}

		err = optr.statusProgressing()
		assert.NoError(t, err)

		gotCO, err = optr.osClient.ConfigV1().ClusterOperators().Get(context.Background(), clusterOperatorName, metav1.GetOptions{})
		assert.NoError(t, err)

		var conditionAfterAnotherSync osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range gotCO.Status.Conditions {
			if coCondition.Type == osconfigv1.OperatorProgressing {
				conditionAfterAnotherSync = coCondition
				break
			}
		}
		assert.True(t, condition.LastTransitionTime.Equal(&conditionAfterAnotherSync.LastTransitionTime), "test-case %v expected LastTransitionTime not to be updated if condition state is same", i)

		for _, expectedCondition := range tc.expectedConditions {
			ok := v1helpers.IsStatusConditionPresentAndEqual(
				gotCO.Status.Conditions, expectedCondition.Type, expectedCondition.Status,
			)
			if !ok {
				t.Errorf("wrong status for condition. Expected: %v, got: %v",
					expectedCondition,
					v1helpers.FindStatusCondition(gotCO.Status.Conditions, expectedCondition.Type))
			}
		}
	}
}

func TestGetOrCreateClusterOperator(t *testing.T) {
	var namespace = "some-namespace"

	var defaultConditions = []osconfigv1.ClusterOperatorStatusCondition{
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorProgressing,
			osconfigv1.ConditionTrue,
			string(ReasonInitializing),
			"Operator is initializing",
		),
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorDegraded,
			osconfigv1.ConditionFalse,
			string(ReasonAsExpected), "",
		),
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorAvailable,
			osconfigv1.ConditionFalse,
			string(ReasonInitializing),
			"Operator is initializing",
		),
	}

	var conditions = []osconfigv1.ClusterOperatorStatusCondition{
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorProgressing,
			osconfigv1.ConditionFalse,
			"", "",
		),
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorDegraded,
			osconfigv1.ConditionFalse,
			"", "",
		),
		{
			Type:               "Available",
			Status:             "true",
			Reason:             "",
			Message:            "",
			LastTransitionTime: metav1.NewTime(time.Now()),
		},
	}

	testCases := []struct {
		existingCO *osconfigv1.ClusterOperator
		expectedCO *osconfigv1.ClusterOperator
	}{
		{
			existingCO: nil,
			expectedCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
					Annotations: map[string]string{
						"openshift.io/required-scc": "restricted-v2",
					},
				},
				Status: osconfigv1.ClusterOperatorStatus{
					Conditions: defaultConditions,
					RelatedObjects: []osconfigv1.ObjectReference{
						{
							Group:    "",
							Resource: "namespaces",
							Name:     namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machines",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machinesets",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machinehealthchecks",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "rbac.authorization.k8s.io",
							Resource:  "roles",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:    "rbac.authorization.k8s.io",
							Resource: "clusterroles",
							Name:     "machine-api-operator",
						},
						{
							Group:    "rbac.authorization.k8s.io",
							Resource: "clusterroles",
							Name:     "machine-api-controllers",
						},
						{
							Group:     "metal3.io",
							Resource:  "baremetalhosts",
							Name:      "",
							Namespace: namespace,
						},
					},
				},
			},
		},
		{
			existingCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					Conditions: conditions,
				},
			},
			expectedCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					RelatedObjects: []osconfigv1.ObjectReference{
						{
							Group:    "",
							Resource: "namespaces",
							Name:     namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machines",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machinesets",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "machine.openshift.io",
							Resource:  "machinehealthchecks",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:     "rbac.authorization.k8s.io",
							Resource:  "roles",
							Name:      "",
							Namespace: namespace,
						},
						{
							Group:    "rbac.authorization.k8s.io",
							Resource: "clusterroles",
							Name:     "machine-api-operator",
						},
						{
							Group:    "rbac.authorization.k8s.io",
							Resource: "clusterroles",
							Name:     "machine-api-controllers",
						},
						{
							Group:     "metal3.io",
							Resource:  "baremetalhosts",
							Name:      "",
							Namespace: namespace,
						},
					},
					Conditions: conditions,
				},
			},
		},
	}

	for _, tc := range testCases {
		var osClient *fakeconfigclientset.Clientset
		if tc.existingCO != nil {
			osClient = fakeconfigclientset.NewSimpleClientset(tc.existingCO)
		} else {
			osClient = fakeconfigclientset.NewSimpleClientset()
		}
		optr := Operator{
			osClient:  osClient,
			namespace: namespace,
		}

		co, err := optr.getOrCreateClusterOperator()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		normalizeTransitionTimes(co.Status, tc.expectedCO.Status)

		if !equality.Semantic.DeepEqual(co, tc.expectedCO) {
			t.Errorf("got: %v, expected: %v", co, tc.expectedCO)
		}
	}
}

func normalizeTransitionTimes(got, expected osconfigv1.ClusterOperatorStatus) {
	now := metav1.NewTime(time.Now())

	for i := range got.Conditions {
		got.Conditions[i].LastTransitionTime = now
	}

	for i := range expected.Conditions {
		expected.Conditions[i].LastTransitionTime = now
	}
}

func TestIsInitializing(t *testing.T) {
	testCases := []struct {
		name                 string
		existingCO           *osconfigv1.ClusterOperator
		expectedError        error
		expectedInitializing bool
	}{
		{
			name:                 "with no existing cluster operator",
			existingCO:           nil,
			expectedError:        apierrors.NewNotFound(osconfigv1.GroupVersion.WithResource("clusteroperators").GroupResource(), clusterOperatorName),
			expectedInitializing: false,
		},
		{
			name: "with an initialized Available condition",
			existingCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					Conditions: []osconfigv1.ClusterOperatorStatusCondition{
						newClusterOperatorStatusCondition(
							osconfigv1.OperatorAvailable,
							osconfigv1.ConditionFalse,
							string(ReasonAsExpected),
							"Operator is initialized",
						),
					},
				},
			},
			expectedError:        nil,
			expectedInitializing: false,
		},
		{
			name: "with an initializing Available condition",
			existingCO: &osconfigv1.ClusterOperator{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterOperatorName,
				},
				Status: osconfigv1.ClusterOperatorStatus{
					Conditions: []osconfigv1.ClusterOperatorStatusCondition{
						newClusterOperatorStatusCondition(
							osconfigv1.OperatorAvailable,
							osconfigv1.ConditionTrue,
							string(ReasonInitializing),
							"Operator is initializing",
						),
					},
				},
			},
			expectedError:        nil,
			expectedInitializing: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			var osClient *fakeconfigclientset.Clientset
			if tc.existingCO != nil {
				osClient = fakeconfigclientset.NewSimpleClientset(tc.existingCO)
			} else {
				osClient = fakeconfigclientset.NewSimpleClientset()
			}
			optr := Operator{
				osClient: osClient,
			}

			initializing, err := optr.isInitializing()
			if tc.expectedError != nil {
				g.Expect(err).To(MatchError(err))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}

			g.Expect(initializing).To(Equal(tc.expectedInitializing))
		})
	}
}
