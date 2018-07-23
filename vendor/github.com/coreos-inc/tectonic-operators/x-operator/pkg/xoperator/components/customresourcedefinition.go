package components

import (
	"fmt"

	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	v1beta1listers "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// CustomResourceDefinitionUpdater can provide rolling updates on
// the given CustomResourceDefinition.
type CustomResourceDefinitionUpdater struct {
	v1beta1ext.CustomResourceDefinition

	client opclient.Interface
	lister v1beta1listers.CustomResourceDefinitionLister
}

// NewCustomResourceDefinitionUpdater is responsible for updating a CustomResourceDefinition
func NewCustomResourceDefinitionUpdater(client opclient.Interface, crdk *v1beta1ext.CustomResourceDefinition, lister v1beta1listers.CustomResourceDefinitionLister) types.Component {
	return &CustomResourceDefinitionUpdater{
		CustomResourceDefinition: *crdk,
		client: client,
		lister: lister,
	}
}

// GetKind returns the kind of underlying object.
func (crdu *CustomResourceDefinitionUpdater) GetKind() string {
	return manifest.KindCustomResourceDefinition
}

// Definition returns the underlying object.
func (crdu *CustomResourceDefinitionUpdater) Definition() metav1.Object {
	return &crdu.CustomResourceDefinition
}

// Get fetches the cluster state for the underlying object.
func (crdu *CustomResourceDefinitionUpdater) Get() (types.Component, error) {
	cm, err := crdu.lister.Get(crdu.GetName())
	if err != nil {
		return nil, err
	}
	return NewCustomResourceDefinitionUpdater(crdu.client, cm, crdu.lister), nil
}

// Create will create the CustomResourceDefinition.
func (crdu *CustomResourceDefinitionUpdater) Create() error {
	return crdu.client.CreateCustomResourceDefinition(&crdu.CustomResourceDefinition)
}

// Delete deletes the object.
func (crdu *CustomResourceDefinitionUpdater) Delete(options *metav1.DeleteOptions) error {
	return crdu.client.DeleteCustomResourceDefinition(crdu.GetName(), options)
}

//List returns all the CustomResourceDefinitions matching a selector in a namespace.
func (crdu *CustomResourceDefinitionUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := crdu.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewCustomResourceDefinitionUpdater(crdu.client, cms[i], crdu.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the CustomResourceDefinition if exists.
func (crdu *CustomResourceDefinitionUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := crdu.lister.Get(crdu.GetName())
	if err != nil {
		return err
	}

	switch strategy {
	case constants.UpgradeStrategyPatch, constants.UpgradeStrategyReplace:
		err = crdu.client.UpdateCustomResourceDefinition(&crdu.CustomResourceDefinition)
	case constants.UpgradeStrategyDeleteAndRecreate:
		delPolicy := metav1.DeletePropagationBackground
		err = crdu.Delete(&metav1.DeleteOptions{PropagationPolicy: &delPolicy})
		if err != nil {
			return err
		}
		err = crdu.Create()
	default:
		return fmt.Errorf("unknown upgrade strategy")
	}
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (crdu *CustomResourceDefinitionUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := crdu.CustomResourceDefinition.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the CustomResourceDefinition.
func (crdu *CustomResourceDefinitionUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := crdu.lister.Get(crdu.GetName())
	if apierrors.IsNotFound(err) {
		return crdu.Create()
	}
	return crdu.Upgrade(old, strategy)
}

// UpgradeIfExists upgrades the CustomResourceDefinition if it exists.
func (crdu *CustomResourceDefinitionUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := crdu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
