package machinehealthcheck

import (
	"context"
	"reflect"
	"time"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	machinesutil "github.com/openshift/machine-api-operator/pkg/util/machines"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	machineAnnotationKey = "machine.openshift.io/machine"
	ownerControllerKind  = "MachineSet"
)

// Add creates a new MachineHealthCheck Controller and adds it to the Manager. The Manager will set fields on the Controller
// and start it when the Manager is started.
func Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr)
	if err != nil {
		return err
	}
	return add(mgr, r)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) (reconcile.Reconciler, error) {
	r := &ReconcileMachineHealthCheck{
		client: mgr.GetClient(),
		scheme: mgr.GetScheme(),
	}

	ns, err := util.GetNamespace(util.ServiceAccountNamespaceFile)
	if err != nil {
		return r, err
	}

	r.namespace = ns
	return r, nil
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
	client    client.Client
	scheme    *runtime.Scheme
	namespace string
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
	machine := &mapiv1.Machine{}
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
	err = r.client.List(context.Background(), allMachineHealthChecks)
	if err != nil {
		glog.Errorf("failed to list MachineHealthChecks, %v", err)
		return reconcile.Result{}, err
	}

	for _, hc := range allMachineHealthChecks.Items {
		if hasMatchingLabels(&hc, machine) {
			unhealhtyConditions, err := conditions.GetConditionsFromConfigMap(r.client, r.namespace)
			if err != nil {
				return reconcile.Result{}, err
			}

			glog.V(4).Infof("Machine %s has a matching machineHealthCheck: %s", machineKey, hc.Name)
			result, err := remediate(r, machine, unhealhtyConditions)
			if err != nil {
				return reconcile.Result{}, err
			}
			// update MHC status
			err = updateMHCStatus(r.client, &hc, unhealhtyConditions)
			if err != nil {
				return reconcile.Result{}, err
			}
			return result, nil
		}
	}

	glog.Infof("Machine %s has no MachineHealthCheck associated", machineName)
	return reconcile.Result{}, nil
}

func updateMHCStatus(c client.Client, mhc *healthcheckingv1alpha1.MachineHealthCheck, unhealhtyConditions []conditions.UnhealthyCondition) error {
	machines, err := machinesutil.GetMahcinesByLabelSelector(c, &mhc.Spec.Selector, mhc.Namespace)
	if err != nil {
		return err
	}

	var totalHealthy int32
	targetedMachines := []healthcheckingv1alpha1.TargetedMachine{}
	for _, m := range machines.Items {
		machineUnhealthyConditions, err := conditions.GetMachineUnhealthyConditions(c, &m, unhealhtyConditions)
		if err != nil {
			return err
		}

		conditionsTypes := []corev1.NodeConditionType{}
		for _, c := range machineUnhealthyConditions {
			conditionsTypes = append(conditionsTypes, c.Name)
		}

		healthy := healthcheckingv1alpha1.MachineHealthyFalse
		if len(machineUnhealthyConditions) == 0 {
			healthy = healthcheckingv1alpha1.MachineHealthyTrue
			totalHealthy++
		}

		targetedMachines = append(targetedMachines, healthcheckingv1alpha1.TargetedMachine{
			Name:                m.Name,
			Healthy:             healthy,
			UnhealthyConditions: conditionsTypes,
		})
	}

	targetedConditions := []healthcheckingv1alpha1.TargetedCondition{}
	for _, c := range unhealhtyConditions {
		targetedConditions = append(targetedConditions, healthcheckingv1alpha1.TargetedCondition{
			Name:   c.Name,
			Status: c.Status,
		})
	}

	if reflect.DeepEqual(targetedConditions, mhc.Status.TargetedConditions) &&
		reflect.DeepEqual(targetedMachines, mhc.Status.TotalHealthyMachines) &&
		totalHealthy == mhc.Status.TotalHealthyMachines {
		return nil
	}

	newMhc := mhc.DeepCopy()
	newMhc.Status = healthcheckingv1alpha1.MachineHealthCheckStatus{
		TargetedConditions:   targetedConditions,
		TargetedMachines:     targetedMachines,
		TotalHealthyMachines: totalHealthy,
	}

	return c.Update(context.TODO(), newMhc)
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

func remediate(r *ReconcileMachineHealthCheck, machine *mapiv1.Machine, unhealhtyConditions []conditions.UnhealthyCondition) (reconcile.Result, error) {
	glog.Infof("Initialising remediation logic for machine %s", machine.Name)
	if machinesutil.IsMaster(r.client, machine) {
		glog.Infof("The machine %s is a master node, skipping remediation", machine.Name)
		return reconcile.Result{}, nil
	}
	if !hasMachineSetOwner(*machine) {
		glog.Infof("Machine %s has no machineSet controller owner, skipping remediation", machine.Name)
		return reconcile.Result{}, nil
	}

	node, err := machinesutil.GetNodeByMachine(r.client, machine)
	if err != nil {
		if errors.IsNotFound(err) {
			glog.Warningf("Node %s not found for machine %s", node.Name, machine.Name)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	nodeUnhealthyConditions := conditions.GetNodeUnhealthyConditions(node, unhealhtyConditions)

	var result *reconcile.Result
	var minimalConditionTimeout time.Duration
	minimalConditionTimeout = 0
	for _, c := range nodeUnhealthyConditions {
		nodeCondition := conditions.GetNodeCondition(node, c.Name)
		// skip when current node condition is different from the one reported in the config map
		if nodeCondition == nil || !conditions.IsConditionsStatusesEqual(nodeCondition, &c) {
			continue
		}

		conditionTimeout, err := time.ParseDuration(c.Timeout)
		if err != nil {
			return reconcile.Result{}, err
		}

		// apply remediation logic, if at least one condition last more than specified timeout
		if unhealthyForTooLong(nodeCondition, conditionTimeout) {
			glog.Infof("machine %s has been unhealthy for too long, deleting", machine.Name)
			if err := r.client.Delete(context.TODO(), machine); err != nil {
				glog.Errorf("failed to delete machine %s, requeuing referenced node", machine.Name)
				return reconcile.Result{}, err
			}
			return reconcile.Result{}, nil
		}

		now := time.Now()
		durationUnhealthy := now.Sub(nodeCondition.LastTransitionTime.Time)
		glog.Warningf(
			"Machine %s has unhealthy node %s with the condition %s and the timeout %s for %s. Requeuing...",
			machine.Name,
			node.Name,
			nodeCondition.Type,
			c.Timeout,
			durationUnhealthy.String(),
		)

		// calculate the duration until the node will be unhealthy for too long
		// and re-queue after with this timeout, add one second just to be sure
		// that we will not enter this loop again before the node unhealthy for too long
		unhealthyTooLongTimeout := conditionTimeout - durationUnhealthy + time.Second
		// be sure that we will use timeout with the minimal value for the reconcile.Result
		if minimalConditionTimeout == 0 || minimalConditionTimeout > unhealthyTooLongTimeout {
			minimalConditionTimeout = unhealthyTooLongTimeout
		}
		result = &reconcile.Result{Requeue: true, RequeueAfter: minimalConditionTimeout}
	}

	// requeue
	if result != nil {
		return *result, nil
	}

	glog.Infof("No remediaton action was taken. Machine %s with node %v is healthy", machine.Name, node.Name)
	return reconcile.Result{}, nil
}

func unhealthyForTooLong(nodeCondition *corev1.NodeCondition, timeout time.Duration) bool {
	now := time.Now()
	if nodeCondition.LastTransitionTime.Add(timeout).Before(now) {
		return true
	}
	return false
}

func hasMachineSetOwner(machine mapiv1.Machine) bool {
	ownerRefs := machine.ObjectMeta.GetOwnerReferences()
	for _, or := range ownerRefs {
		if or.Kind == ownerControllerKind {
			return true
		}
	}
	return false
}

func hasMatchingLabels(machineHealthCheck *healthcheckingv1alpha1.MachineHealthCheck, machine *mapiv1.Machine) bool {
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
