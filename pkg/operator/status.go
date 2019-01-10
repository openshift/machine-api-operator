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

// StatusReason is a MixedCaps string representing the reason for a
// status condition change.
type StatusReason string

// The default set of status change reasons.
const (
	ReasonEmpty      StatusReason = ""
	ReasonSyncing    StatusReason = "SyncingResources"
	ReasonSyncFailed StatusReason = "SyncingFailed"
)

// statusProgressing sets the Progressing condition to True, with the given
// reason and message, and sets both the Available and Failing conditions to
// False.
func (optr *Operator) statusProgressing(reason StatusReason, message string) error {
	conds := []osconfigv1.ClusterOperatorStatusCondition{
		{
			Type:    osconfigv1.OperatorProgressing,
			Status:  osconfigv1.ConditionTrue,
			Reason:  string(reason),
			Message: message,
		},
		{
			Type:   osconfigv1.OperatorAvailable,
			Status: osconfigv1.ConditionFalse,
		},
		{
			Type:   osconfigv1.OperatorFailing,
			Status: osconfigv1.ConditionFalse,
		},
	}

	return optr.syncStatus(conds)
}

// statusAvailable sets the Available condition to True, with the given reason
// and message, and sets both the Progressing and Failing conditions to False.
func (optr *Operator) statusAvailable(reason StatusReason, message string) error {
	conds := []osconfigv1.ClusterOperatorStatusCondition{
		{
			Type:    osconfigv1.OperatorAvailable,
			Status:  osconfigv1.ConditionTrue,
			Reason:  string(reason),
			Message: message,
		},
		{
			Type:   osconfigv1.OperatorProgressing,
			Status: osconfigv1.ConditionFalse,
		},

		{
			Type:   osconfigv1.OperatorFailing,
			Status: osconfigv1.ConditionFalse,
		},
	}

	return optr.syncStatus(conds)
}

// statusFailing sets the Failing condition to True, with the given reason and
// message, and sets the Progressing condition to False, and the Available
// condition to True.  This indicates that the operator is present and may be
// partially functioning, but is in a degraded or failing state.
func (optr *Operator) statusFailing(reason StatusReason, message string) error {
	conds := []osconfigv1.ClusterOperatorStatusCondition{
		{
			Type:    osconfigv1.OperatorFailing,
			Status:  osconfigv1.ConditionTrue,
			Reason:  string(reason),
			Message: message,
		},
		{
			Type:   osconfigv1.OperatorProgressing,
			Status: osconfigv1.ConditionFalse,
		},
		{
			Type:   osconfigv1.OperatorAvailable,
			Status: osconfigv1.ConditionTrue,
		},
	}

	return optr.syncStatus(conds)
}

//syncStatus applies the new condition to the mao ClusterOperator object.
func (optr *Operator) syncStatus(conds []osconfigv1.ClusterOperatorStatusCondition) error {
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

	for _, c := range conds {
		cvoresourcemerge.SetOperatorStatusCondition(&clusterOperator.Status.Conditions, c)
	}

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
