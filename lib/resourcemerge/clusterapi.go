package resourcemerge

import (
	"bytes"

	"k8s.io/apimachinery/pkg/api/equality"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

// EnsureMachineSet ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func EnsureMachineSet(modified *bool, existing *clusterv1.MachineSet, required *clusterv1.MachineSet) {
	EnsureObjectMeta(modified, &existing.ObjectMeta, required.ObjectMeta)
	ensureMachineSetSpec(modified, &existing.Spec, required.Spec)
}

// ensureMachineSetSpec ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func ensureMachineSetSpec(modified *bool, existing *clusterv1.MachineSetSpec, required clusterv1.MachineSetSpec) {
	setInt32IfSet(modified, existing.Replicas, *required.Replicas)
	if !equality.Semantic.DeepEqual(existing.Selector, required.Selector) {
		*modified = true
		existing.Selector = required.Selector
	}
	EnsureObjectMeta(modified, &existing.Template.Spec.ObjectMeta, required.Template.Spec.ObjectMeta)
	if !equality.Semantic.DeepEqual(existing.Template.Spec.Taints, required.Template.Spec.Taints) {
		*modified = true
		existing.Template.Spec.Taints = required.Template.Spec.Taints
	}
	// TODO(vikasc): verify if other fields also needs to be synced
}

// EnsureCluster ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func EnsureCluster(modified *bool, existing *clusterv1.Cluster, required *clusterv1.Cluster) {
	EnsureObjectMeta(modified, &existing.ObjectMeta, required.ObjectMeta)
	ensureClusterSpec(modified, &existing.Spec, required.Spec)
}

// ensureClusterSpec ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func ensureClusterSpec(modified *bool, existing *clusterv1.ClusterSpec, required clusterv1.ClusterSpec) {
	for _, required := range required.ClusterNetwork.Services.CIDRBlocks {
		found := false
		for _, curr := range existing.ClusterNetwork.Services.CIDRBlocks {
			if curr == required {
				found = true
				break
			}
		}
		if !found {
			*modified = true
			existing.ClusterNetwork.Services.CIDRBlocks = append(existing.ClusterNetwork.Services.CIDRBlocks, required)
		}
	}

	for _, required := range required.ClusterNetwork.Pods.CIDRBlocks {
		found := false
		for _, curr := range existing.ClusterNetwork.Pods.CIDRBlocks {
			if curr == required {
				found = true
				break
			}
		}
		if !found {
			*modified = true
			existing.ClusterNetwork.Pods.CIDRBlocks = append(existing.ClusterNetwork.Pods.CIDRBlocks, required)
		}
	}
	setStringIfSet(modified, &existing.ClusterNetwork.ServiceDomain, required.ClusterNetwork.ServiceDomain)

	// TODO(vikasc) In future we might need to compare individual fields
	if !clusterProviderConfigValueEqual(existing, required) {
		existing.ProviderConfig.Value = required.ProviderConfig.Value
	}

}

func clusterProviderConfigValueEqual(existing *clusterv1.ClusterSpec, required clusterv1.ClusterSpec) bool {
	existingBytes := existing.ProviderConfig.Value.Raw
	requiredBytes := required.ProviderConfig.Value.Raw
	if bytes.Equal(existingBytes, requiredBytes) {
		return true
	}
	return false
}
