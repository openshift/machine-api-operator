package components

import (
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	rbacinformers "k8s.io/client-go/listers/rbac/v1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// ClusterRoleBindingUpdater can provide rolling updates on
// the given ClusterRoleBinding.
type ClusterRoleBindingUpdater struct {
	rbacv1.ClusterRoleBinding

	client opclient.Interface
	lister rbacinformers.ClusterRoleBindingLister
}

// NewClusterRoleBindingUpdater is responsible for updating a ClusterRoleBinding
func NewClusterRoleBindingUpdater(client opclient.Interface, crb *rbacv1.ClusterRoleBinding, lister rbacinformers.ClusterRoleBindingLister) types.Component {
	return &ClusterRoleBindingUpdater{
		ClusterRoleBinding: *crb,
		client:             client,
		lister:             lister,
	}
}

// GetKind returns the kind of underlying object.
func (crbu *ClusterRoleBindingUpdater) GetKind() string {
	return manifest.KindClusterRoleBinding
}

// Definition returns the underlying object.
func (crbu *ClusterRoleBindingUpdater) Definition() metav1.Object {
	return &crbu.ClusterRoleBinding
}

// Get fetches the cluster state for the underlying object.
func (crbu *ClusterRoleBindingUpdater) Get() (types.Component, error) {
	crb, err := crbu.lister.Get(crbu.GetName())
	if err != nil {
		return nil, err
	}
	return NewClusterRoleBindingUpdater(crbu.client, crb, crbu.lister), nil
}

// Create will create the ClusterRoleBinding.
func (crbu *ClusterRoleBindingUpdater) Create() error {
	_, err := crbu.client.CreateClusterRoleBinding(&crbu.ClusterRoleBinding)
	return err
}

// Delete deletes the object.
func (crbu *ClusterRoleBindingUpdater) Delete(options *metav1.DeleteOptions) error {
	return crbu.client.DeleteClusterRoleBinding(crbu.GetName(), options)
}

// List returns all the ClusterRoleBindings matching a selector in a namespace.
func (crbu *ClusterRoleBindingUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	crbs, err := crbu.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range crbs {
		cmps = append(cmps, NewClusterRoleBindingUpdater(crbu.client, crbs[i], crbu.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the ClusterRoleBinding if exists.
func (crbu *ClusterRoleBindingUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := crbu.client.GetClusterRoleBinding(crbu.GetName())
	if err != nil {
		return err
	}
	_, err = crbu.client.UpdateClusterRoleBinding(&crbu.ClusterRoleBinding)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (crbu *ClusterRoleBindingUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := crbu.ClusterRoleBinding.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the ClusterRoleBinding.
func (crbu *ClusterRoleBindingUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := crbu.client.GetClusterRoleBinding(crbu.GetName())
	if apierrors.IsNotFound(err) {
		return crbu.Create()
	}

	_, err = crbu.client.UpdateClusterRoleBinding(&crbu.ClusterRoleBinding)
	return err
}

// UpgradeIfExists upgrades the ClusterRoleBinding if it exists.
func (crbu *ClusterRoleBindingUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := crbu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
