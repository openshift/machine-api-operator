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

// RoleUpdater can provide rolling updates on
// the given Role.
type RoleUpdater struct {
	rbacv1.Role

	client opclient.Interface
	lister rbacinformers.RoleLister
}

// NewRoleUpdater is responsible for updating a Role
func NewRoleUpdater(client opclient.Interface, r *rbacv1.Role, lister rbacinformers.RoleLister) types.Component {
	return &RoleUpdater{
		Role:   *r,
		client: client,
		lister: lister,
	}
}

// GetKind returns the kind of underlying object.
func (ru *RoleUpdater) GetKind() string {
	return manifest.KindRole
}

// Definition returns the underlying object.
func (ru *RoleUpdater) Definition() metav1.Object {
	return &ru.Role
}

// Get fetches the cluster state for the underlying object.
func (ru *RoleUpdater) Get() (types.Component, error) {
	cr, err := ru.lister.Roles(ru.GetNamespace()).Get(ru.GetName())
	if err != nil {
		return nil, err
	}
	return NewRoleUpdater(ru.client, cr, ru.lister), nil
}

// Create will create the Role.
func (ru *RoleUpdater) Create() error {
	_, err := ru.client.CreateRole(&ru.Role)
	return err
}

// Delete deletes the object.
func (ru *RoleUpdater) Delete(options *metav1.DeleteOptions) error {
	return ru.client.DeleteRole(ru.GetNamespace(), ru.GetName(), options)
}

// List returns all the Role matching a selector in a namespace.
func (ru *RoleUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	crs, err := ru.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range crs {
		cmps = append(cmps, NewRoleUpdater(ru.client, crs[i], ru.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the Role if exists.
func (ru *RoleUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := ru.client.GetRole(ru.GetNamespace(), ru.GetName())
	if err != nil {
		return err
	}
	_, err = ru.client.UpdateRole(&ru.Role)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (ru *RoleUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := ru.Role.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the Role.
func (ru *RoleUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := ru.client.GetRole(ru.GetNamespace(), ru.GetName())
	if apierrors.IsNotFound(err) {
		return ru.Create()
	}

	_, err = ru.client.UpdateRole(&ru.Role)
	return err
}

// UpgradeIfExists upgrades the Role if it exists.
func (ru *RoleUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := ru.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
