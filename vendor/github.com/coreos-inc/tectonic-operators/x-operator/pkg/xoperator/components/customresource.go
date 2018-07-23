package components

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// CustomResourceUpdater updates the given CustomResource.
type CustomResourceUpdater struct {
	client opclient.Interface
	manifest.CustomResource
}

// NewCustomResourceUpdater is responsible for updating a CustomResource
func NewCustomResourceUpdater(client opclient.Interface, cr *manifest.CustomResource) types.Component {
	return &CustomResourceUpdater{
		client:         client,
		CustomResource: *cr,
	}
}

// GetKind returns the kind of underlying object.
func (u *CustomResourceUpdater) GetKind() string {
	return u.CustomResource.GetKind()
}

// Definition returns the underlying object.
func (u *CustomResourceUpdater) Definition() metav1.Object {
	return &u.CustomResource
}

// Get fetches the cluster state for the underlying object.
func (u *CustomResourceUpdater) Get() (types.Component, error) {
	gvk := u.CustomResource.GroupVersionKind()
	cr, err := u.client.GetCustomResource(gvk.Group, gvk.Version, u.CustomResource.GetNamespace(), gvk.Kind, u.CustomResource.GetName())
	if err != nil {
		return nil, err
	}
	return NewCustomResourceUpdater(u.client, &manifest.CustomResource{Unstructured: cr}), nil
}

// Create will create the CustomResource.
func (u *CustomResourceUpdater) Create() error {
	return u.client.CreateCustomResource(u.CustomResource.Unstructured)
}

// Delete deletes the object.
func (u *CustomResourceUpdater) Delete(_ *metav1.DeleteOptions) error {
	gvk := u.CustomResource.GroupVersionKind()
	return u.client.DeleteCustomResource(gvk.Group, gvk.Version, u.CustomResource.GetNamespace(), gvk.Kind, u.CustomResource.GetName())
}

//List returns all the CustomResources in a namespace. The selector is ignored.
func (u *CustomResourceUpdater) List(_ string, _ labels.Selector) ([]types.Component, error) {
	gvk := u.CustomResource.GroupVersionKind()
	crs, err := u.client.ListCustomResource(gvk.Group, gvk.Version, u.CustomResource.GetNamespace(), gvk.Kind)
	if err != nil {
		return nil, err
	}
	var cmps []types.Component
	for i := range crs.Items {
		cmps = append(cmps, NewCustomResourceUpdater(u.client, &manifest.CustomResource{Unstructured: crs.Items[i]}))
	}
	return cmps, nil
}

// Upgrade will upgrade the CustomResource if exists.
func (u *CustomResourceUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	gvk := u.CustomResource.GroupVersionKind()
	_, err := u.client.GetCustomResource(gvk.Group, gvk.Version, u.CustomResource.GetNamespace(), gvk.Kind, u.CustomResource.GetName())
	if err != nil {
		return err
	}

	switch strategy {
	case constants.UpgradeStrategyPatch, constants.UpgradeStrategyReplace:
		err = u.client.UpdateCustomResource(u.CustomResource.Unstructured)
	case constants.UpgradeStrategyDeleteAndRecreate:
		err = u.Delete(&metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		err = u.Create()
	default:
		return fmt.Errorf("unknown upgrade strategy")
	}
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (u *CustomResourceUpdater) CloneAndSanitize() (metav1.Object, error) {
	return u.CustomResource.DeepCopy(), nil
}

// CreateOrUpgrade creates or upgrades the CustomResource.
func (u *CustomResourceUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	gvk := u.CustomResource.GroupVersionKind()
	_, err := u.client.GetCustomResource(gvk.Group, gvk.Version, u.CustomResource.GetNamespace(), gvk.Kind, u.CustomResource.GetName())
	if apierrors.IsNotFound(err) {
		return u.Create()
	} else if err != nil {
		return err
	}
	return u.Upgrade(old, strategy)
}

// UpgradeIfExists upgrades the CustomResource if it exists.
func (u *CustomResourceUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := u.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
