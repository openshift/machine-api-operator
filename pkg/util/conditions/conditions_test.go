package conditions

import (
	"reflect"
	"testing"

	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	namespace   = "openshift-machine-api"
	correctData = `items:
- name: Ready 
  timeout: 60s
  status: Unknown`
	unrelatedConditionData = `items:
- name: Unrelated 
  timeout: 60s
  status: Unknown`
	unrelatedStatusData = `items:
- name: Ready 
  timeout: 60s
  status: Unrelated`
)

type expectedConditions struct {
	conditions []UnhealthyCondition
	error      bool
}

func node(name string, ready bool) *v1.Node {
	nodeReadyStatus := corev1.ConditionTrue
	if !ready {
		nodeReadyStatus = corev1.ConditionUnknown
	}

	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceNone,
			Labels:    map[string]string{},
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: nodeReadyStatus,
				},
			},
		},
	}
}

func configMap(name string, data map[string]string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		Data: data,
	}
}
func TestGetNodeUnhealthyConditions(t *testing.T) {
	nodeHealthy := node("nodeHealthy", true)
	nodeRecentlyUnhealthy := node("nodeRecentlyUnhealthy", false)

	cmWithoutConditions := configMap(
		healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		map[string]string{
			"noconditions": "",
		},
	)
	cmWithBadData := configMap(
		healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		map[string]string{
			"conditions": "unparsed-data",
		},
	)
	cmWithUnrelatedCondition := configMap(
		healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		map[string]string{
			"conditions": unrelatedConditionData,
		},
	)
	cmWithUnrelatedConditionStatus := configMap(
		healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		map[string]string{
			"conditions": unrelatedStatusData,
		},
	)
	cmWithCorrectData := configMap(
		healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		map[string]string{
			"conditions": correctData,
		},
	)

	testsCases := []struct {
		name     string
		node     *corev1.Node
		cm       *corev1.ConfigMap
		expected expectedConditions
	}{
		{
			name: "healthy node",
			node: nodeHealthy,
			cm:   cmWithCorrectData,
			expected: expectedConditions{
				conditions: []UnhealthyCondition{},
				error:      false,
			},
		},
		{
			name: "unhealthy node",
			node: nodeRecentlyUnhealthy,
			cm:   cmWithCorrectData,
			expected: expectedConditions{
				conditions: []UnhealthyCondition{
					{
						Name:    "Ready",
						Timeout: "60s",
						Status:  "Unknown",
					},
				},
				error: false,
			},
		},
		{
			name: "configmap without conditions",
			node: nodeRecentlyUnhealthy,
			cm:   cmWithoutConditions,
			expected: expectedConditions{
				conditions: nil,
				error:      true,
			},
		},
		{
			name: "configmap with bad data",
			node: nodeRecentlyUnhealthy,
			cm:   cmWithBadData,
			expected: expectedConditions{
				conditions: nil,
				error:      true,
			},
		},
		{
			name: "configmap with unrelated condition",
			node: nodeRecentlyUnhealthy,
			cm:   cmWithUnrelatedCondition,
			expected: expectedConditions{
				conditions: []UnhealthyCondition{},
				error:      false,
			},
		},
		{
			name: "configmap with unrelated condition status",
			node: nodeRecentlyUnhealthy,
			cm:   cmWithUnrelatedConditionStatus,
			expected: expectedConditions{
				conditions: []UnhealthyCondition{},
				error:      false,
			},
		},
	}

	for _, tc := range testsCases {
		conditions, err := GetNodeUnhealthyConditions(tc.node, tc.cm)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected %s error, got: %v", tc.name, errorExpectation, err)
		}

		numOfConditions := len(conditions)
		expectedNumOfConditions := len(tc.expected.conditions)
		if numOfConditions != expectedNumOfConditions {
			t.Errorf("Test case: %s. Expected number of conditions %d ,got: %d", tc.name, expectedNumOfConditions, numOfConditions)
		}

		if !reflect.DeepEqual(tc.expected.conditions, conditions) {
			t.Errorf("Test case: %s. Expected %s conditions, got: %s", tc.name, tc.expected.conditions, conditions)
		}
	}
}
