package resourcemerge

import (
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
	// TODO(alberto): implement this
}
