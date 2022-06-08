package machine

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/kubectl/pkg/drain"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	machinev1 "github.com/openshift/api/machine/v1beta1"

	"github.com/openshift/machine-api-operator/pkg/util/conditions"
)

// DrainController performs pods eviction for deleting node
type machineDrainController struct {
	client.Client
	config *rest.Config
	scheme *runtime.Scheme

	eventRecorder record.EventRecorder
}

// newDrainController returns a new reconcile.Reconciler for machine-drain-controller
func newDrainController(mgr manager.Manager) reconcile.Reconciler {
	d := &machineDrainController{
		Client:        mgr.GetClient(),
		eventRecorder: mgr.GetEventRecorderFor("machine-drain-controller"),
		config:        mgr.GetConfig(),
		scheme:        mgr.GetScheme(),
	}
	return d
}

func (d *machineDrainController) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	m := &machinev1.Machine{}
	if err := d.Client.Get(ctx, request.NamespacedName, m); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.
			return reconcile.Result{}, nil
		}

		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	existingDrainedCondition := conditions.Get(m, machinev1.MachineDrained)
	alreadyDrained := existingDrainedCondition != nil && existingDrainedCondition.Status == corev1.ConditionTrue

	if !m.ObjectMeta.DeletionTimestamp.IsZero() && stringPointerDeref(m.Status.Phase) == phaseDeleting && !alreadyDrained {
		drainFinishedCondition := conditions.TrueCondition(machinev1.MachineDrained)

		if _, exists := m.ObjectMeta.Annotations[ExcludeNodeDrainingAnnotation]; !exists && m.Status.NodeRef != nil {
			// pre-drain.delete lifecycle hook
			// Return early without error, will requeue if/when the hook owner removes the annotation.
			if len(m.Spec.LifecycleHooks.PreDrain) > 0 {
				klog.Infof("%v: not draining machine: lifecycle blocked by pre-drain hook", m.Name)
				d.eventRecorder.Eventf(m, corev1.EventTypeNormal, "DrainBlocked", "Drain blocked by pre-drain hook")
				return reconcile.Result{}, nil
			}
			d.eventRecorder.Eventf(m, corev1.EventTypeNormal, "DrainProceeds", "Node drain proceeds")
			if err := d.drainNode(ctx, m); err != nil {
				klog.Errorf("%v: failed to drain node for machine: %v", m.Name, err)
				conditions.Set(m, conditions.FalseCondition(
					machinev1.MachineDrained,
					machinev1.MachineDrainError,
					machinev1.ConditionSeverityWarning,
					"could not drain machine: %v", err,
				))
				d.eventRecorder.Eventf(m, corev1.EventTypeNormal, "DrainRequeued", "Node drain requeued: %v", err.Error())
				return delayIfRequeueAfterError(err)
			}
			d.eventRecorder.Eventf(m, corev1.EventTypeNormal, "DrainSucceeded", "Node drain succeeded")
			drainFinishedCondition.Message = "Drain finished successfully"
		} else {
			d.eventRecorder.Eventf(m, corev1.EventTypeNormal, "DrainSkipped", "Node drain skipped")
			drainFinishedCondition.Message = "Node drain skipped"
		}

		conditions.Set(m, drainFinishedCondition)
		// requeue request in case of failed update
		if err := d.Client.Status().Update(ctx, m); err != nil {
			return reconcile.Result{}, fmt.Errorf("could not update machine status: %w", err)
		}
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (d *machineDrainController) drainNode(ctx context.Context, machine *machinev1.Machine) error {
	kubeClient, err := kubernetes.NewForConfig(d.config)
	if err != nil {
		return fmt.Errorf("unable to build kube client: %v", err)
	}
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, machine.Status.NodeRef.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// If an admin deletes the node directly, we'll end up here.
			klog.Infof("Could not find node from noderef, it may have already been deleted: %v", machine.Status.NodeRef.Name)
			return nil
		}
		return fmt.Errorf("unable to get node %q: %v", machine.Status.NodeRef.Name, err)
	}

	drainer := &drain.Helper{
		Ctx:                 ctx,
		Client:              kubeClient,
		Force:               true,
		IgnoreAllDaemonSets: true,
		DeleteEmptyDirData:  true,
		GracePeriodSeconds:  -1,
		// If a pod is not evicted in 20 seconds, retry the eviction next time the
		// machine gets reconciled again (to allow other machines to be reconciled).
		Timeout: 20 * time.Second,
		OnPodDeletedOrEvicted: func(pod *corev1.Pod, usingEviction bool) {
			verbStr := "Deleted"
			if usingEviction {
				verbStr = "Evicted"
			}
			klog.Info(fmt.Sprintf("%s pod from Node", verbStr),
				"pod", fmt.Sprintf("%s/%s", pod.Name, pod.Namespace))
		},
		Out:    writer{klog.Info},
		ErrOut: writer{klog.Error},
	}

	if nodeIsUnreachable(node) {
		klog.Infof("%q: Node %q is unreachable, draining will ignore gracePeriod. PDBs are still honored.",
			machine.Name, node.Name)
		// Since kubelet is unreachable, pods will never disappear and we still
		// need SkipWaitForDeleteTimeoutSeconds so we don't wait for them.
		drainer.SkipWaitForDeleteTimeoutSeconds = skipWaitForDeleteTimeoutSeconds
		drainer.GracePeriodSeconds = 1
	}

	if err := drain.RunCordonOrUncordon(drainer, node, true); err != nil {
		// Can't cordon a node
		klog.Warningf("cordon failed for node %q: %v", node.Name, err)
		return &RequeueAfterError{RequeueAfter: 20 * time.Second}
	}

	if err := drain.RunNodeDrain(drainer, node.Name); err != nil {
		// Machine still tries to terminate after drain failure
		klog.Warningf("drain failed for machine %q: %v", machine.Name, err)
		return &RequeueAfterError{RequeueAfter: 20 * time.Second}
	}

	klog.Infof("drain successful for machine %q", machine.Name)
	d.eventRecorder.Eventf(machine, corev1.EventTypeNormal, "Deleted", "Node %q drained", node.Name)

	return nil
}
