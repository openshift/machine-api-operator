package operators

import (
	"context"
	"time"

	"github.com/golang/glog"
	osconfigv1 "github.com/openshift/api/config/v1"
	e2e "github.com/openshift/cluster-api-actuator-pkg/pkg/e2e/framework"
	cov1helpers "github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func isStatusAvailable(client runtimeclient.Client, name string) bool {
	key := types.NamespacedName{
		Namespace: e2e.TestContext.MachineApiNamespace,
		Name:      name,
	}
	clusterOperator := &osconfigv1.ClusterOperator{}

	if err := wait.PollImmediate(1*time.Second, e2e.WaitShort, func() (bool, error) {
		if err := client.Get(context.TODO(), key, clusterOperator); err != nil {
			glog.Errorf("error querying api for OperatorStatus object: %v, retrying...", err)
			return false, nil
		}
		if cov1helpers.IsStatusConditionFalse(clusterOperator.Status.Conditions, osconfigv1.OperatorAvailable) {
			glog.Errorf("Condition: %q is false", osconfigv1.OperatorAvailable)
			return false, nil
		}
		if cov1helpers.IsStatusConditionTrue(clusterOperator.Status.Conditions, osconfigv1.OperatorProgressing) {
			glog.Errorf("Condition: %q is true", osconfigv1.OperatorProgressing)
			return false, nil
		}
		if cov1helpers.IsStatusConditionTrue(clusterOperator.Status.Conditions, osconfigv1.OperatorDegraded) {
			glog.Errorf("Condition: %q is true", osconfigv1.OperatorDegraded)
			return false, nil
		}
		return true, nil
	}); err != nil {
		glog.Errorf("Error checking isStatusAvailable: %v", err)
		return false
	}
	return true

}
