package infrastructurevalidation

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Machine label keys for region and zone
	machineRegionLabelKey = "machine.openshift.io/region"
	machineZoneLabelKey   = "machine.openshift.io/zone"
)

// FailureDomainValidationResult contains the results of failure domain validation.
type FailureDomainValidationResult struct {
	Valid              bool
	OrphanedReferences []OrphanedReference
	ConfiguredDomains  []string
}

// OrphanedReference represents a machine that references a removed failure domain.
type OrphanedReference struct {
	FailureDomainName string // Region+Zone combination
	MachineName       string
	MachineNamespace  string
}

// failureDomainKey represents a unique region+zone combination
type failureDomainKey struct {
	Region string
	Zone   string
}

// VSphereFailureDomainValidator validates VSphere failure domain references.
type VSphereFailureDomainValidator struct {
	client runtimeclient.Client
}

// Validate checks if any VSphere machines reference failure domains that have been removed
// from the Infrastructure configuration.
func (v *VSphereFailureDomainValidator) Validate(
	ctx context.Context,
	infra *configv1.Infrastructure,
) (*FailureDomainValidationResult, error) {
	result := &FailureDomainValidationResult{
		Valid:              true,
		OrphanedReferences: []OrphanedReference{},
		ConfiguredDomains:  []string{},
	}

	// Build map of configured failure domains by region+zone
	configuredFDs := make(map[failureDomainKey]string)
	if infra.Spec.PlatformSpec.VSphere != nil {
		for _, fd := range infra.Spec.PlatformSpec.VSphere.FailureDomains {
			key := failureDomainKey{
				Region: fd.Region,
				Zone:   fd.Zone,
			}
			configuredFDs[key] = fd.Name
			result.ConfiguredDomains = append(result.ConfiguredDomains, fd.Name)
		}
	}

	klog.V(4).Infof("Configured VSphere failure domains: %v", result.ConfiguredDomains)

	// List all machines
	machineList := &machinev1.MachineList{}
	if err := v.client.List(ctx, machineList); err != nil {
		return nil, fmt.Errorf("failed to list machines: %w", err)
	}

	klog.V(4).Infof("Checking %d machines for failure domain references", len(machineList.Items))

	// Check each machine's failure domain reference via region/zone labels
	for i := range machineList.Items {
		machine := &machineList.Items[i]

		// Extract region and zone from machine labels
		region := machine.Labels[machineRegionLabelKey]
		zone := machine.Labels[machineZoneLabelKey]

		// Skip machines without region/zone labels (not using failure domains)
		if region == "" && zone == "" {
			klog.V(5).Infof("Skipping machine %s/%s: no region/zone labels",
				machine.Namespace, machine.Name)
			continue
		}

		// If machine has partial labels, that's suspicious but we'll check it
		machineKey := failureDomainKey{
			Region: region,
			Zone:   zone,
		}

		// Check if this region+zone combination matches a configured failure domain
		fdName, exists := configuredFDs[machineKey]
		if !exists {
			// Machine references a region+zone that doesn't match any configured failure domain
			fdReference := fmt.Sprintf("region=%s, zone=%s", region, zone)
			if region == "" {
				fdReference = fmt.Sprintf("zone=%s (missing region)", zone)
			} else if zone == "" {
				fdReference = fmt.Sprintf("region=%s (missing zone)", region)
			}

			klog.Warningf("Machine %s/%s references removed or invalid failure domain: %s",
				machine.Namespace, machine.Name, fdReference)
			result.Valid = false
			result.OrphanedReferences = append(result.OrphanedReferences, OrphanedReference{
				FailureDomainName: fdReference,
				MachineName:       machine.Name,
				MachineNamespace:  machine.Namespace,
			})
		} else {
			klog.V(5).Infof("Machine %s/%s references valid failure domain %s (%s)",
				machine.Namespace, machine.Name, fdName, machineKey)
		}
	}

	if !result.Valid {
		klog.Warningf("Found %d machines with orphaned failure domain references", len(result.OrphanedReferences))
	} else {
		klog.V(3).Infof("All machine failure domain references are valid")
	}

	return result, nil
}

// String returns a string representation of the failureDomainKey
func (k failureDomainKey) String() string {
	return fmt.Sprintf("region=%s, zone=%s", k.Region, k.Zone)
}
