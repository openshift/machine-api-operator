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
		oV       []osconfigv1.OperandVersion
		expected osconfigv1.ConditionStatus
	}
	tCases := []tCase{
		{
			oV: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "1.0",
				},
			},
			expected: osconfigv1.ConditionFalse,
		},
		{
			oV: []osconfigv1.OperandVersion{
				{
					Name:    "operator",
					Version: "2.0",
				},
			},
			expected: osconfigv1.ConditionTrue,
		},
	}

	optr := Operator{
		operandVersions: []osconfigv1.OperandVersion{
			{
				Name:    "operator",
				Version: "1.0",
			},
		},
		eventRecorder: record.NewFakeRecorder(5),
	}
	for i, tc := range tCases {
		co := &osconfigv1.ClusterOperator{ObjectMeta: metav1.ObjectMeta{Name: clusterOperatorName}}
		co.Status.Versions = tc.oV

		optr.osClient = fakeconfigclientset.NewSimpleClientset(co)
		optr.statusProgressing()
		o, _ := optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
		var condition osconfigv1.ClusterOperatorStatusCondition
		for _, coCondition := range o.Status.Conditions {
			if coCondition.Type == osconfigv1.OperatorProgressing {
				condition = coCondition
				break
			}
		}
		assert.Equal(t, tc.expected, condition.Status, "test-case %v expected condition %v to be %v, but got %v", i, condition.Type, tc.expected, condition.Status)
	}
}
