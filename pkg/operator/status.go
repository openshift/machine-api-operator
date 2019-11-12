package operator

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"

	"github.com/golang/glog"
	osconfigv1 "github.com/openshift/api/config/v1"
	cvoresourcemerge "github.com/openshift/cluster-version-operator/lib/resourcemerge"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

const (
	clusterOperatorName = "machine-api"
)

var (
	// This is to be compliant with
	// https://github.com/openshift/cluster-version-operator/blob/b57ee63baf65f7cb6e95a8b2b304d88629cfe3c0/docs/dev/clusteroperator.md#what-should-an-operator-report-with-clusteroperator-custom-resource
	// When known hazardous states for upgrades are determined
	// specific "Upgradeable=False" status can be added with messages for how admins
	// can resolve it.
	operatorUpgradeable = newClusterOperatorStatusCondition(osconfigv1.OperatorUpgradeable, osconfigv1.ConditionTrue, "", "")
)

// statusProgressing sets the Progressing condition to True, with the given
// reason and message, and sets both the Available and Degraded conditions to
// False.
func (optr *Operator) statusProgressing() error {
	desiredVersions := optr.operandVersions
	currentVersions, err := optr.getCurrentVersions()
	if err != nil {
		glog.Errorf("Error getting operator current versions: %v", err)
		return err
	}
	var isProgressing osconfigv1.ConditionStatus

	co, err := optr.getOrCreateClusterOperator()
	if err != nil {
		glog.Errorf("Failed to get or create Cluster Operator: %v", err)
		return err
	}

	var message string
	if !reflect.DeepEqual(desiredVersions, currentVersions) {
		glog.V(2).Info("Syncing status: progressing")
		message = fmt.Sprintf("Progressing towards %s", optr.printOperandVersions())
		optr.eventRecorder.Eventf(co, v1.EventTypeNormal, "Status upgrade", message)
		isProgressing = osconfigv1.ConditionTrue
	} else {
		glog.V(2).Info("Syncing status: re-syncing")
		message = fmt.Sprintf("Running resync for %s", optr.printOperandVersions())
		isProgressing = osconfigv1.ConditionFalse
	}

	conds := []osconfigv1.ClusterOperatorStatusCondition{
		newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, isProgressing, string(ReasonSyncing), message),
		newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionTrue, "", ""),
		newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionFalse, "", ""),
		operatorUpgradeable,
	}

	return optr.syncStatus(co, conds)
}

// statusAvailable sets the Available condition to True, with the given reason
// and message, and sets both the Progressing and Degraded conditions to False.
func (optr *Operator) statusAvailable() error {
	conds := []osconfigv1.ClusterOperatorStatusCondition{
		newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionTrue, string(ReasonEmpty),
			fmt.Sprintf("Cluster Machine API Operator is available at %s", optr.printOperandVersions())),
		newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, osconfigv1.ConditionFalse, "", ""),
		newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionFalse, "", ""),
		operatorUpgradeable,
	}

	co, err := optr.getOrCreateClusterOperator()
	if err != nil {
		return err
	}

	// 	important: we only write the version field if we report available at the present level
	co.Status.Versions = optr.operandVersions
	glog.V(2).Info("Syncing status: available")
	return optr.syncStatus(co, conds)
}

// statusDegraded sets the Degraded condition to True, with the given reason and
// message, and sets the Progressing condition to False, and the Available
// condition to True.  This indicates that the operator is present and may be
// partially functioning, but is in a degraded or failing state.
func (optr *Operator) statusDegraded(error string) error {
	desiredVersions := optr.operandVersions
	currentVersions, err := optr.getCurrentVersions()
	if err != nil {
		glog.Errorf("Error getting current versions: %v", err)
		return err
	}

	var message string
	if !reflect.DeepEqual(desiredVersions, currentVersions) {
		message = fmt.Sprintf("Failed when progressing towards %s because %s", optr.printOperandVersions(), error)
	} else {
		message = fmt.Sprintf("Failed to resync for %s because %s", optr.printOperandVersions(), error)
	}

	conds := []osconfigv1.ClusterOperatorStatusCondition{
		newClusterOperatorStatusCondition(osconfigv1.OperatorDegraded, osconfigv1.ConditionTrue,
			string(ReasonSyncFailed), message),
		newClusterOperatorStatusCondition(osconfigv1.OperatorProgressing, osconfigv1.ConditionFalse, "", ""),
		newClusterOperatorStatusCondition(osconfigv1.OperatorAvailable, osconfigv1.ConditionTrue, "", ""),
		operatorUpgradeable,
	}

	co, err := optr.getOrCreateClusterOperator()
	if err != nil {
		return err
	}
	optr.eventRecorder.Eventf(co, v1.EventTypeWarning, "Status degraded", error)
	glog.V(2).Info("Syncing status: degraded")
	return optr.syncStatus(co, conds)
}

func newClusterOperatorStatusCondition(conditionType osconfigv1.ClusterStatusConditionType,
	conditionStatus osconfigv1.ConditionStatus, reason string,
	message string) osconfigv1.ClusterOperatorStatusCondition {
	return osconfigv1.ClusterOperatorStatusCondition{
		Type:               conditionType,
		Status:             conditionStatus,
		LastTransitionTime: metav1.Now(),
		Reason:             reason,
		Message:            message,
	}
}

//syncStatus applies the new condition to the mao ClusterOperator object.
func (optr *Operator) syncStatus(co *osconfigv1.ClusterOperator, conds []osconfigv1.ClusterOperatorStatusCondition) error {
	for _, c := range conds {
		cvoresourcemerge.SetOperatorStatusCondition(&co.Status.Conditions, c)
	}

	_, err := optr.osClient.ConfigV1().ClusterOperators().UpdateStatus(co)
	return err
}

func (optr *Operator) getOrCreateClusterOperator() (*osconfigv1.ClusterOperator, error) {
	var co *osconfigv1.ClusterOperator
	co, err := optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		co = &osconfigv1.ClusterOperator{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterOperatorName,
			},
		}
		glog.Infof("clusterOperator %q does not exist, creating a new one: %v", clusterOperatorName, co)
		co, err = optr.osClient.ConfigV1().ClusterOperators().Create(co)
		if err != nil {
			return nil, fmt.Errorf("failed to create new clusterOperator: %v", err)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get clusterOperator %q: %v", clusterOperatorName, err)
	}

	// Related objects lets openshift/must-gather collect diagnostic content
	relatedObjects := []osconfigv1.ObjectReference{
		{
			Group:    "",
			Resource: "namespaces",
			Name:     optr.namespace,
		},
		{
			Group:     "machine.openshift.io",
			Resource:  "machines",
			Name:      "",
			Namespace: optr.namespace,
		},
		{
			Group:     "machine.openshift.io",
			Resource:  "machinesets",
			Name:      "",
			Namespace: optr.namespace,
		},
		{
			Group:     "rbac.authorization.k8s.io",
			Resource:  "roles",
			Name:      "",
			Namespace: optr.namespace,
		},
		{
			Group:    "rbac.authorization.k8s.io",
			Resource: "clusterroles",
			Name:     "machine-api-operator",
		},
		{
			Group:    "rbac.authorization.k8s.io",
			Resource: "clusterroles",
			Name:     "machine-api-controllers",
		},
	}
	if !equality.Semantic.DeepEqual(co.Status.RelatedObjects, relatedObjects) {
		co.Status.RelatedObjects = relatedObjects
		return optr.osClient.ConfigV1().ClusterOperators().UpdateStatus(co)
	}
	return co, nil
}

func (optr *Operator) getCurrentVersions() ([]osconfigv1.OperandVersion, error) {
	co, err := optr.getOrCreateClusterOperator()
	if err != nil {
		return nil, err
	}
	return co.Status.Versions, nil
}

func (optr *Operator) printOperandVersions() string {
	versionsOutput := []string{}
	for _, operand := range optr.operandVersions {
		versionsOutput = append(versionsOutput, fmt.Sprintf("%s: %s", operand.Name, operand.Version))
	}
	return strings.Join(versionsOutput, ", ")
}
