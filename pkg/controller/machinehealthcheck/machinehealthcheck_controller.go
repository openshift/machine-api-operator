package machinehealthcheck

import (
	"context"

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
)

const (
	machineAnnotationKey = "machine"
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
		glog.Infof("No machine annotation for node %s", node.Name)
		return reconcile.Result{}, nil
	}

	glog.Infof("Node %s is annotated for machine %s", node.Name, machineKey)
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
			glog.Warning("machine %s not found", machineKey)
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		glog.Errorf("error getting machine %s, requeuing", machineKey)
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
			remediate(node)
		}
	}

	return reconcile.Result{}, nil
}

// This set so the fake client can be used for unit test. See:
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

func remediate(node *corev1.Node) {
	// TODO(alberto): implement Remediate logic via hash or CRD
	if !isHealthy(node) {
	}
	return
}

func isHealthy(node *corev1.Node) bool {
	nodeReady := getNodeCondition(node, corev1.NodeReady)
	return nodeReady.Status == corev1.ConditionTrue
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
