package infrastructurevalidation

import (
	"context"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/library-go/pkg/config/clusteroperator/v1helpers"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	controllerName                   = "infrastructurevalidation-controller"
	clusterOperatorName              = "machine-api"
	infrastructureName               = "cluster"
	conditionType                    = "InfrastructureFailureDomainsValid"
	reasonAllReferencesValid         = "AllReferencesValid"
	reasonOrphanedReferences         = "OrphanedMachineReferences"
	degradedReasonOrphanedReferences = "OrphanedFailureDomainReferences"
)

// Ensure ReconcileInfrastructureValidation implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileInfrastructureValidation{}

// ReconcileInfrastructureValidation validates Infrastructure failure domain configurations
type ReconcileInfrastructureValidation struct {
	client    client.Client
	apiReader client.Reader // Bypasses cache for cluster-scoped resources
	recorder  record.EventRecorder
}

// Add creates a new Infrastructure validation controller and adds it to the Manager.
func Add(mgr manager.Manager, opts manager.Options) error {
	reconciler := newReconciler(mgr)
	return add(mgr, reconciler)
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) *ReconcileInfrastructureValidation {
	return &ReconcileInfrastructureValidation{
		client:    mgr.GetClient(),
		apiReader: mgr.GetAPIReader(), // Use APIReader to bypass cache for Infrastructure/ClusterOperator
		recorder:  mgr.GetEventRecorderFor(controllerName),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r *ReconcileInfrastructureValidation) error {
	// Create a new controller
	c, err := controller.New(controllerName, mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch Infrastructure/cluster
	err = c.Watch(source.Kind(mgr.GetCache(), &configv1.Infrastructure{},
		&handler.TypedEnqueueRequestForObject[*configv1.Infrastructure]{}))
	if err != nil {
		return fmt.Errorf("failed to watch Infrastructure: %w", err)
	}

	// Watch Machines and trigger Infrastructure reconcile when they change
	err = c.Watch(source.Kind(mgr.GetCache(), &machinev1.Machine{},
		handler.TypedEnqueueRequestsFromMapFunc[*machinev1.Machine](infrastructureRequestFromMachine)))
	if err != nil {
		return fmt.Errorf("failed to watch Machines: %w", err)
	}

	klog.Infof("Infrastructure validation controller started")
	return nil
}

// infrastructureRequestFromMachine maps any Machine event to a reconcile request
// for the Infrastructure/cluster singleton.
func infrastructureRequestFromMachine(ctx context.Context, machine *machinev1.Machine) []reconcile.Request {
	// Always reconcile the cluster Infrastructure singleton when any machine changes
	return []reconcile.Request{
		{
			NamespacedName: client.ObjectKey{Name: infrastructureName},
		},
	}
}

// Reconcile validates that failure domains referenced by machines are configured
// in the Infrastructure object.
func (r *ReconcileInfrastructureValidation) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling Infrastructure validation for %v", request.NamespacedName)

	// Fetch Infrastructure using APIReader to bypass cache
	// This ensures we always get the latest state, even with namespace-scoped cache
	infra := &configv1.Infrastructure{}
	err := r.apiReader.Get(ctx, request.NamespacedName, infra)
	if err != nil {
		if errors.IsNotFound(err) {
			// Infrastructure doesn't exist, nothing to validate
			klog.V(3).Infof("Infrastructure %s not found, skipping validation", request.NamespacedName)
			return reconcile.Result{}, nil
		}
		klog.Errorf("Failed to get Infrastructure: %v", err)
		return reconcile.Result{}, fmt.Errorf("failed to get Infrastructure: %w", err)
	}

	klog.V(4).Infof("Processing Infrastructure generation=%d, resourceVersion=%s",
		infra.Generation, infra.ResourceVersion)

	// Skip validation if not VSphere platform
	if !isVSpherePlatform(infra) {
		klog.V(4).Infof("Platform is not VSphere, skipping failure domain validation")
		// Clear any existing validation conditions for non-VSphere platforms
		return r.clearValidationCondition(ctx)
	}

	// Run VSphere validation
	validator := &VSphereFailureDomainValidator{client: r.client}
	result, err := validator.Validate(ctx, infra)
	if err != nil {
		klog.Errorf("Failed to validate failure domains: %v", err)
		return reconcile.Result{}, fmt.Errorf("failed to validate failure domains: %w", err)
	}

	// Update ClusterOperator conditions
	if err := r.updateClusterOperatorConditions(ctx, result); err != nil {
		klog.Errorf("Failed to update ClusterOperator conditions: %v", err)
		return reconcile.Result{}, fmt.Errorf("failed to update ClusterOperator conditions: %w", err)
	}

	// Note: We don't emit events on Infrastructure because it's cluster-scoped
	// and would create events in the default namespace, causing RBAC issues.
	// The ClusterOperator conditions provide the canonical status.

	return reconcile.Result{}, nil
}

// isVSpherePlatform checks if the Infrastructure is configured for VSphere.
func isVSpherePlatform(infra *configv1.Infrastructure) bool {
	return infra.Status.PlatformStatus != nil &&
		infra.Status.PlatformStatus.Type == configv1.VSpherePlatformType
}

// updateClusterOperatorConditions updates the ClusterOperator with validation results.
func (r *ReconcileInfrastructureValidation) updateClusterOperatorConditions(
	ctx context.Context,
	result *FailureDomainValidationResult,
) error {
	// Use APIReader to bypass cache and get latest ClusterOperator state
	co := &configv1.ClusterOperator{}
	if err := r.apiReader.Get(ctx, client.ObjectKey{Name: clusterOperatorName}, co); err != nil {
		if errors.IsNotFound(err) {
			klog.Warningf("ClusterOperator %s not found, cannot update condition", clusterOperatorName)
			return nil
		}
		return fmt.Errorf("failed to get ClusterOperator: %w", err)
	}

	// Create validation condition
	validationCondition := configv1.ClusterOperatorStatusCondition{
		Type: conditionType,
	}

	// Create degraded condition
	var degradedCondition configv1.ClusterOperatorStatusCondition

	if result.Valid {
		// Validation passed
		validationCondition.Status = configv1.ConditionTrue
		validationCondition.Reason = reasonAllReferencesValid
		validationCondition.Message = "All machine failure domain references are valid"

		// Only clear Degraded if it was set by us (matching our specific reason)
		existingDegraded := v1helpers.FindStatusCondition(co.Status.Conditions, configv1.OperatorDegraded)
		if existingDegraded != nil && existingDegraded.Reason == degradedReasonOrphanedReferences {
			degradedCondition.Type = configv1.OperatorDegraded
			degradedCondition.Status = configv1.ConditionFalse
			degradedCondition.Reason = reasonAllReferencesValid
			degradedCondition.Message = "All machine failure domain references are valid"
			v1helpers.SetStatusCondition(&co.Status.Conditions, degradedCondition, clock.RealClock{})
			klog.V(3).Info("Cleared Degraded condition (orphaned failure domain references resolved)")
		}
	} else {
		// Validation failed - machines reference removed failure domains
		validationCondition.Status = configv1.ConditionFalse
		validationCondition.Reason = reasonOrphanedReferences
		validationCondition.Message = formatOrphanedMessage(result.OrphanedReferences)

		// Set operator to Degraded
		degradedCondition.Type = configv1.OperatorDegraded
		degradedCondition.Status = configv1.ConditionTrue
		degradedCondition.Reason = degradedReasonOrphanedReferences
		degradedCondition.Message = fmt.Sprintf("Machines reference removed failure domains. %s",
			formatOrphanedMessage(result.OrphanedReferences))
		v1helpers.SetStatusCondition(&co.Status.Conditions, degradedCondition, clock.RealClock{})
		klog.Warningf("Set ClusterOperator to Degraded: %s", degradedCondition.Message)
	}

	// Set validation condition
	v1helpers.SetStatusCondition(&co.Status.Conditions, validationCondition, clock.RealClock{})

	// Update ClusterOperator status
	if err := r.client.Status().Update(ctx, co); err != nil {
		return fmt.Errorf("failed to update ClusterOperator status: %w", err)
	}

	klog.V(3).Infof("Updated ClusterOperator conditions: %s=%s, Degraded=%s",
		conditionType, validationCondition.Status, degradedCondition.Status)
	return nil
}

// clearValidationCondition removes the validation condition and our Degraded condition for non-VSphere platforms.
func (r *ReconcileInfrastructureValidation) clearValidationCondition(ctx context.Context) (reconcile.Result, error) {
	// Use APIReader to bypass cache and get latest ClusterOperator state
	co := &configv1.ClusterOperator{}
	if err := r.apiReader.Get(ctx, client.ObjectKey{Name: clusterOperatorName}, co); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("failed to get ClusterOperator: %w", err)
	}

	// Remove the validation condition and our specific Degraded condition if they exist
	changed := false
	newConditions := []configv1.ClusterOperatorStatusCondition{}
	for _, cond := range co.Status.Conditions {
		// Remove our validation condition
		if cond.Type == conditionType {
			changed = true
			continue
		}
		// Remove Degraded condition if it was set by us
		if cond.Type == configv1.OperatorDegraded && cond.Reason == degradedReasonOrphanedReferences {
			changed = true
			klog.V(3).Info("Cleared Degraded condition for non-VSphere platform")
			continue
		}
		newConditions = append(newConditions, cond)
	}

	if changed {
		co.Status.Conditions = newConditions
		if err := r.client.Status().Update(ctx, co); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update ClusterOperator status: %w", err)
		}
		klog.V(3).Infof("Cleared validation conditions for non-VSphere platform")
	}

	return reconcile.Result{}, nil
}

// formatOrphanedMessage formats the orphaned references into a human-readable message.
func formatOrphanedMessage(refs []OrphanedReference) string {
	if len(refs) == 0 {
		return ""
	}

	// Group by failure domain for cleaner message
	domainToMachines := make(map[string][]string)
	for _, ref := range refs {
		key := ref.FailureDomainName
		machine := fmt.Sprintf("%s/%s", ref.MachineNamespace, ref.MachineName)
		domainToMachines[key] = append(domainToMachines[key], machine)
	}

	var parts []string
	for domain, machines := range domainToMachines {
		if len(machines) == 1 {
			parts = append(parts, fmt.Sprintf("domain %q: %s", domain, machines[0]))
		} else {
			parts = append(parts, fmt.Sprintf("domain %q: %s and %d more",
				domain, machines[0], len(machines)-1))
		}
	}

	return fmt.Sprintf("Machines reference removed failure domains: %s",
		strings.Join(parts, "; "))
}
