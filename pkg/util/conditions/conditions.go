package conditions

import (
	"github.com/ghodss/yaml"

	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
)

// GetNodeCondition returns node condition by type
func GetNodeCondition(node *corev1.Node, conditionType corev1.NodeConditionType) *corev1.NodeCondition {
	for _, cond := range node.Status.Conditions {
		if cond.Type == conditionType {
			return &cond
		}
	}
	return nil
}

// UnhealthyConditions contains a list of UnhealthyCondition
type UnhealthyConditions struct {
	Items []UnhealthyCondition `json:"items"`
}

// UnhealthyCondition is the representation of unhealthy conditions under the config map
type UnhealthyCondition struct {
	Name    corev1.NodeConditionType `json:"name"`
	Status  corev1.ConditionStatus   `json:"status"`
	Timeout string                   `json:"timeout"`
}

// GetNodeUnhealthyConditions returns node unhealthy conditions
func GetNodeUnhealthyConditions(node *corev1.Node, unhealthyConditions []healthcheckingv1alpha1.UnhealthyNodeCondition) []healthcheckingv1alpha1.UnhealthyNodeCondition {
	conditions := []healthcheckingv1alpha1.UnhealthyNodeCondition{}
	for _, c := range unhealthyConditions {
		cond := GetNodeCondition(node, c.Name)
		if cond != nil && cond.Status == c.Status {
			conditions = append(conditions, c)
		}
	}
	return conditions
}

// CreateDummyUnhealthyConditionsConfigMap creates dummy config map with default unhealthy conditions
func CreateDummyUnhealthyConditionsConfigMap() (*corev1.ConfigMap, error) {
	unhealthyConditions := &UnhealthyConditions{
		Items: []UnhealthyCondition{
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
	}
	conditionsData, err := yaml.Marshal(unhealthyConditions)
	if err != nil {
		return nil, err
	}
	return &corev1.ConfigMap{Data: map[string]string{"conditions": string(conditionsData)}}, nil
}
