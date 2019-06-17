package conditions

import (
	"reflect"
	"testing"

	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	namespace   = "openshift-machine-api"
	correctData = `items:
- name: Ready 
  timeout: 60s
  status: Unknown`
)

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

	testsCases := []struct {
		name     string
		node     *corev1.Node
		conds    []UnhealthyCondition
		expected []UnhealthyCondition
	}{
		{
			name: "healthy node",
			node: nodeHealthy,
			conds: []UnhealthyCondition{
				{
					Name:    "Ready",
					Status:  "Unknown",
					Timeout: "60s",
				},
			},
			expected: []UnhealthyCondition{},
		},
		{
			name: "unhealthy node",
			node: nodeRecentlyUnhealthy,
			conds: []UnhealthyCondition{
				{
					Name:    "Ready",
					Status:  "Unknown",
					Timeout: "60s",
				},
			},
			expected: []UnhealthyCondition{
				{
					Name:    "Ready",
					Timeout: "60s",
					Status:  "Unknown",
				},
			},
		},
		{
			name:     "no unhealthy conditions",
			node:     nodeRecentlyUnhealthy,
			conds:    []UnhealthyCondition{},
			expected: []UnhealthyCondition{},
		},
		{
			name: "unrelated unhealthy condition type",
			node: nodeRecentlyUnhealthy,
			conds: []UnhealthyCondition{
				{
					Name:    "Unrelated",
					Status:  "Unknown",
					Timeout: "60s",
				},
			},
			expected: []UnhealthyCondition{},
		},
		{
			name: "unrelated unhealthy condition status",
			node: nodeRecentlyUnhealthy,
			conds: []UnhealthyCondition{
				{
					Name:    "Ready",
					Status:  "Unrelated",
					Timeout: "60s",
				},
			},
			expected: []UnhealthyCondition{},
		},
	}

	for _, tc := range testsCases {
		conditions := GetNodeUnhealthyConditions(tc.node, tc.conds)
		numOfConditions := len(conditions)
		expectedNumOfConditions := len(tc.expected)
		if numOfConditions != expectedNumOfConditions {
			t.Errorf("Test case: %s. Expected number of conditions %d ,got: %d", tc.name, expectedNumOfConditions, numOfConditions)
		}

		if !reflect.DeepEqual(tc.expected, conditions) {
			t.Errorf("Test case: %s. Expected %s conditions, got: %s", tc.name, tc.expected, conditions)
		}
	}
}

type Expected struct {
	unhealthyConditions []UnhealthyCondition
	error               bool
}

func TestGetConditionsFromConfigMap(t *testing.T) {
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
	cmWithCorrectData := configMap(
		healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		map[string]string{
			"conditions": correctData,
		},
	)

	testsCases := []struct {
		name     string
		cm       *corev1.ConfigMap
		expected Expected
	}{
		{
			name: "without condition key under the config map",
			cm:   cmWithoutConditions,
			expected: Expected{
				unhealthyConditions: nil,
				error:               true,
			},
		},
		{
			name: "with a bad data under the config map",
			cm:   cmWithBadData,
			expected: Expected{
				unhealthyConditions: nil,
				error:               true,
			},
		},
		{
			name: "with correct data",
			cm:   cmWithCorrectData,
			expected: Expected{
				unhealthyConditions: []UnhealthyCondition{
					{
						Name:    "Ready",
						Status:  "Unknown",
						Timeout: "60s",
					},
				},
				error: false,
			},
		},
		{
			name: "without unhealthy condition config map",
			cm:   nil,
			expected: Expected{
				unhealthyConditions: []UnhealthyCondition{
					{
						Name:    "Ready",
						Status:  "Unknown",
						Timeout: "300s",
					},
					{
						Name:    "Ready",
						Status:  "False",
						Timeout: "300s",
					},
				},
				error: false,
			},
		},
	}

	for _, tc := range testsCases {
		objects := []runtime.Object{}
		if tc.cm != nil {
			objects = append(objects, tc.cm)
		}
		fakeClient := fake.NewFakeClient(objects...)

		unhealthyConditions, err := GetConditionsFromConfigMap(fakeClient, namespace)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error == true {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.name, errorExpectation, err)
		}

		if !reflect.DeepEqual(tc.expected.unhealthyConditions, unhealthyConditions) {
			t.Errorf("Test case: %s. Expected unhealthy conditions %v ,got: %v", tc.name, tc.expected.unhealthyConditions, unhealthyConditions)
		}
	}
}
