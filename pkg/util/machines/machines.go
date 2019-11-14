package machines

import (
	"context"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// IsMachineHealthy returns true if the the machine is running and machine node is healthy
func IsMachineHealthy(c client.Client, machine *mapiv1.Machine) bool {
	if machine.Status.NodeRef == nil {
		glog.V(4).Infof("machine %s does not have NodeRef", machine.Name)
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
		glog.V(4).Infof("node %s does have 'Ready' condition", machine.Name)
		return false
	}

	if readyCond.Status != v1.ConditionTrue {
		glog.V(4).Infof("node %s does have has 'Ready' condition with the status %s", machine.Name, readyCond.Status)
		return false
	}
	return true
}
