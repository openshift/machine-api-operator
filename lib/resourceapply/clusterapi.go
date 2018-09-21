package resourceapply

import (
	"github.com/openshift/machine-api-operator/lib/resourcemerge"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1alpha "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

// ApplyMachineSet applies the required machineset to the cluster.
func ApplyMachineSet(client clientset.Interface, required *clusterv1alpha.MachineSet) (*clusterv1alpha.MachineSet, bool, error) {
	v1alphaClient := client.ClusterV1alpha1()
	existing, err := v1alphaClient.MachineSets(required.GetNamespace()).Get(required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := v1alphaClient.MachineSets(required.GetNamespace()).Create(required)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	resourcemerge.EnsureMachineSet(modified, existing, required)
	if !*modified {
		return existing, false, nil
	}

	actual, err := v1alphaClient.MachineSets(required.GetNamespace()).Update(existing)
	return actual, true, err
}

// ApplyCluster applies the required cluster object to the cluster.
func ApplyCluster(client clientset.Interface, required *clusterv1alpha.Cluster) (*clusterv1alpha.Cluster, bool, error) {
	v1alphaClient := client.ClusterV1alpha1()
	existing, err := v1alphaClient.Clusters(required.GetNamespace()).Get(required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		actual, err := v1alphaClient.Clusters(required.GetNamespace()).Create(required)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	resourcemerge.EnsureCluster(modified, existing, required)
	if !*modified {
		return existing, false, nil
	}

	actual, err := v1alphaClient.Clusters(required.GetNamespace()).Update(existing)
	return actual, true, err
}
