package machines

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetMachineMachineDisruptionBudgets returns list of machine disruption budgets that suit for the machine
func GetMachineMachineDisruptionBudgets(c client.Client, machine *mapiv1.Machine) ([]*healthcheckingv1alpha1.MachineDisruptionBudget, error) {
	if len(machine.Labels) == 0 {
		return nil, fmt.Errorf("no MachineDisruptionBudgets found for machine %v because it has no labels", machine.Name)
	}

	list := &healthcheckingv1alpha1.MachineDisruptionBudgetList{}
	err := c.List(context.TODO(), list, client.InNamespace(machine.Namespace))
	if err != nil {
		return nil, err
	}

	var mdbs []*healthcheckingv1alpha1.MachineDisruptionBudget
	for i := range list.Items {
		mdb := &list.Items[i]
		selector, err := metav1.LabelSelectorAsSelector(mdb.Spec.Selector)
		if err != nil {
			glog.Warningf("invalid selector: %v", err)
			continue
		}

		// If a mdb with a nil or empty selector creeps in, it should match nothing, not everything.
		if selector.Empty() || !selector.Matches(labels.Set(machine.Labels)) {
			continue
		}
		mdbs = append(mdbs, mdb)
	}

	return mdbs, nil
}

// GetMahcinesByLabelSelector returns machines that suit to the label selector
func GetMahcinesByLabelSelector(c client.Client, selector *metav1.LabelSelector, namespace string) (*mapiv1.MachineList, error) {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}
	if sel.Empty() {
		return nil, nil
	}

	machines := &mapiv1.MachineList{}
	listOptions := &client.ListOptions{
		Namespace:     namespace,
		LabelSelector: sel,
	}
	err = c.List(context.TODO(), machines, client.UseListOptions(listOptions))
	if err != nil {
		return nil, err
	}
	return machines, nil
}

// GetNodeByMachine get the node object by machine object
func GetNodeByMachine(c client.Client, machine *mapiv1.Machine) (*corev1.Node, error) {
	if machine.Status.NodeRef == nil {
		glog.Errorf("machine %s does not have NodeRef", machine.Name)
		return nil, fmt.Errorf("machine %s does not have NodeRef", machine.Name)
	}
	node := &corev1.Node{}
	nodeKey := types.NamespacedName{
		Namespace: machine.Status.NodeRef.Namespace,
		Name:      machine.Status.NodeRef.Name,
	}
	err := c.Get(context.TODO(), nodeKey, node)
	return node, err
}

// IsMaster returns true if machine is master, otherwise false
func IsMaster(c client.Client, machine *mapiv1.Machine) bool {
	masterLabels := []string{
		"node-role.kubernetes.io/master",
	}

	node, err := GetNodeByMachine(c, machine)
	if err != nil {
		glog.Warningf("Couldn't get node for machine %s", machine.Name)
		return false
	}
	nodeLabels := labels.Set(node.Labels)
	for _, masterLabel := range masterLabels {
		if nodeLabels.Has(masterLabel) {
			return true
		}
	}
	return false
}
