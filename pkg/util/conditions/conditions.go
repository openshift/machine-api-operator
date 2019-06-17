package conditions

import (
	"context"
	"fmt"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	machinesutil "github.com/openshift/machine-api-operator/pkg/util/machines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
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

// GetConditionsFromConfigMap returns unhealthy conditions from the config map
func GetConditionsFromConfigMap(c client.Client, namespace string) ([]UnhealthyCondition, error) {
	cmUnealthyConditions, err := getUnhealthyConditionsConfigMap(c, namespace)
	if err != nil {
		return nil, err
	}

	data, ok := cmUnealthyConditions.Data["conditions"]
	if !ok {
		return nil, fmt.Errorf("can not find \"conditions\" under the configmap")
	}

	var unealthyConditions UnhealthyConditions
	err = yaml.Unmarshal([]byte(data), &unealthyConditions)
	if err != nil {
		glog.Errorf("failed to umarshal: %v", err)
		return nil, err
	}
	return unealthyConditions.Items, nil
}

// GetMachineUnhealthyConditions returns machine unhealthy conditions
func GetMachineUnhealthyConditions(c client.Client, machine *mapiv1.Machine, unealthyConditions []UnhealthyCondition) ([]UnhealthyCondition, error) {
	node, err := machinesutil.GetNodeByMachine(c, machine)
	if err != nil {
		return nil, err
	}

	return GetNodeUnhealthyConditions(node, unealthyConditions), nil
}

// GetNodeUnhealthyConditions returns node unhealthy conditions
func GetNodeUnhealthyConditions(node *corev1.Node, unealthyConditions []UnhealthyCondition) []UnhealthyCondition {
	conditions := []UnhealthyCondition{}
	for _, c := range unealthyConditions {
		cond := GetNodeCondition(node, c.Name)
		if cond != nil && IsConditionsStatusesEqual(cond, &c) {
			conditions = append(conditions, c)
		}
	}
	return conditions
}

// getUnhealthyConditionsConfigMap returns unhealthy conditions config map
func getUnhealthyConditionsConfigMap(c client.Client, namespace string) (*corev1.ConfigMap, error) {
	cmUnhealtyConditions := &corev1.ConfigMap{}
	cmKey := types.NamespacedName{
		Name:      healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		Namespace: namespace,
	}
	err := c.Get(context.TODO(), cmKey, cmUnhealtyConditions)
	if err != nil {
		// Error reading the object - requeue the request
		if !errors.IsNotFound(err) {
			return nil, err
		}

		// creates dummy config map with default values if it does not exist
		cmUnhealtyConditions, err = createDummyUnhealthyConditionsConfigMap()
		if err != nil {
			return nil, err
		}
		glog.Infof(
			"ConfigMap %s not found under the namespace %s, fallback to default values: %s",
			healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
			namespace,
			cmUnhealtyConditions.Data["conditions"],
		)
	}
	return cmUnhealtyConditions, nil
}

// createDummyUnhealthyConditionsConfigMap creates dummy config map with default unhealthy conditions
func createDummyUnhealthyConditionsConfigMap() (*corev1.ConfigMap, error) {
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

// IsConditionsStatusesEqual returns true if conditions statuses equal, otherwise false
func IsConditionsStatusesEqual(cond *corev1.NodeCondition, unhealthyCond *UnhealthyCondition) bool {
	return cond.Status == unhealthyCond.Status
}
