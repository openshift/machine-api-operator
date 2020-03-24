package operator

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/golang/glog"
	osconfigv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
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
// reason and message, and sets the upgradeable condition to True.  It does not
// modify any existing Available or Degraded conditions.
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
// message, and sets the upgradeable condition.  It does not modify any existing
// Available or Progressing conditions.
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
		v1helpers.SetStatusCondition(&co.Status.Conditions, c)
	}

	_, err := optr.osClient.ConfigV1().ClusterOperators().UpdateStatus(context.Background(), co, metav1.UpdateOptions{})
	return err
}

// relatedObjects returns the current list of ObjectReference's for the
// ClusterOperator objects's status.
func (optr *Operator) relatedObjects() []osconfigv1.ObjectReference {
	return []osconfigv1.ObjectReference{
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
		{
			Group:     "rbac.authorization.k8s.io",
			Resource:  "roles",
			Name:      "cloud-provider-config-reader",
			Namespace: "openshift-config",
		},
	}
}

// defaultStatusConditions returns the default set of status conditions for the
// ClusterOperator resource used on first creation of the ClusterOperator.
func (optr *Operator) defaultStatusConditions() []osconfigv1.ClusterOperatorStatusCondition {
	// All conditions default to False with no message.
	return []osconfigv1.ClusterOperatorStatusCondition{
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorProgressing,
			osconfigv1.ConditionFalse,
			"", "",
		),
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorDegraded,
			osconfigv1.ConditionFalse,
			"", "",
		),
		newClusterOperatorStatusCondition(
			osconfigv1.OperatorAvailable,
			osconfigv1.ConditionFalse,
			"", "",
		),
	}
}

// defaultClusterOperator returns the default ClusterOperator resource with
// default values for related objects and status conditions.
func (optr *Operator) defaultClusterOperator() *osconfigv1.ClusterOperator {
	return &osconfigv1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterOperatorName,
		},
		Status: osconfigv1.ClusterOperatorStatus{
			Conditions:     optr.defaultStatusConditions(),
			RelatedObjects: optr.relatedObjects(),
		},
	}
}

// updateRelatedObjects updates the ClusterOperator's related objects field if
// necessary and returns the updated ClusterOperator object.
func (optr *Operator) updateRelatedObjects(co *osconfigv1.ClusterOperator) (*osconfigv1.ClusterOperator, error) {
	relatedObjects := optr.relatedObjects()

	if !equality.Semantic.DeepEqual(co.Status.RelatedObjects, relatedObjects) {
		co.Status.RelatedObjects = relatedObjects
		return optr.osClient.ConfigV1().ClusterOperators().UpdateStatus(context.Background(), co, metav1.UpdateOptions{})
	}

	return co, nil
}

// setMissingStatusConditions checks that the given ClusterOperator has a value
// for each of the default status conditions, and sets the default value for any
// that are missing.
func (optr *Operator) setMissingStatusConditions(co *osconfigv1.ClusterOperator) (*osconfigv1.ClusterOperator, error) {
	var modified bool

	for _, c := range optr.defaultStatusConditions() {
		if v1helpers.FindStatusCondition(co.Status.Conditions, c.Type) == nil {
			v1helpers.SetStatusCondition(&co.Status.Conditions, c)
			modified = true
		}
	}

	if modified {
		return optr.osClient.ConfigV1().ClusterOperators().UpdateStatus(context.Background(), co, metav1.UpdateOptions{})
	}

	return co, nil
}

// getClusterOperator returns the current ClusterOperator.
func (optr *Operator) getClusterOperator() (*osconfigv1.ClusterOperator, error) {
	return optr.osClient.ConfigV1().ClusterOperators().
		Get(context.Background(), clusterOperatorName, metav1.GetOptions{})
}

// createClusterOperator creates the ClusterOperator and updates its status.
func (optr *Operator) createClusterOperator() (*osconfigv1.ClusterOperator, error) {
	defaultCO := optr.defaultClusterOperator()

	co, err := optr.osClient.ConfigV1().ClusterOperators().Create(context.Background(), defaultCO, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}

	co.Status = defaultCO.Status

	return optr.osClient.ConfigV1().ClusterOperators().UpdateStatus(context.Background(), co, metav1.UpdateOptions{})
}

// getOrCreateClusterOperator fetches the current ClusterOperator or creates a
// default one if not found -- ensuring the related objects list is current.
func (optr *Operator) getOrCreateClusterOperator() (*osconfigv1.ClusterOperator, error) {
	existing, err := optr.getClusterOperator()

	if errors.IsNotFound(err) {
		glog.Infof("ClusterOperator does not exist, creating a new one.")
		return optr.createClusterOperator()
	}

	if err != nil {
		return nil, fmt.Errorf("failed to get clusterOperator %q: %v", clusterOperatorName, err)
	}

	// Update any missing status conditions with their default value.
	existing, err = optr.setMissingStatusConditions(existing)
	if err != nil {
		return nil, fmt.Errorf("failed to set default conditions: %v", err)
	}

	return optr.updateRelatedObjects(existing)
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
