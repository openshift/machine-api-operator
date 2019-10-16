package machinehealthcheck

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	corev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	apimachineryutilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	machineAnnotationKey        = "machine.openshift.io/machine"
	machineRebootAnnotationKey  = "healthchecking.openshift.io/machine-remediation-reboot"
	ownerControllerKind         = "MachineSet"
	nodeMasterLabel             = "node-role.kubernetes.io/master"
	machineRoleLabel            = "machine.openshift.io/cluster-api-machine-role"
	machineMasterRole           = "master"
	machinePhaseFailed          = "Failed"
	remediationStrategyReboot   = healthcheckingv1alpha1.RemediationStrategyType("reboot")
	timeoutForMachineToHaveNode = 10 * time.Minute
)

// Add creates a new MachineHealthCheck Controller and adds it to the Manager. The Manager will set fields on the Controller
// and start it when the Manager is started.
func Add(mgr manager.Manager, opts manager.Options) error {
	r := newReconciler(mgr, opts)
	return add(mgr, r, r.mhcRequestsFromMachine, r.mhcRequestsFromNode)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager, opts manager.Options) *ReconcileMachineHealthCheck {
	return &ReconcileMachineHealthCheck{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		namespace: opts.Namespace,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, mapMachineToMHC, mapNodeToMHC handler.ToRequestsFunc) error {
	c, err := controller.New("machinehealthcheck-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &healthcheckingv1alpha1.MachineHealthCheck{}}, &handler.EnqueueRequestForObject{})
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
}

type target struct {
	Machine mapiv1.Machine
	Node    *corev1.Node
	MHC     healthcheckingv1alpha1.MachineHealthCheck
}

// Reconcile fetch all targets for a MachineHealthCheck request and does health checking for each of them
func (r *ReconcileMachineHealthCheck) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	glog.Infof("Reconciling %s", request.String())

	mhc := &healthcheckingv1alpha1.MachineHealthCheck{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, mhc); err != nil {
		if apimachineryerrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			return reconcile.Result{}, nil
		}
		glog.Errorf("Reconciling %s: failed to get MHC: %v", request.String(), err)
		return reconcile.Result{}, err
	}

	glog.V(3).Infof("Reconciling %s: finding targets", request.String())
	targets, err := r.getTargetsFromMHC(*mhc)
	if err != nil {
		return reconcile.Result{}, err
	}

	// TODO: short circuit logic goes here:
	// Count all unhealthy targets, compare with allowed API field and update status
	var nextCheckTimes []time.Duration
	var errList []error
	for _, t := range targets {
		glog.V(3).Infof("Reconciling %s: health checking", t.string())
		unhealthy, nextCheck, err := t.isUnhealthy()
		if err != nil {
			glog.Errorf("Reconciling %s: error health checking: %v", t.string(), err)
			errList = append(errList, err)
			continue
		}

		if unhealthy {
			glog.V(3).Infof("Reconciling %s: meet unhealthy criteria, triggers remediation", t.string())
			if err := r.remediate(t); err != nil {
				glog.Errorf("Reconciling %s: error remediating: %v", t.string(), err)
				errList = append(errList, err)
			}
			continue
		}
		if nextCheck > 0 {
			glog.V(3).Infof("Reconciling %s: is likely to go unhealthy in %v", t.string(), nextCheck)
			nextCheckTimes = append(nextCheckTimes, nextCheck)
		}
	}

	if len(errList) > 0 {
		requeueError := apimachineryutilerrors.NewAggregate(errList)
		glog.V(3).Infof("Reconciling %s: there were errors, requeuing: %v", request.String(), requeueError)
		return reconcile.Result{}, requeueError
	}

	if minNextCheck := minDuration(nextCheckTimes); minNextCheck > 0 {
		glog.V(3).Infof("Reconciling %s: some targets might go unhealthy. Ensuring a requeue happens in %v", request.String(), minNextCheck)
		return reconcile.Result{RequeueAfter: minNextCheck}, nil
	}

	glog.V(3).Infof("Reconciling %s: no targets meet unhealthy criteria", request.String())
	return reconcile.Result{}, nil
}

func (r *ReconcileMachineHealthCheck) getTargetsFromMHC(mhc healthcheckingv1alpha1.MachineHealthCheck) ([]target, error) {
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

func (r *ReconcileMachineHealthCheck) getMachinesFromMHC(mhc healthcheckingv1alpha1.MachineHealthCheck) ([]mapiv1.Machine, error) {
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

func (r *ReconcileMachineHealthCheck) getMachineFromNode(node corev1.Node) (*mapiv1.Machine, error) {
	machineKey, ok := node.Annotations[machineAnnotationKey]
	if !ok {
		glog.V(4).Infof("no machine annotation for node %s", node.GetName())
		return nil, nil
	}
	glog.V(4).Infof("Node %s is annotated with machine %s", node.GetName(), machineKey)

	namespace, machineName, err := cache.SplitMetaNamespaceKey(machineKey)
	if err != nil {
		return nil, fmt.Errorf("machine name has wrong format %v", machineKey)
	}
	machine := &mapiv1.Machine{}
	if err = r.client.Get(
		context.TODO(),
		types.NamespacedName{
			Namespace: namespace,
			Name:      machineName,
		},
		machine,
	); err != nil {
		return nil, fmt.Errorf("error getting machine: %v", err)
	}
	return machine, nil
}

func (r *ReconcileMachineHealthCheck) mhcRequestsFromNode(o handler.MapObject) []reconcile.Request {
	glog.V(4).Infof("Getting MHC requests from node %q", namespacedName(o.Meta).String())
	node := &corev1.Node{}
	if err := r.client.Get(context.Background(), namespacedName(o.Meta), node); err != nil {
		glog.Errorf("No-op: Unable to retrieve node %q from store: %v", namespacedName(o.Meta).String(), err)
		return nil
	}
	machine, err := r.getMachineFromNode(*node)
	if machine == nil || err != nil {
		glog.Errorf("No-op: Unable to retrieve machine from node %q: %v", namespacedName(node).String(), err)
		return nil
	}

	mhcList := &healthcheckingv1alpha1.MachineHealthCheckList{}
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

	mhcList := &healthcheckingv1alpha1.MachineHealthCheckList{}
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

func (r *ReconcileMachineHealthCheck) remediate(t target) error {
	glog.Infof(" %s: start remediation logic", t.string())
	if !t.hasMachineSetOwner() {
		glog.Infof("%s: no machineSet controller owner, skipping remediation", t.string())
		return nil
	}

	remediationStrategy := t.MHC.Spec.RemediationStrategy
	if remediationStrategy != nil && *remediationStrategy == remediationStrategyReboot {
		return r.remediationStrategyReboot(&t.Machine, t.Node)
	}
	if t.isMaster() {
		glog.Infof("%s: master node, skipping remediation", t.string())
		return nil
	}

	glog.Infof("%s: deleting", t.string())
	if err := r.client.Delete(context.TODO(), &t.Machine); err != nil {
		return fmt.Errorf("%s: failed to delete machine: %v", t.string(), err)
	}
	return nil
}

func (r *ReconcileMachineHealthCheck) remediationStrategyReboot(machine *mapiv1.Machine, node *corev1.Node) error {
	// we already have reboot annotation on the node, stop reconcile
	if _, ok := node.Annotations[machineRebootAnnotationKey]; ok {
		return nil
	}

	if node.Annotations == nil {
		node.Annotations = map[string]string{}
	}

	glog.Infof("Machine %s has been unhealthy for too long, adding reboot annotation", machine.Name)
	node.Annotations[machineRebootAnnotationKey] = ""
	if err := r.client.Update(context.TODO(), node); err != nil {
		return err
	}
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

func (t *target) isMaster() bool {
	if t.Node != nil {
		if labels.Set(t.Node.Labels).Has(nodeMasterLabel) {
			return true
		}
	}

	// if the node is not found we fallback to check the machine
	if labels.Set(t.Machine.Labels).Get(machineRoleLabel) == machineMasterRole {
		return true
	}

	return false
}

func (t *target) isUnhealthy() (bool, time.Duration, error) {
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

func (t *target) hasMachineSetOwner() bool {
	ownerRefs := t.Machine.ObjectMeta.GetOwnerReferences()
	for _, or := range ownerRefs {
		if or.Kind == ownerControllerKind {
			return true
		}
	}
	return false
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

func hasMatchingLabels(machineHealthCheck *healthcheckingv1alpha1.MachineHealthCheck, machine *mapiv1.Machine) bool {
	selector, err := metav1.LabelSelectorAsSelector(&machineHealthCheck.Spec.Selector)
	if err != nil {
		glog.Warningf("unable to convert selector: %v", err)
		return false
	}
	// If a deployment with a nil or empty selector creeps in, it should match nothing, not everything.
	if selector.Empty() {
		glog.V(2).Infof("%q machineHealthCheck has empty selector", machineHealthCheck.GetName())
		return false
	}
	if !selector.Matches(labels.Set(machine.Labels)) {
		glog.V(4).Infof("%q machine has mismatched labels for MHC %q", machine.GetName(), machineHealthCheck.GetName())
		return false
	}
	return true
}
