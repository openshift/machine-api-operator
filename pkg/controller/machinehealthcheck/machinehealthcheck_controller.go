package machinehealthcheck

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	apimachineryutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	machineAnnotationKey          = "machine.openshift.io/machine"
	machineExternalAnnotationKey  = "host.metal3.io/external-remediation"
	nodeMasterLabel               = "node-role.kubernetes.io/master"
	machineRoleLabel              = "machine.openshift.io/cluster-api-machine-role"
	machineMasterRole             = "master"
	machinePhaseFailed            = "Failed"
	remediationStrategyAnnotation = "machine.openshift.io/remediation-strategy"
	remediationStrategyExternal   = mapiv1.RemediationStrategyType("external-baremetal")
	defaultNodeStartupTimeout     = 10 * time.Minute
	machineNodeNameIndex          = "machineNodeNameIndex"
	controllerName                = "machinehealthcheck-controller"

	// Event types
	// EventRemediationRestricted is emitted in case when machine remediation
	// is restricted by remediation circuit shorting logic
	EventRemediationRestricted string = "RemediationRestricted"
	// EventDetectedUnhealthy is emitted in case a node asociated with a
	// machine was detected unhealthy
	EventDetectedUnhealthy string = "DetectedUnhealthy"
	// EventSkippedMaster is emitted in case an unhealthy node (or a machine
	// associated with the node) has Master role and external remediation strategy
	// is not enabled
	EventSkippedMaster string = "SkippedMaster"
	// EventMachineDeletionFailed is emitted in case remediation of a machine
	// is required but deletion of its Machine object failed
	EventMachineDeletionFailed string = "MachineDeletionFailed"
	// EventMachineDeleted is emitted when machine was successfully remediated
	// by deleting its Machine object
	EventMachineDeleted string = "MachineDeleted"
	// EventExternalAnnotationFailed is emitted in case adding external annotation
	// to a Node object failed
	EventExternalAnnotationFailed string = "ExternalAnnotationFailed"
	// EventExternalAnnotationAdded is emitted when external annotation was
	// successfully added to a Node object
	EventExternalAnnotationAdded string = "ExternalAnnotationAdded"
)

// Add creates a new MachineHealthCheck Controller and adds it to the Manager. The Manager will set fields on the Controller
// and start it when the Manager is started.
func Add(mgr manager.Manager, opts manager.Options) error {
	r, err := newReconciler(mgr, opts)
	if err != nil {
		return fmt.Errorf("error building reconciler: %v", err)
	}
	return add(mgr, r, r.mhcRequestsFromMachine, r.mhcRequestsFromNode)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, opts manager.Options) (*ReconcileMachineHealthCheck, error) {
	if err := mgr.GetCache().IndexField(&mapiv1.Machine{},
		machineNodeNameIndex,
		indexMachineByNodeName,
	); err != nil {
		return nil, fmt.Errorf("error setting index fields: %v", err)
	}

	return &ReconcileMachineHealthCheck{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		namespace: opts.Namespace,
		recorder:  mgr.GetEventRecorderFor(controllerName),
	}, nil
}

func indexMachineByNodeName(object runtime.Object) []string {
	machine, ok := object.(*mapiv1.Machine)
	if !ok {
		glog.Warningf("Expected a machine for indexing field, got: %T", object)
		return nil
	}

	if machine.Status.NodeRef != nil {
		return []string{machine.Status.NodeRef.Name}
	}

	return nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, mapMachineToMHC, mapNodeToMHC handler.ToRequestsFunc) error {
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &mapiv1.MachineHealthCheck{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &mapiv1.Machine{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapMachineToMHC})
	if err != nil {
		return err
	}

	return c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapNodeToMHC})
}

var _ reconcile.Reconciler = &ReconcileMachineHealthCheck{}

// ReconcileMachineHealthCheck reconciles a MachineHealthCheck object
type ReconcileMachineHealthCheck struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client    client.Client
	scheme    *runtime.Scheme
	namespace string
	recorder  record.EventRecorder
}

type target struct {
	Machine mapiv1.Machine
	Node    *corev1.Node
	MHC     mapiv1.MachineHealthCheck
}

// Reconcile fetch all targets for a MachineHealthCheck request and does health checking for each of them
func (r *ReconcileMachineHealthCheck) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	glog.Infof("Reconciling %s", request.String())

	mhc := &mapiv1.MachineHealthCheck{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, mhc); err != nil {
		if apimachineryerrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			return reconcile.Result{}, nil
		}
		glog.Errorf("Reconciling %s: failed to get MHC: %v", request.String(), err)
		return reconcile.Result{}, err
	}

	// fetch all targets
	glog.V(3).Infof("Reconciling %s: finding targets", request.String())
	targets, err := r.getTargetsFromMHC(*mhc)
	if err != nil {
		return reconcile.Result{}, err
	}
	totalTargets := len(targets)

	nodeStartupTimeout, err := getNodeStartupTimeout(mhc)
	if err != nil {
		glog.Errorf("Reconciling %s: error getting NodeStartupTimeout: %v", request.String(), err)
		return reconcile.Result{}, err
	}

	// health check all targets and reconcile mhc status
	currentHealthy, needRemediationTargets, nextCheckTimes, errList := r.healthCheckTargets(targets, nodeStartupTimeout)
	if err := r.reconcileStatus(mhc, totalTargets, currentHealthy); err != nil {
		glog.Errorf("Reconciling %s: error patching status: %v", request.String(), err)
		return reconcile.Result{}, err
	}

	// check MHC current health against MaxUnhealthy
	if !isAllowedRemediation(mhc) {
		glog.Warningf("Reconciling %s: total targets: %v,  maxUnhealthy: %v, unhealthy: %v. Short-circuiting remediation",
			request.String(),
			totalTargets,
			mhc.Spec.MaxUnhealthy,
			totalTargets-currentHealthy,
		)
		r.recorder.Eventf(
			mhc,
			corev1.EventTypeWarning,
			EventRemediationRestricted,
			"Remediation restricted due to exceeded number of unhealthy machines (total: %v, unhealthy: %v, maxUnhealthy: %v)",
			totalTargets,
			totalTargets-currentHealthy,
			mhc.Spec.MaxUnhealthy,
		)
		return reconcile.Result{Requeue: true}, nil
	}
	glog.V(3).Infof("Reconciling %s: monitoring MHC: total targets: %v,  maxUnhealthy: %v, unhealthy: %v. Remediations are allowed",
		request.String(),
		totalTargets,
		mhc.Spec.MaxUnhealthy,
		totalTargets-currentHealthy,
	)

	// remediate
	for _, t := range needRemediationTargets {
		glog.V(3).Infof("Reconciling %s: meet unhealthy criteria, triggers remediation", t.string())
		if err := t.remediate(r); err != nil {
			glog.Errorf("Reconciling %s: error remediating: %v", t.string(), err)
			errList = append(errList, err)
		}
	}

	// return values
	if len(errList) > 0 {
		requeueError := apimachineryutilerrors.NewAggregate(errList)
		glog.V(3).Infof("Reconciling %s: there were errors, requeuing: %v", request.String(), requeueError)
		return reconcile.Result{}, requeueError
	}

	if minNextCheck := minDuration(nextCheckTimes); minNextCheck > 0 {
		glog.V(3).Infof("Reconciling %s: some targets might go unhealthy. Ensuring a requeue happens in %v", request.String(), minNextCheck)
		return reconcile.Result{RequeueAfter: minNextCheck}, nil
	}

	glog.V(3).Infof("Reconciling %s: no more targets meet unhealthy criteria", request.String())
	return reconcile.Result{}, nil
}

func isAllowedRemediation(mhc *mapiv1.MachineHealthCheck) bool {
	if mhc.Spec.MaxUnhealthy == nil {
		return true
	}
	maxUnhealthy, err := getValueFromIntOrPercent(mhc.Spec.MaxUnhealthy, derefInt(mhc.Status.ExpectedMachines), false)
	if err != nil {
		glog.Errorf("%s: error decoding maxUnhealthy, remediation won't be allowed: %v", namespacedName(mhc), err)
		return false
	}

	// if noHealthy are above maxUnhealthy we short circuit any farther remediation
	noHealthy := derefInt(mhc.Status.ExpectedMachines) - derefInt(mhc.Status.CurrentHealthy)
	return (maxUnhealthy - noHealthy) >= 0
}

func derefInt(i *int) int {
	if i != nil {
		return *i
	}
	return 0
}

func (r *ReconcileMachineHealthCheck) reconcileStatus(mhc *mapiv1.MachineHealthCheck, targets, currentHealthy int) error {
	baseToPatch := client.MergeFrom(mhc.DeepCopy())
	mhc.Status.ExpectedMachines = &targets
	mhc.Status.CurrentHealthy = &currentHealthy

	if err := r.client.Status().Patch(context.Background(), mhc, baseToPatch); err != nil {
		return err
	}
	return nil
}

// healthCheckTargets health checks a slice of targets
// and gives a data to measure the average health
func (r *ReconcileMachineHealthCheck) healthCheckTargets(targets []target, timeoutForMachineToHaveNode time.Duration) (int, []target, []time.Duration, []error) {
	var nextCheckTimes []time.Duration
	var errList []error
	var needRemediationTargets []target
	var currentHealthy int
	for _, t := range targets {
		glog.V(3).Infof("Reconciling %s: health checking", t.string())
		needsRemediation, nextCheck, err := t.needsRemediation(timeoutForMachineToHaveNode)
		if err != nil {
			glog.Errorf("Reconciling %s: error health checking: %v", t.string(), err)
			errList = append(errList, err)
			continue
		}

		if needsRemediation {
			needRemediationTargets = append(needRemediationTargets, t)
			continue
		}

		if nextCheck > 0 {
			glog.V(3).Infof("Reconciling %s: is likely to go unhealthy in %v", t.string(), nextCheck)
			r.recorder.Eventf(
				&t.Machine,
				corev1.EventTypeNormal,
				EventDetectedUnhealthy,
				"Machine %v has unhealthy node %v",
				t.string(),
				t.nodeName(),
			)
			nextCheckTimes = append(nextCheckTimes, nextCheck)
			continue
		}

		if t.Machine.DeletionTimestamp == nil {
			currentHealthy++
		}
	}
	return currentHealthy, needRemediationTargets, nextCheckTimes, errList
}

func (r *ReconcileMachineHealthCheck) getTargetsFromMHC(mhc mapiv1.MachineHealthCheck) ([]target, error) {
	machines, err := r.getMachinesFromMHC(mhc)
	if err != nil {
		return nil, fmt.Errorf("error getting machines from MHC: %v", err)
	}
	if len(machines) == 0 {
		return nil, nil
	}

	var targets []target
	for k := range machines {
		target := target{
			MHC:     mhc,
			Machine: machines[k],
		}
		node, err := r.getNodeFromMachine(machines[k])
		if err != nil {
			if !apimachineryerrors.IsNotFound(err) {
				return nil, fmt.Errorf("error getting node: %v", err)
			}
			// a node with only a name represents a
			// not found node in the target
			node.Name = machines[k].Status.NodeRef.Name
		}
		target.Node = node
		targets = append(targets, target)
	}
	return targets, nil
}

func (r *ReconcileMachineHealthCheck) getMachinesFromMHC(mhc mapiv1.MachineHealthCheck) ([]mapiv1.Machine, error) {
	selector, err := metav1.LabelSelectorAsSelector(&mhc.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("failed to build selector")
	}

	options := client.ListOptions{
		LabelSelector: selector,
		Namespace:     mhc.GetNamespace(),
	}
	machineList := &mapiv1.MachineList{}
	if err := r.client.List(context.Background(), machineList, &options); err != nil {
		return nil, fmt.Errorf("failed to list machines: %v", err)
	}
	return machineList.Items, nil
}

func (r *ReconcileMachineHealthCheck) getMachineFromNode(nodeName string) (*mapiv1.Machine, error) {
	machineList := &mapiv1.MachineList{}
	if err := r.client.List(
		context.TODO(),
		machineList,
		client.MatchingFields{machineNodeNameIndex: nodeName},
	); err != nil {
		return nil, fmt.Errorf("failed getting machine list: %v", err)
	}
	if len(machineList.Items) != 1 {
		return nil, fmt.Errorf("expecting one machine for node %v, got: %v", nodeName, machineList.Items)
	}
	return &machineList.Items[0], nil
}

func (r *ReconcileMachineHealthCheck) mhcRequestsFromNode(o handler.MapObject) []reconcile.Request {
	glog.V(4).Infof("Getting MHC requests from node %q", namespacedName(o.Meta).String())
	node := &corev1.Node{}
	if err := r.client.Get(context.Background(), namespacedName(o.Meta), node); err != nil {
		if apimachineryerrors.IsNotFound(err) {
			node.Name = o.Meta.GetName()
		} else {
			glog.Errorf("No-op: Unable to retrieve node %q from store: %v", namespacedName(o.Meta).String(), err)
			return nil
		}
	}

	machine, err := r.getMachineFromNode(node.Name)
	if machine == nil || err != nil {
		glog.Errorf("No-op: Unable to retrieve machine from node %q: %v", namespacedName(node).String(), err)
		return nil
	}

	mhcList := &mapiv1.MachineHealthCheckList{}
	if err := r.client.List(context.Background(), mhcList); err != nil {
		glog.Errorf("No-op: Unable to list mhc: %v", err)
		return nil
	}

	// get all MHCs which selectors match this machine
	var requests []reconcile.Request
	for k := range mhcList.Items {
		if hasMatchingLabels(&mhcList.Items[k], machine) {
			requests = append(requests, reconcile.Request{NamespacedName: namespacedName(&mhcList.Items[k])})
		}
	}
	return requests
}

func (r *ReconcileMachineHealthCheck) mhcRequestsFromMachine(o handler.MapObject) []reconcile.Request {
	glog.V(4).Infof("Getting MHC requests from machine %q", namespacedName(o.Meta).String())
	machine := &mapiv1.Machine{}
	if err := r.client.Get(context.Background(),
		client.ObjectKey{
			Namespace: o.Meta.GetNamespace(),
			Name:      o.Meta.GetName(),
		},
		machine,
	); err != nil {
		glog.Errorf("No-op: Unable to retrieve machine %q from store: %v", namespacedName(o.Meta).String(), err)
		return nil
	}

	mhcList := &mapiv1.MachineHealthCheckList{}
	if err := r.client.List(context.Background(), mhcList); err != nil {
		glog.Errorf("No-op: Unable to list mhc: %v", err)
		return nil
	}

	var requests []reconcile.Request
	for k := range mhcList.Items {
		if hasMatchingLabels(&mhcList.Items[k], machine) {
			requests = append(requests, reconcile.Request{NamespacedName: namespacedName(&mhcList.Items[k])})
		}
	}
	return requests
}

func (t *target) remediate(r *ReconcileMachineHealthCheck) error {
	glog.Infof(" %s: start remediation logic", t.string())
	if !t.hasControllerOwner() {
		glog.Infof("%s: no controller owner, skipping remediation", t.string())
		return nil
	}

	remediationStrategy, ok := t.MHC.Annotations[remediationStrategyAnnotation]
	if ok {
		if mapiv1.RemediationStrategyType(remediationStrategy) == remediationStrategyExternal {
			return t.remediationStrategyExternal(r)
		}
	}

	glog.Infof("%s: deleting", t.string())
	if err := r.client.Delete(context.TODO(), &t.Machine); err != nil {
		r.recorder.Eventf(
			&t.Machine,
			corev1.EventTypeWarning,
			EventMachineDeletionFailed,
			"Machine %v remediation failed: unable to delete Machine object: %v",
			t.string(),
			err,
		)
		return fmt.Errorf("%s: failed to delete machine: %v", t.string(), err)
	}
	r.recorder.Eventf(
		&t.Machine,
		corev1.EventTypeNormal,
		EventMachineDeleted,
		"Machine %v has been remediated by requesting to delete Machine object",
		t.string(),
	)
	return nil
}

func (t *target) remediationStrategyExternal(r *ReconcileMachineHealthCheck) error {
	// we already have external annotation on the machine, stop reconcile
	if _, ok := t.Machine.Annotations[machineExternalAnnotationKey]; ok {
		return nil
	}

	if t.Machine.Annotations == nil {
		t.Machine.Annotations = map[string]string{}
	}

	glog.Infof("Machine %s has been unhealthy for too long, adding external annotation", t.Machine.Name)
	t.Machine.Annotations[machineExternalAnnotationKey] = ""
	if err := r.client.Update(context.TODO(), &t.Machine); err != nil {
		r.recorder.Eventf(
			&t.Machine,
			corev1.EventTypeWarning,
			EventExternalAnnotationFailed,
			"Requesting external remediation of node associated with machine %v failed: %v",
			t.string(),
			err,
		)
		return err
	}
	r.recorder.Eventf(
		&t.Machine,
		corev1.EventTypeNormal,
		EventExternalAnnotationAdded,
		"Requesting external remediation of node associated with machine %v",
		t.string(),
	)
	return nil
}

func (r *ReconcileMachineHealthCheck) getNodeFromMachine(machine mapiv1.Machine) (*corev1.Node, error) {
	if machine.Status.NodeRef == nil {
		return nil, nil
	}

	node := &corev1.Node{}
	nodeKey := types.NamespacedName{
		Namespace: machine.Status.NodeRef.Namespace,
		Name:      machine.Status.NodeRef.Name,
	}
	err := r.client.Get(context.TODO(), nodeKey, node)
	return node, err
}

func (t *target) string() string {
	return fmt.Sprintf("%s/%s/%s/%s",
		t.MHC.GetNamespace(),
		t.MHC.GetName(),
		t.Machine.GetName(),
		t.nodeName(),
	)
}

func (t *target) nodeName() string {
	if t.Node != nil {
		return t.Node.GetName()
	}
	return ""
}

func (t *target) needsRemediation(timeoutForMachineToHaveNode time.Duration) (bool, time.Duration, error) {
	var nextCheckTimes []time.Duration
	now := time.Now()

	// machine has failed
	if derefStringPointer(t.Machine.Status.Phase) == machinePhaseFailed {
		glog.V(3).Infof("%s: unhealthy: machine phase is %q", t.string(), machinePhaseFailed)
		return true, time.Duration(0), nil
	}

	// the node has not been set yet
	if t.Node == nil {
		// status not updated yet
		if t.Machine.Status.LastUpdated == nil {
			return false, timeoutForMachineToHaveNode, nil
		}
		if t.Machine.Status.LastUpdated.Add(timeoutForMachineToHaveNode).Before(now) {
			glog.V(3).Infof("%s: unhealthy: machine has no node after %v", t.string(), timeoutForMachineToHaveNode)
			return true, time.Duration(0), nil
		}
		durationUnhealthy := now.Sub(t.Machine.Status.LastUpdated.Time)
		nextCheck := timeoutForMachineToHaveNode - durationUnhealthy + time.Second
		return false, nextCheck, nil
	}

	// the node does not exist
	if t.Node != nil && t.Node.UID == "" {
		return true, time.Duration(0), nil
	}

	// check conditions
	for _, c := range t.MHC.Spec.UnhealthyConditions {
		now := time.Now()
		nodeCondition := conditions.GetNodeCondition(t.Node, c.Type)

		timeout, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return false, time.Duration(0), fmt.Errorf("error parsing duration: %v", err)
		}

		// Skip when current node condition is different from the one reported
		// in the MachineHealthCheck.
		if nodeCondition == nil || nodeCondition.Status != c.Status {
			continue
		}

		// If the condition has been in the unhealthy state for longer than the
		// timeout, return true with no requeue time.
		if nodeCondition.LastTransitionTime.Add(timeout).Before(now) {
			glog.V(3).Infof("%s: unhealthy: condition %v in state %v longer than %v", t.string(), c.Type, c.Status, c.Timeout)
			return true, time.Duration(0), nil
		}

		durationUnhealthy := now.Sub(nodeCondition.LastTransitionTime.Time)
		nextCheck := timeout - durationUnhealthy + time.Second
		if nextCheck > 0 {
			nextCheckTimes = append(nextCheckTimes, nextCheck)
		}
	}
	return false, minDuration(nextCheckTimes), nil
}

func (t *target) hasControllerOwner() bool {
	return metav1.GetControllerOf(&t.Machine) != nil
}

func derefStringPointer(stringPointer *string) string {
	if stringPointer != nil {
		return *stringPointer
	}
	return ""
}

func minDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return time.Duration(0)
	}

	minDuration := time.Duration(1 * time.Hour)
	for _, nc := range durations {
		if nc < minDuration {
			minDuration = nc
		}
	}
	return minDuration
}

func namespacedName(obj metav1.Object) types.NamespacedName {
	return types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func hasMatchingLabels(machineHealthCheck *mapiv1.MachineHealthCheck, machine *mapiv1.Machine) bool {
	selector, err := metav1.LabelSelectorAsSelector(&machineHealthCheck.Spec.Selector)
	if err != nil {
		glog.Warningf("unable to convert selector: %v", err)
		return false
	}
	// If the selector is empty, all machines are considered to match
	if selector.Empty() {
		return true
	}
	if !selector.Matches(labels.Set(machine.Labels)) {
		glog.V(4).Infof("%q machine has mismatched labels for MHC %q", machine.GetName(), machineHealthCheck.GetName())
		return false
	}
	return true
}

func getNodeStartupTimeout(mhc *mapiv1.MachineHealthCheck) (time.Duration, error) {
	if mhc.Spec.NodeStartupTimeout == "" {
		return defaultNodeStartupTimeout, nil
	}

	timeout, err := time.ParseDuration(mhc.Spec.NodeStartupTimeout)
	if err != nil {
		return time.Duration(0), fmt.Errorf("error parsing NodeStartupTimeout: %v", err)
	}
	return timeout, nil
}

// getValueFromIntOrPercent returns the integer number value based on the
// percentage of the total or absolute number dependent on the IntOrString given
//
// The following code is copied from https://github.com/kubernetes/apimachinery/blob/1a505bc60c6dfb15cb18a8cdbfa01db042156fe2/pkg/util/intstr/intstr.go#L154-L185
// But fixed so that string values aren't always assumed to be percentages
// See https://github.com/kubernetes/kubernetes/issues/89082 for details
func getValueFromIntOrPercent(intOrPercent *intstr.IntOrString, total int, roundUp bool) (int, error) {
	if intOrPercent == nil {
		return 0, errors.New("nil value for IntOrString")
	}
	value, isPercent, err := getIntOrPercentValue(intOrPercent)
	if err != nil {
		return 0, fmt.Errorf("invalid value for IntOrString: %v", err)
	}
	if isPercent {
		if roundUp {
			value = int(math.Ceil(float64(value) * (float64(total)) / 100))
		} else {
			value = int(math.Floor(float64(value) * (float64(total)) / 100))
		}
	}
	return value, nil
}

// getIntOrPercentValue returns the integer value of the IntOrString and
// determines if this value is a percentage or absolute number
//
// The following code is copied from https://github.com/kubernetes/apimachinery/blob/1a505bc60c6dfb15cb18a8cdbfa01db042156fe2/pkg/util/intstr/intstr.go#L154-L185
// But fixed so that string values aren't always assumed to be percentages
// See https://github.com/kubernetes/kubernetes/issues/89082 for details
func getIntOrPercentValue(intOrStr *intstr.IntOrString) (int, bool, error) {
	switch intOrStr.Type {
	case intstr.Int:
		return intOrStr.IntValue(), false, nil
	case intstr.String:
		isPercent := false
		s := intOrStr.StrVal
		if strings.Contains(s, "%") {
			isPercent = true
			s = strings.Replace(intOrStr.StrVal, "%", "", -1)
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0, isPercent, fmt.Errorf("invalid value %q: %v", intOrStr.StrVal, err)
		}
		return int(v), isPercent, nil
	}
	return 0, false, fmt.Errorf("invalid type: neither int nor percentage")
}
