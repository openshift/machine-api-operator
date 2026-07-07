package vsphere

// This is a thin layer to implement the machine actuator interface with cloud provider details.
// The lifetime of scope and reconciler is a machine actuator operation.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"k8s.io/component-base/featuregate"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/events"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	scopeFailFmt        = "%s: failed to create scope for machine: %v"
	reconcilerFailFmt   = "%s: reconciler failed to %s machine: %w"
	createEventAction   = "Create"
	updateEventAction   = "Update"
	deleteEventAction   = "Delete"
	noEventAction       = ""
	requeueAfterSeconds = 20

	// lastReconciledProviderSpecHashAnnotation records a hash of the provider spec that was
	// in effect the last time the machine went through a full reconciliation. It is used,
	// together with lastFullReconcileTimestampAnnotation, to short-circuit reconciliation of
	// stable Running machines and avoid unnecessary vCenter API load.
	lastReconciledProviderSpecHashAnnotation = "machine.openshift.io/last-reconciled-provider-spec-hash"

	// lastFullReconcileTimestampAnnotation records the RFC3339 timestamp of the last full
	// reconciliation. Once it is older than fullReconcileTTL, the short-circuit no longer
	// applies and a full reconciliation is forced, guaranteeing drift detection at least
	// once per TTL window regardless of spec changes.
	lastFullReconcileTimestampAnnotation = "machine.openshift.io/last-full-reconcile-timestamp"

	// fullReconcileTTL bounds how long a machine can go without a full reconciliation.
	fullReconcileTTL = time.Hour
)

// hashProviderSpec returns a short, deterministic hex-encoded SHA-256 hash of the given
// raw provider spec bytes, used to detect provider spec changes cheaply.
func hashProviderSpec(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:16]
}

// canSkipFullReconcile returns true if machine is a stable, Running machine whose provider
// spec has not changed since the last full reconciliation and whose last full reconciliation
// happened within fullReconcileTTL. When true, callers may skip vCenter API calls entirely.
func canSkipFullReconcile(machine *machinev1.Machine) bool {
	if ptr.Deref(machine.Status.Phase, "") != machinev1.PhaseRunning {
		return false
	}
	if ptr.Deref(machine.Spec.ProviderID, "") == "" {
		return false
	}
	if machine.Status.NodeRef == nil {
		return false
	}
	if len(machine.Status.Addresses) == 0 {
		return false
	}
	if !machine.GetDeletionTimestamp().IsZero() {
		return false
	}

	annotations := machine.GetAnnotations()

	raw := providerSpecRawBytes(machine)
	if raw == nil {
		return false
	}

	lastHash, ok := annotations[lastReconciledProviderSpecHashAnnotation]
	if !ok || lastHash != hashProviderSpec(raw) {
		return false
	}

	lastFullReconcile, ok := annotations[lastFullReconcileTimestampAnnotation]
	if !ok {
		return false
	}
	lastFullReconcileTime, err := time.Parse(time.RFC3339, lastFullReconcile)
	if err != nil {
		return false
	}

	return time.Since(lastFullReconcileTime) < fullReconcileTTL
}

// markFullReconcileComplete records the current provider spec hash and timestamp on the
// machine so that subsequent reconciliations can short-circuit via canSkipFullReconcile.
func markFullReconcileComplete(machine *machinev1.Machine) {
	if machine.Annotations == nil {
		machine.Annotations = map[string]string{}
	}
	machine.Annotations[lastReconciledProviderSpecHashAnnotation] = hashProviderSpec(providerSpecRawBytes(machine))
	machine.Annotations[lastFullReconcileTimestampAnnotation] = time.Now().UTC().Format(time.RFC3339)
}

func providerSpecRawBytes(machine *machinev1.Machine) []byte {
	if machine.Spec.ProviderSpec.Value == nil {
		return nil
	}
	return machine.Spec.ProviderSpec.Value.Raw
}

// Actuator is responsible for performing machine reconciliation.
type Actuator struct {
	client                   runtimeclient.Client
	apiReader                runtimeclient.Reader
	eventRecorder            events.EventRecorder
	TaskIDCache              map[string]string
	FeatureGates             featuregate.MutableFeatureGate
	openshiftConfigNamespace string
}

// ActuatorParams holds parameter information for Actuator.
type ActuatorParams struct {
	Client                   runtimeclient.Client
	APIReader                runtimeclient.Reader
	EventRecorder            events.EventRecorder
	TaskIDCache              map[string]string
	FeatureGates             featuregate.MutableFeatureGate
	OpenshiftConfigNamespace string
}

// NewActuator returns an actuator.
func NewActuator(params ActuatorParams) *Actuator {
	return &Actuator{
		client:                   params.Client,
		apiReader:                params.APIReader,
		eventRecorder:            params.EventRecorder,
		TaskIDCache:              params.TaskIDCache,
		FeatureGates:             params.FeatureGates,
		openshiftConfigNamespace: params.OpenshiftConfigNamespace,
	}
}

// Set corresponding event based on error. It also returns the original error
// for convenience, so callers can do "return handleMachineError(...)".
func (a *Actuator) handleMachineError(machine *machinev1.Machine, err error, eventAction string) error {
	klog.Errorf("%q error: %v", machine.GetName(), err)
	if eventAction != noEventAction {
		a.eventRecorder.Eventf(machine, nil, corev1.EventTypeWarning, "Failed"+eventAction, eventAction, "%v", err)
	}
	return err
}

// Create creates a machine and is invoked by the machine controller.
func (a *Actuator) Create(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: actuator creating machine", machine.GetName())

	scope, err := newMachineScope(machineScopeParams{
		Context:                  ctx,
		client:                   a.client,
		machine:                  machine,
		apiReader:                a.apiReader,
		featureGates:             a.FeatureGates,
		openshiftConfigNameSpace: a.openshiftConfigNamespace,
	})
	if err != nil {
		fmtErr := fmt.Errorf(scopeFailFmt, machine.GetName(), err)
		return a.handleMachineError(machine, fmtErr, createEventAction)
	}

	// Ensure we're not reconciling a stale machine by checking our task-id.
	// This is a workaround for a cache race condition.
	if val, ok := a.TaskIDCache[machine.Name]; ok {
		if val != scope.providerStatus.TaskRef {
			klog.Errorf("%s: machine object missing expected provider task ID, requeue", machine.GetName())
			return &machinecontroller.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
		}
	}

	var retErr error
	err = newReconciler(scope).create()
	// save the taskRef in our cache in case of any error with patch.
	if scope.providerStatus.TaskRef != "" {
		a.TaskIDCache[machine.Name] = scope.providerStatus.TaskRef
	}
	if err != nil {
		fmtErr := fmt.Errorf(reconcilerFailFmt, machine.GetName(), createEventAction, err)
		retErr = a.handleMachineError(machine, fmtErr, createEventAction)
	} else {
		a.eventRecorder.Eventf(machine, nil, corev1.EventTypeNormal, createEventAction, createEventAction, "Created Machine %v", machine.GetName())
	}

	if err := scope.PatchMachine(); err != nil {
		return err
	}

	return retErr
}

func (a *Actuator) Exists(ctx context.Context, machine *machinev1.Machine) (bool, error) {
	if canSkipFullReconcile(machine) {
		klog.V(3).Infof("%s: machine is stable, skipping full reconcile in Exists()", machine.GetName())
		return true, nil
	}

	klog.Infof("%s: actuator checking if machine exists", machine.GetName())
	scope, err := newMachineScope(machineScopeParams{
		Context:                  ctx,
		client:                   a.client,
		machine:                  machine,
		apiReader:                a.apiReader,
		featureGates:             a.FeatureGates,
		openshiftConfigNameSpace: a.openshiftConfigNamespace,
	})
	if err != nil {
		return false, fmt.Errorf(scopeFailFmt, machine.GetName(), err)
	}
	return newReconciler(scope).exists()
}

func (a *Actuator) Update(ctx context.Context, machine *machinev1.Machine) error {
	if canSkipFullReconcile(machine) {
		klog.V(3).Infof("%s: machine is stable, skipping full reconcile in Update()", machine.GetName())
		return nil
	}

	klog.Infof("%s: actuator updating machine", machine.GetName())
	// Cleanup TaskIDCache so we don't continually grow
	delete(a.TaskIDCache, machine.Name)

	scope, err := newMachineScope(machineScopeParams{
		Context:                  ctx,
		client:                   a.client,
		machine:                  machine,
		apiReader:                a.apiReader,
		featureGates:             a.FeatureGates,
		openshiftConfigNameSpace: a.openshiftConfigNamespace,
	})
	if err != nil {
		fmtErr := fmt.Errorf(scopeFailFmt, machine.GetName(), err)
		return a.handleMachineError(machine, fmtErr, updateEventAction)
	}
	if err := newReconciler(scope).update(); err != nil {
		// Update machine and machine status in case it was modified
		if err := scope.PatchMachine(); err != nil {
			return err
		}
		fmtErr := fmt.Errorf(reconcilerFailFmt, machine.GetName(), updateEventAction, err)
		return a.handleMachineError(machine, fmtErr, updateEventAction)
	}

	// A full reconciliation just completed successfully: record the provider spec hash and
	// timestamp so that future stable reconciliations can short-circuit via
	// canSkipFullReconcile, avoiding unnecessary vCenter API calls.
	markFullReconcileComplete(scope.machine)

	previousResourceVersion := scope.machine.ResourceVersion

	if err := scope.PatchMachine(); err != nil {
		return err
	}

	currentResourceVersion := scope.machine.ResourceVersion

	// Create event only if machine object was modified
	if previousResourceVersion != currentResourceVersion {
		a.eventRecorder.Eventf(machine, nil, corev1.EventTypeNormal, updateEventAction, updateEventAction, "Updated Machine %v", machine.GetName())
	}

	return nil
}

func (a *Actuator) Delete(ctx context.Context, machine *machinev1.Machine) error {
	klog.Infof("%s: actuator deleting machine", machine.GetName())
	// Cleanup TaskIDCache so we don't continually grow
	// Cleanup here as well in case Update() was never successfully called.
	delete(a.TaskIDCache, machine.Name)

	scope, err := newMachineScope(machineScopeParams{
		Context:                  ctx,
		client:                   a.client,
		machine:                  machine,
		apiReader:                a.apiReader,
		featureGates:             a.FeatureGates,
		openshiftConfigNameSpace: a.openshiftConfigNamespace,
	})
	if err != nil {
		fmtErr := fmt.Errorf(scopeFailFmt, machine.GetName(), err)
		return a.handleMachineError(machine, fmtErr, deleteEventAction)
	}
	if err := newReconciler(scope).delete(); err != nil {
		if err := scope.PatchMachine(); err != nil {
			return err
		}
		fmtErr := fmt.Errorf(reconcilerFailFmt, machine.GetName(), deleteEventAction, err)
		return a.handleMachineError(machine, fmtErr, deleteEventAction)
	}
	a.eventRecorder.Eventf(machine, nil, corev1.EventTypeNormal, deleteEventAction, deleteEventAction, "Deleted machine %v", machine.GetName())
	return scope.PatchMachine()
}
