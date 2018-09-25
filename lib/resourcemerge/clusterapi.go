package resourcemerge

import (
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
// We don't wanna implement this for now as we only deploy the cluster object
// because it's currenlty required by the actuator interface.
// see https://github.com/kubernetes-sigs/cluster-api/issues/490
func EnsureCluster(modified *bool, existing *clusterv1.Cluster, required *clusterv1.Cluster) {
	return
}
