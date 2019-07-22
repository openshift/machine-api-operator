package conditions

import (
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
