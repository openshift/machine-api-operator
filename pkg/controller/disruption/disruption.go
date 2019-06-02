package disruption

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/golang/glog"
	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util"
	machineutil "github.com/openshift/machine-api-operator/pkg/util/machines"

	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// DeletionTimeout sets maximum time from the moment a machine is added to DisruptedMachines in MDB.Status
// to the time when the machine is expected to be seen by MDB controller as having been marked for deletion.
// If the machine was not marked for deletion during that time it is assumed that it won't be deleted at
// all and the corresponding entry can be removed from mdb.Status.DisruptedMachines. It is assumed that
// machine/mdb apiserver to controller latency is relatively small (like 1-2sec) so the below value should
// be more than enough.
// If the controller is running on a different node it is important that the two nodes have synced
// clock (via ntp for example). Otherwise MachineDisruptionBudget controller may not provide enough
// protection against unwanted pod disruptions.
const DeletionTimeout = 2 * time.Minute

// Add creates a new MachineDisruption Controller and adds it to the Manager. The Manager will set fields on the Controller
// and start it when the Manager is started.
func Add(mgr manager.Manager) error {
	r, err := newReconciler(mgr)
	if err != nil {
		return err
	}
	return add(mgr, r, r.machineToMachineDisruptionBudget)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) (*ReconcileMachineDisruption, error) {
	r := &ReconcileMachineDisruption{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		recorder: mgr.GetEventRecorderFor("machine-disruption-controller"),
	}

	ns, err := util.GetNamespace(util.ServiceAccountNamespaceFile)
	if err != nil {
		return r, err
	}

	r.namespace = ns
	return r, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, mapFn handler.ToRequestsFunc) error {
	// Create a new controller
	c, err := controller.New("MachineDisruption-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	if err = c.Watch(&source.Kind{Type: &mapiv1.Machine{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapFn}); err != nil {
		return err
	}

	return c.Watch(&source.Kind{Type: &healthcheckingv1alpha1.MachineDisruptionBudget{}}, &handler.EnqueueRequestForObject{})
}

var _ reconcile.Reconciler = &ReconcileMachineDisruption{}

// ReconcileMachineDisruption reconciles a MachineDisruption object
type ReconcileMachineDisruption struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client    client.Client
	recorder  record.EventRecorder
	scheme    *runtime.Scheme
	namespace string
}

// Reconcile reads that state of the cluster for MachineDisruptionBudget and machine objects and makes changes based on labels under
// MachineDisruptionBudget or machine objects
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileMachineDisruption) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	glog.Infof("Reconciling MachineDisruption triggered by %s/%s\n", request.Namespace, request.Name)

	// Get machine from request
	mdb := &healthcheckingv1alpha1.MachineDisruptionBudget{}
	err := r.client.Get(context.TODO(), request.NamespacedName, mdb)
	glog.V(4).Infof("Reconciling, getting MachineDisruptionBudget %v", mdb)
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
	result, err := r.reconcile(mdb)
	if err != nil {
		glog.Errorf("Failed to reconcile mdb %s/%s: %v", mdb.Namespace, mdb.Name, err)
		err = r.failSafe(mdb)
	}
	return result, err
}

func (r *ReconcileMachineDisruption) reconcile(mdb *healthcheckingv1alpha1.MachineDisruptionBudget) (reconcile.Result, error) {
	machines, err := r.getMachinesForMachineDisruptionBudget(mdb)
	if err != nil {
		r.recorder.Eventf(mdb, v1.EventTypeWarning, "NoMachines", "Failed to get machines: %v", err)
		return reconcile.Result{}, err
	}

	if len(machines) == 0 {
		r.recorder.Eventf(mdb, v1.EventTypeNormal, "NoMachines", "No matching machines found")
	}

	expectedCount, desiredHealthy := r.getExpectedMachineCount(mdb, machines)

	currentTime := time.Now()
	disruptedMachines, recheckTime := r.buildDisruptedMachineMap(machines, mdb, currentTime)
	currentHealthy := r.countHealthyMachines(machines, disruptedMachines, currentTime)
	err = r.updateMachineDisruptionBudgetStatus(mdb, currentHealthy, desiredHealthy, expectedCount, disruptedMachines)
	if err != nil {
		return reconcile.Result{}, err
	}

	if recheckTime != nil {
		return reconcile.Result{Requeue: true, RequeueAfter: recheckTime.Sub(currentTime)}, nil
	}

	return reconcile.Result{}, nil
}

func (r *ReconcileMachineDisruption) getExpectedMachineCount(mdb *healthcheckingv1alpha1.MachineDisruptionBudget, machines []mapiv1.Machine) (expectedCount, desiredHealthy int32) {
	if mdb.Spec.MaxUnavailable != nil {
		expectedCount = r.getExpectedScale(mdb, machines)
		desiredHealthy = expectedCount - int32(*mdb.Spec.MaxUnavailable)
		if desiredHealthy < 0 {
			desiredHealthy = 0
		}
	} else if mdb.Spec.MinAvailable != nil {
		desiredHealthy = *mdb.Spec.MinAvailable
		expectedCount = int32(len(machines))
	}
	return
}

func (r *ReconcileMachineDisruption) getExpectedScale(mdb *healthcheckingv1alpha1.MachineDisruptionBudget, machines []mapiv1.Machine) int32 {
	// When the user specifies a fraction of machines that must be available, we
	// use as the fraction's denominator
	// SUM_{all c in C} scale(c)
	// where C is the union of C_m1, C_m2, ..., C_mN
	// and each C_mi is the set of controllers controlling the machine mi

	// A mapping from controllers to their scale.
	controllerScale := map[types.UID]int32{}

	// 1. Find the controller for each machine. If any machine has 0 controllers,
	// it will add map item with machine.UID as a key and 1 as a value.
	// With ControllerRef, a machine can only have 1 controller.
	for _, machine := range machines {
		foundController := false
		for _, finder := range r.finders() {
			controllerNScale := finder(&machine)
			if controllerNScale != nil {
				if _, ok := controllerScale[controllerNScale.UID]; !ok {
					controllerScale[controllerNScale.UID] = controllerNScale.scale
				}
				foundController = true
				break
			}
		}
		if !foundController {
			controllerScale[machine.UID] = 1
		}
	}

	// 2. Sum up all relevant machine scales to get the expected number
	var expectedCount int32
	for _, count := range controllerScale {
		expectedCount += count
	}
	return expectedCount
}

type controllerAndScale struct {
	types.UID
	scale int32
}

// machineControllerFinder is a function type that maps a machine to a list of
// controllers and their scale.
type machineControllerFinder func(*mapiv1.Machine) *controllerAndScale

var (
	controllerKindMachineSet        = mapiv1.SchemeGroupVersion.WithKind("MachineSet")
	controllerKindMachineDeployment = mapiv1.SchemeGroupVersion.WithKind("MachineDeployment")
)

func (r *ReconcileMachineDisruption) finders() []machineControllerFinder {
	return []machineControllerFinder{r.getMachineSetFinder, r.getMachineDeploymentFinder}
}

func (r *ReconcileMachineDisruption) getMachineMachineSet(machine *mapiv1.Machine) *mapiv1.MachineSet {
	controllerRef := metav1.GetControllerOf(machine)
	if controllerRef == nil {
		glog.Infof("machine %s does not have owner reference", machine.Name)
		return nil
	}
	if controllerRef.Kind != controllerKindMachineSet.Kind {
		// Skip MachineSet if the machine controlled by different controller
		return nil
	}

	machineSet := &mapiv1.MachineSet{}
	key := client.ObjectKey{Namespace: machine.Namespace, Name: controllerRef.Name}
	err := r.client.Get(context.TODO(), key, machineSet)
	if err != nil {
		glog.Infof("failed to get machine set object for machine %s", machine.Name)
		return nil
	}

	if machineSet.UID != controllerRef.UID {
		glog.Infof("machine %s owner reference UID is different from machines set %s UID", machine.Name, machineSet.Name)
		return nil
	}

	return machineSet
}

func (r *ReconcileMachineDisruption) getMachineSetFinder(machine *mapiv1.Machine) *controllerAndScale {
	machineSet := r.getMachineMachineSet(machine)
	if machineSet == nil {
		return nil
	}

	controllerRef := metav1.GetControllerOf(machineSet)
	if controllerRef != nil && controllerRef.Kind == controllerKindMachineDeployment.Kind {
		// Skip MachineSet if it's controlled by a Deployment.
		return nil
	}
	return &controllerAndScale{machineSet.UID, *(machineSet.Spec.Replicas)}
}

func (r *ReconcileMachineDisruption) getMachineDeploymentFinder(machine *mapiv1.Machine) *controllerAndScale {
	machineSet := r.getMachineMachineSet(machine)
	if machineSet == nil {
		return nil
	}

	controllerRef := metav1.GetControllerOf(machineSet)
	if controllerRef == nil {
		return nil
	}
	if controllerRef.Kind != controllerKindMachineDeployment.Kind {
		return nil
	}
	machineDeployment := &mapiv1.MachineDeployment{}
	key := client.ObjectKey{Namespace: machine.Namespace, Name: controllerRef.Name}
	err := r.client.Get(context.TODO(), key, machineDeployment)
	if err != nil {
		// The only possible error is NotFound, which is ok here.
		return nil
	}
	if machineDeployment.UID != controllerRef.UID {
		return nil
	}
	return &controllerAndScale{machineDeployment.UID, *(machineDeployment.Spec.Replicas)}
}

func (r *ReconcileMachineDisruption) countHealthyMachines(machines []mapiv1.Machine, disruptedMachines map[string]metav1.Time, currentTime time.Time) (currentHealthy int32) {
	for _, machine := range machines {
		// Machine is being deleted.
		if machine.DeletionTimestamp != nil {
			continue
		}
		// Machine is expected to be deleted soon.
		if disruptionTime, found := disruptedMachines[machine.Name]; found && disruptionTime.Time.Add(DeletionTimeout).After(currentTime) {
			continue
		}
		if machineutil.IsMachineHealthy(r.client, &machine) {
			currentHealthy++
		}
	}
	return
}

func (r *ReconcileMachineDisruption) updateMachineDisruptionBudgetStatus(
	mdb *healthcheckingv1alpha1.MachineDisruptionBudget,
	currentHealthy,
	desiredHealthy,
	expectedCount int32,
	disruptedMachines map[string]metav1.Time) error {

	// We require expectedCount to be > 0 so that MDBs which currently match no
	// machines are in a safe state when their first machines appear but this controller
	// has not updated their status yet.  This isn't the only race, but it's a
	// common one that's easy to detect.
	disruptionsAllowed := currentHealthy - desiredHealthy
	if expectedCount <= 0 || disruptionsAllowed <= 0 {
		disruptionsAllowed = 0
	}

	if mdb.Status.CurrentHealthy == currentHealthy &&
		mdb.Status.DesiredHealthy == desiredHealthy &&
		mdb.Status.ExpectedMachines == expectedCount &&
		mdb.Status.MachineDisruptionsAllowed == disruptionsAllowed &&
		reflect.DeepEqual(mdb.Status.DisruptedMachines, disruptedMachines) &&
		mdb.Status.ObservedGeneration == mdb.Generation {
		return nil
	}

	newMdb := mdb.DeepCopy()
	newMdb.Status = healthcheckingv1alpha1.MachineDisruptionBudgetStatus{
		CurrentHealthy:            currentHealthy,
		DesiredHealthy:            desiredHealthy,
		ExpectedMachines:          expectedCount,
		MachineDisruptionsAllowed: disruptionsAllowed,
		DisruptedMachines:         disruptedMachines,
		ObservedGeneration:        mdb.Generation,
	}

	return r.client.Update(context.TODO(), newMdb)
}

// failSafe is an attempt to at least update the MachineDisruptionsAllowed field to
// 0 if everything else has failed.  This is one place we
// implement the  "fail open" part of the design since if we manage to update
// this field correctly, we will prevent the deletion when it may be unsafe to do
func (r *ReconcileMachineDisruption) failSafe(mdb *healthcheckingv1alpha1.MachineDisruptionBudget) error {
	newMdb := mdb.DeepCopy()
	mdb.Status.MachineDisruptionsAllowed = 0
	return r.client.Update(context.TODO(), newMdb)
}

func (r *ReconcileMachineDisruption) getMachineDisruptionBudgetForMachine(machine *mapiv1.Machine) *healthcheckingv1alpha1.MachineDisruptionBudget {
	// GetMachineMachineDisruptionBudgets returns an error only if no
	// MachineDisruptionBudgets are found.  We don't return that as an error to the
	// caller.
	mdbs, err := machineutil.GetMachineMachineDisruptionBudgets(r.client, machine)
	if err != nil {
		glog.V(4).Infof("No MachineDisruptionBudgets found for machine %v, MachineDisruptionBudget controller will avoid syncing.", machine.Name)
		return nil
	}

	if len(mdbs) == 0 {
		glog.V(4).Infof("Could not find MachineDisruptionBudget for machine %s in namespace %s with labels: %v", machine.Name, machine.Namespace, machine.Labels)
		return nil
	}

	if len(mdbs) > 1 {
		msg := fmt.Sprintf("Machine %q/%q matches multiple MachineDisruptionBudgets.  Chose %q arbitrarily.", machine.Namespace, machine.Name, mdbs[0].Name)
		glog.Warning(msg)
		r.recorder.Event(machine, v1.EventTypeWarning, "MultipleMachineDisruptionBudgets", msg)
	}
	return mdbs[0]
}

// This function returns machines using the MachineDisruptionBudget object.
func (r *ReconcileMachineDisruption) getMachinesForMachineDisruptionBudget(mdb *healthcheckingv1alpha1.MachineDisruptionBudget) ([]mapiv1.Machine, error) {
	sel, err := metav1.LabelSelectorAsSelector(mdb.Spec.Selector)
	if err != nil {
		return nil, err
	}
	if sel.Empty() {
		return nil, nil
	}

	machines := &mapiv1.MachineList{}
	listOptions := &client.ListOptions{
		Namespace:     mdb.Namespace,
		LabelSelector: sel,
	}
	err = r.client.List(context.TODO(), machines, client.UseListOptions(listOptions))
	if err != nil {
		return nil, err
	}
	return machines.Items, nil
}

// Builds new MachineDisruption map, possibly removing items that refer to non-existing, already deleted
// or not-deleted at all items. Also returns an information when this check should be repeated.
func (r *ReconcileMachineDisruption) buildDisruptedMachineMap(machines []mapiv1.Machine, mdb *healthcheckingv1alpha1.MachineDisruptionBudget, currentTime time.Time) (map[string]metav1.Time, *time.Time) {
	disruptedMachines := mdb.Status.DisruptedMachines
	result := make(map[string]metav1.Time)
	var recheckTime *time.Time

	if disruptedMachines == nil || len(disruptedMachines) == 0 {
		return result, recheckTime
	}
	for _, machine := range machines {
		if machine.DeletionTimestamp != nil {
			// Already being deleted.
			continue
		}
		disruptionTime, found := disruptedMachines[machine.Name]
		if !found {
			// Machine not on the list.
			continue
		}
		expectedDeletion := disruptionTime.Time.Add(DeletionTimeout)
		if expectedDeletion.Before(currentTime) {
			glog.V(1).Infof("Machine %s/%s was expected to be deleted at %s but it wasn't, updating mdb %s/%s",
				machine.Namespace, machine.Name, disruptionTime.String(), mdb.Namespace, mdb.Name)
			r.recorder.Eventf(&machine, v1.EventTypeWarning, "NotDeleted", "Machine was expected by MDB %s/%s to be deleted but it wasn't",
				mdb.Namespace, mdb.Namespace)
		} else {
			if recheckTime == nil || expectedDeletion.Before(*recheckTime) {
				recheckTime = &expectedDeletion
			}
			result[machine.Name] = disruptionTime
		}
	}
	return result, recheckTime
}

func (r *ReconcileMachineDisruption) machineToMachineDisruptionBudget(o handler.MapObject) []reconcile.Request {
	machine := &mapiv1.Machine{}
	key := client.ObjectKey{Namespace: o.Meta.GetNamespace(), Name: o.Meta.GetName()}
	if err := r.client.Get(context.TODO(), key, machine); err != nil {
		glog.Errorf("Unable to retrieve Machine %v from store: %v", key, err)
	} else {
		glog.Infof("Probably machine %s was deleted, uses a dummy machine to get MDB object", o.Meta.GetName())
		machine.Name = o.Meta.GetName()
		machine.Namespace = o.Meta.GetNamespace()
		machine.Labels = o.Meta.GetLabels()
	}

	mdb := r.getMachineDisruptionBudgetForMachine(machine)
	if mdb == nil {
		glog.Errorf("Unable to find MachineDisruptionBudget for machine %s", machine.Name)
		return nil
	}

	name := client.ObjectKey{Namespace: mdb.Namespace, Name: mdb.Name}
	return []reconcile.Request{{NamespacedName: name}}
}
