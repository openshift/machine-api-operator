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

// RoleBindingUpdater can provide rolling updates on
// the given RoleBinding.
type RoleBindingUpdater struct {
	rbacv1.RoleBinding

	client opclient.Interface
	lister rbacinformers.RoleBindingLister
}

// NewRoleBindingUpdater is responsible for updating a RoleBinding
func NewRoleBindingUpdater(client opclient.Interface, r *rbacv1.RoleBinding, lister rbacinformers.RoleBindingLister) types.Component {
	return &RoleBindingUpdater{
		RoleBinding: *r,
		client:      client,
		lister:      lister,
	}
}

// GetKind returns the kind of underlying object.
func (ru *RoleBindingUpdater) GetKind() string {
	return manifest.KindRoleBinding
}

// Definition returns the underlying object.
func (ru *RoleBindingUpdater) Definition() metav1.Object {
	return &ru.RoleBinding
}

// Get fetches the cluster state for the underlying object.
func (ru *RoleBindingUpdater) Get() (types.Component, error) {
	cr, err := ru.lister.RoleBindings(ru.GetNamespace()).Get(ru.GetName())
	if err != nil {
		return nil, err
	}
	return NewRoleBindingUpdater(ru.client, cr, ru.lister), nil
}

// Create will create the RoleBinding.
func (ru *RoleBindingUpdater) Create() error {
	_, err := ru.client.CreateRoleBinding(&ru.RoleBinding)
	return err
}

// Delete deletes the object.
func (ru *RoleBindingUpdater) Delete(options *metav1.DeleteOptions) error {
	return ru.client.DeleteRoleBinding(ru.GetNamespace(), ru.GetName(), options)
}

// List returns all the RoleBinding matching a selector in a namespace.
func (ru *RoleBindingUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	crs, err := ru.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range crs {
		cmps = append(cmps, NewRoleBindingUpdater(ru.client, crs[i], ru.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the RoleBinding if exists.
func (ru *RoleBindingUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := ru.client.GetRoleBinding(ru.GetNamespace(), ru.GetName())
	if err != nil {
		return err
	}
	_, err = ru.client.UpdateRoleBinding(&ru.RoleBinding)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (ru *RoleBindingUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := ru.RoleBinding.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the RoleBinding.
func (ru *RoleBindingUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := ru.client.GetRoleBinding(ru.GetNamespace(), ru.GetName())
	if apierrors.IsNotFound(err) {
		return ru.Create()
	}

	_, err = ru.client.UpdateRoleBinding(&ru.RoleBinding)
	return err
}

// UpgradeIfExists upgrades the RoleBinding if it exists.
func (ru *RoleBindingUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := ru.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
