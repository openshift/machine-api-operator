package machines

import (
	"context"
	"fmt"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsMachineHealthy returns true if the the machine is running and machine node is healthy
func IsMachineHealthy(c client.Client, machine *mapiv1.Machine) bool {
	if machine.Status.NodeRef == nil {
		glog.Infof("machine %s does not have NodeRef", machine.Name)
		return false
	}

	node := &v1.Node{}
	key := client.ObjectKey{Namespace: metav1.NamespaceNone, Name: machine.Status.NodeRef.Name}
	err := c.Get(context.TODO(), key, node)
	if err != nil {
		glog.Errorf("failed to fetch node for machine %s", machine.Name)
		return false
	}

	readyCond := conditions.GetNodeCondition(node, v1.NodeReady)
	if readyCond == nil {
		glog.Infof("node %s does have 'Ready' condition", machine.Name)
		return false
	}

	if readyCond.Status != v1.ConditionTrue {
		glog.Infof("node %s does have has 'Ready' condition with the status %s", machine.Name, readyCond.Status)
		return false
	}
	return true
}

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
