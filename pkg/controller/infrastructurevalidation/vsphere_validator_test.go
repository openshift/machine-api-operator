package infrastructurevalidation

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	if err := machinev1.Install(scheme.Scheme); err != nil {
		klog.Fatal(err)
	}
	if err := configv1.Install(scheme.Scheme); err != nil {
		klog.Fatal(err)
	}
}

const (
	testNamespace = "openshift-machine-api"
)

// createVSphereMachine creates a test machine with region/zone labels
func createVSphereMachine(name, namespace, region, zone string) *machinev1.Machine {
	labels := make(map[string]string)
	if region != "" {
		labels[machineRegionLabelKey] = region
	}
	if zone != "" {
		labels[machineZoneLabelKey] = zone
	}

	return &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    labels,
		},
		Spec: machinev1.MachineSpec{},
	}
}

// createInfrastructure creates a test Infrastructure with VSphere failure domains
func createInfrastructure(failureDomains []failureDomainDef) *configv1.Infrastructure {
	fds := []configv1.VSpherePlatformFailureDomainSpec{}
	for _, fd := range failureDomains {
		fds = append(fds, configv1.VSpherePlatformFailureDomainSpec{
			Name:   fd.Name,
			Region: fd.Region,
			Zone:   fd.Zone,
		})
	}

	return &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: infrastructureName,
		},
		Spec: configv1.InfrastructureSpec{
			PlatformSpec: configv1.PlatformSpec{
				Type: configv1.VSpherePlatformType,
				VSphere: &configv1.VSpherePlatformSpec{
					FailureDomains: fds,
				},
			},
		},
		Status: configv1.InfrastructureStatus{
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.VSpherePlatformType,
			},
		},
	}
}

type failureDomainDef struct {
	Name   string
	Region string
	Zone   string
}

func TestVSphereValidatorAllReferencesValid(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
		{Name: "fd-west", Region: "us-west", Zone: "us-west-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a")
	machine2 := createVSphereMachine("machine-2", testNamespace, "us-west", "us-west-1a")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1, machine2).
		Build()

	validator := &VSphereFailureDomainValidator{client: fakeClient}
	result, err := validator.Validate(ctx, infra)

	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if !result.Valid {
		t.Errorf("Expected validation to pass, but got: %v", result.OrphanedReferences)
	}

	if len(result.OrphanedReferences) != 0 {
		t.Errorf("Expected no orphaned references, got %d", len(result.OrphanedReferences))
	}
}

func TestVSphereValidatorOrphanedReferences(t *testing.T) {
	ctx := context.Background()

	// Infrastructure only has fd-east
	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a") // valid
	machine2 := createVSphereMachine("machine-2", testNamespace, "us-west", "us-west-1a") // orphaned
	machine3 := createVSphereMachine("machine-3", testNamespace, "eu-west", "eu-west-1a") // orphaned

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1, machine2, machine3).
		Build()

	validator := &VSphereFailureDomainValidator{client: fakeClient}
	result, err := validator.Validate(ctx, infra)

	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected validation to fail, but it passed")
	}

	if len(result.OrphanedReferences) != 2 {
		t.Errorf("Expected 2 orphaned references, got %d", len(result.OrphanedReferences))
	}

	// Check that the right machines are orphaned
	orphanedMachines := make(map[string]string)
	for _, ref := range result.OrphanedReferences {
		orphanedMachines[ref.MachineName] = ref.FailureDomainName
	}

	if fd, ok := orphanedMachines["machine-2"]; !ok || fd != "region=us-west, zone=us-west-1a" {
		t.Errorf("Expected machine-2 to be orphaned with region=us-west, zone=us-west-1a, got %v", orphanedMachines)
	}

	if fd, ok := orphanedMachines["machine-3"]; !ok || fd != "region=eu-west, zone=eu-west-1a" {
		t.Errorf("Expected machine-3 to be orphaned with region=eu-west, zone=eu-west-1a, got %v", orphanedMachines)
	}
}

func TestVSphereValidatorNoFailureDomains(t *testing.T) {
	ctx := context.Background()

	// Infrastructure with no failure domains
	infra := createInfrastructure([]failureDomainDef{})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1).
		Build()

	validator := &VSphereFailureDomainValidator{client: fakeClient}
	result, err := validator.Validate(ctx, infra)

	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if result.Valid {
		t.Error("Expected validation to fail when machine references non-existent domain")
	}

	if len(result.OrphanedReferences) != 1 {
		t.Errorf("Expected 1 orphaned reference, got %d", len(result.OrphanedReferences))
	}
}

func TestVSphereValidatorMachineWithoutRegionZone(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})
	// Machine without region/zone labels
	machine1 := createVSphereMachine("machine-1", testNamespace, "", "")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1).
		Build()

	validator := &VSphereFailureDomainValidator{client: fakeClient}
	result, err := validator.Validate(ctx, infra)

	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected validation to pass for machine without region/zone labels")
	}

	if len(result.OrphanedReferences) != 0 {
		t.Errorf("Expected no orphaned references, got %d", len(result.OrphanedReferences))
	}
}

func TestVSphereValidatorMachineWithPartialLabels(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})

	tests := []struct {
		name              string
		region            string
		zone              string
		expectOrphaned    bool
		expectedReference string
	}{
		{
			name:              "Region only",
			region:            "us-east",
			zone:              "",
			expectOrphaned:    true,
			expectedReference: "region=us-east (missing zone)",
		},
		{
			name:              "Zone only",
			region:            "",
			zone:              "us-east-1a",
			expectOrphaned:    true,
			expectedReference: "zone=us-east-1a (missing region)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machine := createVSphereMachine("test-machine", testNamespace, tt.region, tt.zone)

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme.Scheme).
				WithObjects(infra, machine).
				Build()

			validator := &VSphereFailureDomainValidator{client: fakeClient}
			result, err := validator.Validate(ctx, infra)

			if err != nil {
				t.Fatalf("Validation failed: %v", err)
			}

			if tt.expectOrphaned {
				if result.Valid {
					t.Error("Expected validation to fail for machine with partial labels")
				}
				if len(result.OrphanedReferences) != 1 {
					t.Errorf("Expected 1 orphaned reference, got %d", len(result.OrphanedReferences))
				}
				if len(result.OrphanedReferences) > 0 && result.OrphanedReferences[0].FailureDomainName != tt.expectedReference {
					t.Errorf("Expected reference %q, got %q", tt.expectedReference, result.OrphanedReferences[0].FailureDomainName)
				}
			}
		})
	}
}

func TestVSphereValidatorNoMachines(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
		{Name: "fd-west", Region: "us-west", Zone: "us-west-1a"},
	})

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra).
		Build()

	validator := &VSphereFailureDomainValidator{client: fakeClient}
	result, err := validator.Validate(ctx, infra)

	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected validation to pass when no machines exist")
	}

	if len(result.OrphanedReferences) != 0 {
		t.Errorf("Expected no orphaned references, got %d", len(result.OrphanedReferences))
	}
}

func TestVSphereValidatorMultipleMachinesSameFailureDomain(t *testing.T) {
	ctx := context.Background()

	infra := createInfrastructure([]failureDomainDef{
		{Name: "fd-east", Region: "us-east", Zone: "us-east-1a"},
	})
	machine1 := createVSphereMachine("machine-1", testNamespace, "us-east", "us-east-1a")
	machine2 := createVSphereMachine("machine-2", testNamespace, "us-east", "us-east-1a")
	machine3 := createVSphereMachine("machine-3", testNamespace, "us-east", "us-east-1a")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithObjects(infra, machine1, machine2, machine3).
		Build()

	validator := &VSphereFailureDomainValidator{client: fakeClient}
	result, err := validator.Validate(ctx, infra)

	if err != nil {
		t.Fatalf("Validation failed: %v", err)
	}

	if !result.Valid {
		t.Error("Expected validation to pass for multiple machines in same failure domain")
	}

	if len(result.OrphanedReferences) != 0 {
		t.Errorf("Expected no orphaned references, got %d", len(result.OrphanedReferences))
	}
}

func TestFailureDomainKeyString(t *testing.T) {
	key := failureDomainKey{
		Region: "us-east",
		Zone:   "us-east-1a",
	}

	expected := "region=us-east, zone=us-east-1a"
	result := key.String()

	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}
