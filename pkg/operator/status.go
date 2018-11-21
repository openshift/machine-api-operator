package operator

import (
	osconfigv1 "github.com/openshift/api/config/v1"
	osclientsetv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	cvoresourcemerge "github.com/openshift/cluster-version-operator/lib/resourcemerge"
	"github.com/openshift/machine-api-operator/pkg/version"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

//syncStatus applies the new condition to the mao ClusterOperator object.
func (optr *Operator) syncStatus(cond osconfigv1.ClusterOperatorStatusCondition) error {
	// to report the status of all the managed components.
	clusterOperator := &osconfigv1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: optr.namespace,
			Name:      optr.name,
		},
		Status: osconfigv1.ClusterOperatorStatus{
			Version: version.Raw,
		},
	}
	cvoresourcemerge.SetOperatorStatusCondition(&clusterOperator.Status.Conditions, cond)
	_, _, err := ApplyClusterOperator(optr.osClient.ConfigV1(), clusterOperator)
	return err
}

// ApplyClusterOperator applies the required ClusterOperator
func ApplyClusterOperator(client osclientsetv1.ClusterOperatorsGetter, required *osconfigv1.ClusterOperator) (*osconfigv1.ClusterOperator, bool, error) {
	existing, err := client.ClusterOperators().Get(required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := client.ClusterOperators().Create(required)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := pointer.BoolPtr(false)
	cvoresourcemerge.EnsureClusterOperatorStatus(modified, existing, *required)
	if !*modified {
		return existing, false, nil
	}

	actual, err := client.ClusterOperators().UpdateStatus(existing)
	return actual, true, err
}
