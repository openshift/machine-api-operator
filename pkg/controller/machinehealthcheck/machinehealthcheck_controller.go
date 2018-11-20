package machinehealthcheck

import (
	"context"

	golangerrors "errors"
	"github.com/golang/glog"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"
	capiv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
	"time"
)

const (
	machineAnnotationKey = "machine"
	// TODO(alberto) ensure we handle the case for when a new machine comes up
	// so remediation doesn't kill it before it goes healthy
	remediationWaitTime = 5 * time.Minute
	healthCriteria      = corev1.NodeReady
	ownerControllerKind = "MachineSet"
)

// Add creates a new MachineHealthCheck Controller and adds it to the Manager. The Manager will set fields on the Controller
// and start it when the Manager is started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileMachineHealthCheck{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("machinehealthcheck-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}
	return c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{})
}

var _ reconcile.Reconciler = &ReconcileMachineHealthCheck{}

// ReconcileMachineHealthCheck reconciles a MachineHealthCheck object
type ReconcileMachineHealthCheck struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for MachineHealthCheck, machine and nodes objects and makes changes based on the state read
// and what is in the MachineHealthCheck.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileMachineHealthCheck) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	glog.Infof("Reconciling MachineHealthCheck triggered by %s/%s\n", request.Namespace, request.Name)

	// Get node from request
	node := &corev1.Node{}
	err := r.client.Get(context.TODO(), request.NamespacedName, node)
	glog.V(4).Infof("Reconciling, getting node %v", node.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	machineKey, ok := node.Annotations[machineAnnotationKey]
	if !ok {
		glog.Warningf("No machine annotation for node %s", node.Name)
		return reconcile.Result{}, nil
	}

	glog.Infof("Node %s is annotated with machine %s", node.Name, machineKey)
	machine := &capiv1.Machine{}
	namespace, machineName, err := cache.SplitMetaNamespaceKey(machineKey)
	if err != nil {
		return reconcile.Result{}, err
	}
	key := &types.NamespacedName{
		Namespace: namespace,
		Name:      machineName,
	}
	err = r.client.Get(context.TODO(), *key, machine)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Warningf("machine %s not found", machineKey)
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		glog.Errorf("error getting machine %s. Error: %v. Requeuing...", machineKey, err)
		return reconcile.Result{}, err
	}

	// If the current machine matches any existing MachineHealthCheck CRD
	allMachineHealthChecks := &healthcheckingv1alpha1.MachineHealthCheckList{}
	err = r.client.List(context.Background(), getMachineHealthCheckListOptions(), allMachineHealthChecks)
	if err != nil {
		glog.Errorf("failed to list MachineHealthChecks, %v", err)
		return reconcile.Result{}, err
	}

	for _, hc := range allMachineHealthChecks.Items {
		if hasMatchingLabels(&hc, machine) {
			glog.V(4).Infof("Machine %s has a matching machineHealthCheck: %s", machineKey, hc.Name)
			return remediate(r, machine)
		}
	}

	glog.Infof("Machine %s has no MachineHealthCheck associated", machineName)
	return reconcile.Result{}, nil
}

// This is set so the fake client can be used for unit test. See:
// https://github.com/kubernetes-sigs/controller-runtime/issues/168
func getMachineHealthCheckListOptions() *client.ListOptions {
	return &client.ListOptions{
		Raw: &metav1.ListOptions{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "healthchecking.openshift.io/v1alpha1",
				Kind:       "MachineHealthCheck",
			},
		},
	}
}

func remediate(r *ReconcileMachineHealthCheck, machine *capiv1.Machine) (reconcile.Result, error) {
	glog.Infof("Initialising remediation logic for machine %s", machine.Name)
	if isMaster(*machine, r.client) {
		glog.Info("The machine %s is a master node, skipping remediation", machine.Name)
		return reconcile.Result{}, nil
	}
	if !hasMachineSetOwner(*machine) {
		glog.Info("Machine %s has no machineSet controller owner, skipping remediation", machine.Name)
		return reconcile.Result{}, nil
	}

	node, err := getNodeFromMachine(*machine, r.client)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Warningf("Node %s not found for machine %s", node.Name, machine.Name)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if !isHealthy(node) {
		if unhealthyForTooLong(node) {
			// delete
			glog.Infof("machine %s has been unhealthy for too long, deleting", machine.Name)
			if err := r.client.Delete(context.TODO(), machine); err != nil {
				glog.Errorf("failed to delete machine %s, requeuing referenced node", machine.Name)
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		} else {
			currentTime := time.Now()
			lastTimeHealthy := lastTransitionTime(node, healthCriteria)
			durationUnhealthy := currentTime.Sub(lastTimeHealthy.Time)
			glog.Warningf("machine %s have had node %s unhealthy for %s. Requeuing...", machine.Name, node.Name, durationUnhealthy.String())
			return reconcile.Result{Requeue: true, RequeueAfter: remediationWaitTime}, nil
		}
	}
	glog.Infof("No remediaton action was taken. Machine %s with node %v is healthy", machine.Name, node.Name)
	return reconcile.Result{}, nil
}

func getNodeFromMachine(machine capiv1.Machine, client client.Client) (*corev1.Node, error) {
	if machine.Status.NodeRef == nil {
		glog.Errorf("node NodeRef not found in machine %s", machine.Name)
		return nil, golangerrors.New("node NodeRef not found in machine")
	}
	node := &corev1.Node{}
	nodeKey := types.NamespacedName{
		Namespace: machine.Status.NodeRef.Namespace,
		Name:      machine.Status.NodeRef.Name,
	}
	err := client.Get(context.TODO(), nodeKey, node)
	return node, err
}

func unhealthyForTooLong(node *corev1.Node) bool {
	currentTime := time.Now()
	lastTimeHealthy := lastTransitionTime(node, healthCriteria)
	if lastTimeHealthy.Add(remediationWaitTime).Before(currentTime) {
		return true
	}
	return false
}

func hasMachineSetOwner(machine capiv1.Machine) bool {
	ownerRefs := machine.ObjectMeta.GetOwnerReferences()
	for _, or := range ownerRefs {
		if or.Kind == ownerControllerKind {
			return true
		}
	}
	return false
}

func isHealthy(node *corev1.Node) bool {
	nodeReady := getNodeCondition(node, healthCriteria)
	glog.V(4).Infof("condition %v", nodeReady)
	return nodeReady.Status == corev1.ConditionTrue
}

func lastTransitionTime(node *corev1.Node, conditionType corev1.NodeConditionType) metav1.Time {
	condition := getNodeCondition(node, conditionType)
	currentTime := metav1.Now()
	if condition == nil {
		return metav1.Time{Time: currentTime.Add(-2 * remediationWaitTime)}
	}
	return condition.LastTransitionTime

}

func getNodeCondition(node *corev1.Node, conditionType corev1.NodeConditionType) *corev1.NodeCondition {
	for _, c := range node.Status.Conditions {
		if c.Type == conditionType {
			return &c
		}
	}
	return nil
}

func hasMatchingLabels(machineHealthCheck *healthcheckingv1alpha1.MachineHealthCheck, machine *capiv1.Machine) bool {
	selector, err := metav1.LabelSelectorAsSelector(&machineHealthCheck.Spec.Selector)
	if err != nil {
		glog.Warningf("unable to convert selector: %v", err)
		return false
	}
	// If a deployment with a nil or empty selector creeps in, it should match nothing, not everything.
	if selector.Empty() {
		glog.V(2).Infof("%v machineHealthCheck has empty selector", machineHealthCheck.Name)
		return false
	}
	if !selector.Matches(labels.Set(machine.Labels)) {
		glog.V(4).Infof("%v machine has mismatched labels", machine.Name)
		return false
	}
	return true
}

func isMaster(machine capiv1.Machine, client client.Client) bool {
	machineMasterLabels := []string{
		"sigs.k8s.io/cluster-api-machine-role",
		"sigs.k8s.io/cluster-api-machine-type",
	}
	nodeMasterLabels := []string{
		"node-role.kubernetes.io/master",
	}

	machineLabels := labels.Set(machine.Labels)
	for _, masterLabel := range machineMasterLabels {
		if machineLabels.Get(masterLabel) == "master" {
			return true
		}
	}

	node, err := getNodeFromMachine(machine, client)
	if err != nil {
		glog.Warningf("Couldn't get node for machine %s", machine.Name)
		return false
	}
	nodeLabels := labels.Set(node.Labels)
	for _, masterLabel := range nodeMasterLabels {
		if nodeLabels.Has(masterLabel) {
			return true
		}
	}
	return false
}
