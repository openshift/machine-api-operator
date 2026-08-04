package infrastructurevalidation

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	if err := machinev1.Install(scheme.Scheme); err != nil {
		klog.Fatal(err)
	}
	if err := configv1.Install(scheme.Scheme); err != nil {
		klog.Fatal(err)
	}
}

// createClusterOperator creates a test ClusterOperator
func createClusterOperator() *configv1.ClusterOperator {
	return &configv1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterOperatorName,
		},
		Status: configv1.ClusterOperatorStatus{
			Conditions: []configv1.ClusterOperatorStatusCondition{},
		},
	}
}

func TestReconcileVSphereValidReferences(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
		{Name: "fd-west", Region: "us-west", Zone: "us-west-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a")
	machine2 := createVSphereMachine("machine-2", testNamespace, "us-west", "us-west-1a")
	co := createClusterOperator()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1, machine2, co).
		WithStatusSubresource(co).
		Build()

	recorder := record.NewFakeRecorder(10)

	r := &ReconcileInfrastructureValidation{
		client:    fakeClient,
		apiReader: fakeClient, // In tests, use the same fake client as apiReader
		recorder:  recorder,
	}

	request := reconcile.Request{
		NamespacedName: client.ObjectKey{Name: infrastructureName},
	}

	result, err := r.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("Expected no requeue")
	}

	// Verify ClusterOperator condition was set correctly
	updatedCO := &configv1.ClusterOperator{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: clusterOperatorName}, updatedCO); err != nil {
		t.Fatalf("Failed to get ClusterOperator: %v", err)
	}

	foundValidation := false
	for _, cond := range updatedCO.Status.Conditions {
		if cond.Type == conditionType {
			foundValidation = true
			if cond.Status != configv1.ConditionTrue {
				t.Errorf("Expected condition status to be True, got %v", cond.Status)
			}
			if cond.Reason != reasonAllReferencesValid {
				t.Errorf("Expected reason %q, got %q", reasonAllReferencesValid, cond.Reason)
			}
		}
		if cond.Type == configv1.OperatorDegraded {
			// Degraded should not be set by us when validation passes
			if cond.Reason == degradedReasonOrphanedReferences {
				t.Errorf("Expected Degraded condition not to be set by us when validation passes")
			}
		}
	}

	if !foundValidation {
		t.Error("Expected validation condition to be set")
	}
}

func TestReconcileVSphereOrphanedReferences(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a") // valid
	machine2 := createVSphereMachine("machine-2", testNamespace, "us-west", "us-west-1a") // orphaned
	co := createClusterOperator()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1, machine2, co).
		WithStatusSubresource(co).
		Build()

	recorder := record.NewFakeRecorder(10)

	r := &ReconcileInfrastructureValidation{
		client:    fakeClient,
		apiReader: fakeClient, // In tests, use the same fake client as apiReader
		recorder:  recorder,
	}

	request := reconcile.Request{
		NamespacedName: client.ObjectKey{Name: infrastructureName},
	}

	result, err := r.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("Expected no requeue")
	}

	// Verify ClusterOperator conditions were set correctly
	updatedCO := &configv1.ClusterOperator{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: clusterOperatorName}, updatedCO); err != nil {
		t.Fatalf("Failed to get ClusterOperator: %v", err)
	}

	foundValidation := false
	foundDegraded := false
	for _, cond := range updatedCO.Status.Conditions {
		if cond.Type == conditionType {
			foundValidation = true
			if cond.Status != configv1.ConditionFalse {
				t.Errorf("Expected condition status to be False, got %v", cond.Status)
			}
			if cond.Reason != reasonOrphanedReferences {
				t.Errorf("Expected reason %q, got %q", reasonOrphanedReferences, cond.Reason)
			}
			// Message should contain the orphaned machine info
			if cond.Message == "" {
				t.Error("Expected non-empty message")
			}
		}
		if cond.Type == configv1.OperatorDegraded {
			foundDegraded = true
			if cond.Status != configv1.ConditionTrue {
				t.Errorf("Expected Degraded status to be True, got %v", cond.Status)
			}
			if cond.Reason != degradedReasonOrphanedReferences {
				t.Errorf("Expected Degraded reason %q, got %q", degradedReasonOrphanedReferences, cond.Reason)
			}
			if cond.Message == "" {
				t.Error("Expected non-empty Degraded message")
			}
		}
	}

	if !foundValidation {
		t.Error("Expected validation condition to be set")
	}
	if !foundDegraded {
		t.Error("Expected Degraded condition to be set when orphaned references exist")
	}

	// Note: We no longer emit events on Infrastructure (cluster-scoped resource)
	// to avoid RBAC issues with events in the default namespace.
	// The ClusterOperator conditions provide the canonical status.
}

func TestReconcileDegradedConditionCleared(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a")

	// Create ClusterOperator with pre-existing Degraded condition set by us
	co := createClusterOperator()
	co.Status.Conditions = []configv1.ClusterOperatorStatusCondition{
		{
			Type:    configv1.OperatorDegraded,
			Status:  configv1.ConditionTrue,
			Reason:  degradedReasonOrphanedReferences,
			Message: "Previously degraded",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1, co).
		WithStatusSubresource(co).
		Build()

	recorder := record.NewFakeRecorder(10)

	r := &ReconcileInfrastructureValidation{
		client:    fakeClient,
		apiReader: fakeClient, // In tests, use the same fake client as apiReader
		recorder:  recorder,
	}

	request := reconcile.Request{
		NamespacedName: client.ObjectKey{Name: infrastructureName},
	}

	result, err := r.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("Expected no requeue")
	}

	// Verify Degraded condition was cleared
	updatedCO := &configv1.ClusterOperator{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: clusterOperatorName}, updatedCO); err != nil {
		t.Fatalf("Failed to get ClusterOperator: %v", err)
	}

	for _, cond := range updatedCO.Status.Conditions {
		if cond.Type == configv1.OperatorDegraded {
			if cond.Status == configv1.ConditionTrue && cond.Reason == degradedReasonOrphanedReferences {
				t.Error("Expected Degraded condition to be cleared when validation passes")
			}
		}
	}
}

func TestReconcileNonVSpherePlatform(t *testing.T) {
	ctx := context.Background()

	// AWS platform instead of VSphere
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: infrastructureName,
		},
		Spec: configv1.InfrastructureSpec{
			PlatformSpec: configv1.PlatformSpec{
				Type: configv1.AWSPlatformType,
			},
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.AWSPlatformType,
			},
		},
	}
	co := createClusterOperator()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, co).
		Build()

	recorder := record.NewFakeRecorder(10)

	r := &ReconcileInfrastructureValidation{
		client:    fakeClient,
		apiReader: fakeClient, // In tests, use the same fake client as apiReader
		recorder:  recorder,
	}

	request := reconcile.Request{
		NamespacedName: client.ObjectKey{Name: infrastructureName},
	}

	result, err := r.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("Expected no requeue")
	}

	// Verify validation condition was NOT set (or was removed)
	updatedCO := &configv1.ClusterOperator{}
	if err := fakeClient.Get(ctx, client.ObjectKey{Name: clusterOperatorName}, updatedCO); err != nil {
		t.Fatalf("Failed to get ClusterOperator: %v", err)
	}

	for _, cond := range updatedCO.Status.Conditions {
		if cond.Type == conditionType {
			t.Error("Expected validation condition to be removed for non-VSphere platform")
		}
	}
}

func TestReconcileInfrastructureNotFound(t *testing.T) {
	ctx := context.Background()

	// No Infrastructure object
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		Build()

	recorder := record.NewFakeRecorder(10)

	r := &ReconcileInfrastructureValidation{
		client:    fakeClient,
		apiReader: fakeClient, // In tests, use the same fake client as apiReader
		recorder:  recorder,
	}

	request := reconcile.Request{
		NamespacedName: client.ObjectKey{Name: infrastructureName},
	}

	result, err := r.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("Reconcile failed: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("Expected no requeue when Infrastructure not found")
	}
}

func TestReconcileClusterOperatorNotFound(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a")

	// No ClusterOperator object
	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1).
		Build()

	recorder := record.NewFakeRecorder(10)

	r := &ReconcileInfrastructureValidation{
		client:    fakeClient,
		apiReader: fakeClient, // In tests, use the same fake client as apiReader
		recorder:  recorder,
	}

	request := reconcile.Request{
		NamespacedName: client.ObjectKey{Name: infrastructureName},
	}

	// Should not fail when ClusterOperator doesn't exist
	result, err := r.Reconcile(ctx, request)
	if err != nil {
		t.Fatalf("Reconcile should not fail when ClusterOperator not found: %v", err)
	}

	if result.RequeueAfter > 0 {
		t.Error("Expected no requeue")
	}
}

func TestIsVSpherePlatform(t *testing.T) {
	tests := []struct {
		name     string
		infra    *configv1.Infrastructure
		expected bool
	}{
		{
			name: "VSphere platform",
			infra: createInfrastructure([]failureDomainDef{
				{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
			}),
			expected: true,
		},
		{
			name: "AWS platform",
			infra: &configv1.Infrastructure{
				Status: configv1.InfrastructureStatus{
					PlatformStatus: &configv1.PlatformStatus{
						Type: configv1.AWSPlatformType,
					},
				},
			},
			expected: false,
		},
		{
			name: "No platform status",
			infra: &configv1.Infrastructure{
				Status: configv1.InfrastructureStatus{},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isVSpherePlatform(tt.infra)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFormatOrphanedMessage(t *testing.T) {
	tests := []struct {
		name     string
		refs     []OrphanedReference
		expected string
	}{
		{
			name:     "Empty references",
			refs:     []OrphanedReference{},
			expected: "",
		},
		{
			name: "Single reference",
			refs: []OrphanedReference{
				{
					FailureDomainName: "fd-west",
					MachineName:       "machine-1",
					MachineNamespace:  "openshift-machine-api",
				},
			},
			expected: "Machines reference removed failure domains: domain \"fd-west\": openshift-machine-api/machine-1",
		},
		{
			name: "Multiple references same domain",
			refs: []OrphanedReference{
				{
					FailureDomainName: "fd-west",
					MachineName:       "machine-1",
					MachineNamespace:  "openshift-machine-api",
				},
				{
					FailureDomainName: "fd-west",
					MachineName:       "machine-2",
					MachineNamespace:  "openshift-machine-api",
				},
			},
			expected: "Machines reference removed failure domains: domain \"fd-west\": openshift-machine-api/machine-1 and 1 more",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatOrphanedMessage(tt.refs)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestInfrastructureRequestFromMachine(t *testing.T) {
	ctx := context.Background()
	machine := createVSphereMachine("test-machine", testNamespace, "us-east", "us-east-1a")

	requests := infrastructureRequestFromMachine(ctx, machine)

	if len(requests) != 1 {
		t.Errorf("Expected 1 request, got %d", len(requests))
	}

	if requests[0].Name != infrastructureName {
		t.Errorf("Expected Infrastructure name %q, got %q", infrastructureName, requests[0].Name)
	}

	if requests[0].Namespace != "" {
		t.Errorf("Expected empty namespace, got %q", requests[0].Namespace)
	}
}
