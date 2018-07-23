package components

import (
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
	apiregistrationv1beta1listers "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1beta1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// APIServiceUpdater can perform updates to the given APIService.
type APIServiceUpdater struct {
	apiregistrationv1beta1.APIService

	client opclient.Interface
	lister apiregistrationv1beta1listers.APIServiceLister
}

// NewAPIServiceUpdater is responsible for updating a APIService
func NewAPIServiceUpdater(client opclient.Interface, svc *apiregistrationv1beta1.APIService, lister apiregistrationv1beta1listers.APIServiceLister) types.Component {
	return &APIServiceUpdater{
		APIService: *svc,
		client:     client,
		lister:     lister,
	}
}

// GetKind returns the kind of underlying object.
func (u *APIServiceUpdater) GetKind() string {
	return manifest.KindAPIService
}

// Definition returns the underlying object.
func (u *APIServiceUpdater) Definition() metav1.Object {
	return &u.APIService
}

// Get fetches the cluster state for the underlying object.
func (u *APIServiceUpdater) Get() (types.Component, error) {
	cr, err := u.lister.Get(u.GetName())
	if err != nil {
		return nil, err
	}
	return NewAPIServiceUpdater(u.client, cr, u.lister), nil
}

// Create will create the APIService.
func (u *APIServiceUpdater) Create() error {
	_, err := u.client.CreateAPIService(&u.APIService)
	return err
}

// Delete deletes the object.
func (u *APIServiceUpdater) Delete(options *metav1.DeleteOptions) error {
	return u.client.DeleteAPIService(u.GetName(), options)
}

// List returns all the APIService matching a selector in a namespace.
func (u *APIServiceUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	svcs, err := u.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range svcs {
		cmps = append(cmps, NewAPIServiceUpdater(u.client, svcs[i], u.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the APIService if exists.
func (u *APIServiceUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := u.client.GetAPIService(u.GetName())
	if err != nil {
		return err
	}
	_, err = u.client.UpdateAPIService(&u.APIService)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (u *APIServiceUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := u.APIService.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the APIService.
func (u *APIServiceUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := u.client.GetAPIService(u.GetName())
	if apierrors.IsNotFound(err) {
		return u.Create()
	}

	_, err = u.client.UpdateAPIService(&u.APIService)
	return err
}

// UpgradeIfExists upgrades the APIService if it exists.
func (u *APIServiceUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := u.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
