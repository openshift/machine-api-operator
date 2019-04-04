package operator

import (
	"testing"

	osconfigv1 "github.com/openshift/api/config/v1"
	fakeconfigclientset "github.com/openshift/client-go/config/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
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
		currentVersion        []osconfigv1.OperandVersion
		desiredVersion        []osconfigv1.OperandVersion
		expectedStatus        osconfigv1.ConditionStatus
		transitionTimeUpdated bool
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
			expectedStatus: osconfigv1.ConditionFalse,
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
			expectedStatus: osconfigv1.ConditionTrue,
		},
	}

	optr := Operator{eventRecorder: record.NewFakeRecorder(5)}
	for i, tc := range tCases {
		optr.operandVersions = tc.currentVersion
		co := &osconfigv1.ClusterOperator{ObjectMeta: metav1.ObjectMeta{Name: clusterOperatorName}}
		co.Status.Versions = tc.desiredVersion

		optr.osClient = fakeconfigclientset.NewSimpleClientset(co)
		startTime := metav1.Now()
		optr.statusProgressing()
		o, _ := optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
		var condition osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range o.Status.Conditions {
			assert.True(t, startTime.Before(&coCondition.LastTransitionTime), "test-case %v expected LastTransitionTime for the status condition to be updated", i)
			if coCondition.Type == osconfigv1.OperatorProgressing {
				condition = coCondition
				break
			}
		}
		assert.Equal(t, tc.expectedStatus, condition.Status, "test-case %v expected condition %v to be %v, but got %v", i, condition.Type, tc.expectedStatus, condition.Status)
		optr.statusProgressing()
		o, _ = optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
		var conditionAfterAnotherSync osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range o.Status.Conditions {
			if coCondition.Type == osconfigv1.OperatorProgressing {
				conditionAfterAnotherSync = coCondition
				break
			}
		}
		assert.Equal(t, condition.Status, conditionAfterAnotherSync.Status, "condition state is expected to be same")
		assert.True(t, condition.LastTransitionTime.Equal(&conditionAfterAnotherSync.LastTransitionTime), "test-case %v expected LastTransitionTime not to be updated if condition state is same", i)
	}
}
