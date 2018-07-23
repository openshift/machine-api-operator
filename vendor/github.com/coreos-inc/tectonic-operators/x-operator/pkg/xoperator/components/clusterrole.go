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

// ClusterRoleUpdater can provide rolling updates on
// the given ClusterRole.
type ClusterRoleUpdater struct {
	rbacv1.ClusterRole

	client opclient.Interface
	lister rbacinformers.ClusterRoleLister
}

// NewClusterRoleUpdater is responsible for updating a ClusterRole
func NewClusterRoleUpdater(client opclient.Interface, cr *rbacv1.ClusterRole, lister rbacinformers.ClusterRoleLister) types.Component {
	return &ClusterRoleUpdater{
		ClusterRole: *cr,
		client:      client,
		lister:      lister,
	}
}

// GetKind returns the kind of underlying object.
func (cru *ClusterRoleUpdater) GetKind() string {
	return manifest.KindClusterRole
}

// Definition returns the underlying object.
func (cru *ClusterRoleUpdater) Definition() metav1.Object {
	return &cru.ClusterRole
}

// Get fetches the cluster state for the underlying object.
func (cru *ClusterRoleUpdater) Get() (types.Component, error) {
	cr, err := cru.lister.Get(cru.GetName())
	if err != nil {
		return nil, err
	}
	return NewClusterRoleUpdater(cru.client, cr, cru.lister), nil
}

// Create will create the ClusterRole.
func (cru *ClusterRoleUpdater) Create() error {
	_, err := cru.client.CreateClusterRole(&cru.ClusterRole)
	return err
}

// Delete deletes the object.
func (cru *ClusterRoleUpdater) Delete(options *metav1.DeleteOptions) error {
	return cru.client.DeleteClusterRole(cru.GetName(), options)
}

// List returns all the ClusterRole matching a selector in a namespace.
func (cru *ClusterRoleUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	crs, err := cru.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range crs {
		cmps = append(cmps, NewClusterRoleUpdater(cru.client, crs[i], cru.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the ClusterRole if exists.
func (cru *ClusterRoleUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := cru.client.GetClusterRole(cru.GetName())
	if err != nil {
		return err
	}
	_, err = cru.client.UpdateClusterRole(&cru.ClusterRole)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (cru *ClusterRoleUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := cru.ClusterRole.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the ClusterRole.
func (cru *ClusterRoleUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := cru.client.GetClusterRole(cru.GetName())
	if apierrors.IsNotFound(err) {
		return cru.Create()
	}

	_, err = cru.client.UpdateClusterRole(&cru.ClusterRole)
	return err
}

// UpgradeIfExists upgrades the ClusterRole if it exists.
func (cru *ClusterRoleUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := cru.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
