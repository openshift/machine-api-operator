package components

import (
	"fmt"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	v1informers "k8s.io/client-go/listers/core/v1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// ServiceUpdater can provide updates on given service.
type ServiceUpdater struct {
	v1.Service

	client opclient.Interface
	lister v1informers.ServiceLister
}

// NewServiceUpdater is responsible for updating a Service
func NewServiceUpdater(client opclient.Interface, cd *v1.Service, lister v1informers.ServiceLister) types.Component {
	return &ServiceUpdater{
		Service: *cd,
		client:  client,
		lister:  lister,
	}
}

// GetKind returns the kind of underlying object.
func (su *ServiceUpdater) GetKind() string {
	return manifest.KindService
}

// Definition returns the underlying object.
func (su *ServiceUpdater) Definition() metav1.Object {
	return &su.Service
}

// Get fetches the cluster state for the underlying object.
func (su *ServiceUpdater) Get() (types.Component, error) {
	cm, err := su.lister.Services(su.GetNamespace()).Get(su.GetName())
	if err != nil {
		return nil, err
	}
	return NewServiceUpdater(su.client, cm, su.lister), nil
}

// Create will create the Service.
func (su *ServiceUpdater) Create() error {
	_, err := su.client.CreateService(&su.Service)
	return err
}

// Delete deletes the object.
func (su *ServiceUpdater) Delete(options *metav1.DeleteOptions) error {
	return su.client.DeleteService(su.GetNamespace(), su.GetName(), options)
}

// List returns all the Services matching a selector in a namespace.
func (su *ServiceUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := su.lister.Services(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewServiceUpdater(su.client, cms[i], su.lister))
	}
	return cmps, nil
}

// Upgrade will update the service if exists.
func (su *ServiceUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := su.client.GetService(su.GetNamespace(), su.GetName())
	if err != nil {
		return err
	}

	var osvc, nsvc *v1.Service
	nsvc = &su.Service
	if old != nil {
		osvc = old.Definition().(*v1.Service)
	}

	switch strategy {
	case constants.UpgradeStrategyPatch:
		_, _, err = su.client.PatchService(osvc, nsvc)
	case constants.UpgradeStrategyReplace:
		_, _, err = su.client.UpdateService(nsvc)
	case constants.UpgradeStrategyDeleteAndRecreate:
		err = su.client.DeleteService(su.GetNamespace(), su.GetName(), &metav1.DeleteOptions{})
		if err != nil {
			return err
		}
		err = su.Create()
	default:
		return fmt.Errorf("unknown upgrade strategy")
	}
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (su *ServiceUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := su.Service.DeepCopy()
	obj.ObjectMeta.ResourceVersion = ""
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	obj.Status = v1.ServiceStatus{}
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the Service.
func (su *ServiceUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := su.client.GetService(su.GetNamespace(), su.GetName())
	if apierrors.IsNotFound(err) {
		return su.Create()
	}

	return su.Upgrade(old, strategy)
}

// UpgradeIfExists upgrades the Service if it exists.
func (su *ServiceUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := su.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
