package components

import (
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

// ServiceAccountUpdater can provide rolling updates on
// the given ServiceAccount.
type ServiceAccountUpdater struct {
	v1.ServiceAccount

	client opclient.Interface
	lister v1informers.ServiceAccountLister
}

// NewServiceAccountUpdater is responsible for updating a ServiceAccount
func NewServiceAccountUpdater(client opclient.Interface, cd *v1.ServiceAccount, lister v1informers.ServiceAccountLister) types.Component {
	return &ServiceAccountUpdater{
		ServiceAccount: *cd,
		client:         client,
		lister:         lister,
	}
}

// GetKind returns the kind of underlying object.
func (sau *ServiceAccountUpdater) GetKind() string {
	return manifest.KindServiceAccount
}

// Definition returns the underlying object.
func (sau *ServiceAccountUpdater) Definition() metav1.Object {
	return &sau.ServiceAccount
}

// Get fetches the cluster state for the underlying object.
func (sau *ServiceAccountUpdater) Get() (types.Component, error) {
	cm, err := sau.lister.ServiceAccounts(sau.GetNamespace()).Get(sau.GetName())
	if err != nil {
		return nil, err
	}
	return NewServiceAccountUpdater(sau.client, cm, sau.lister), nil
}

// Create will create the ServiceAccount.
func (sau *ServiceAccountUpdater) Create() error {
	_, err := sau.client.CreateServiceAccount(&sau.ServiceAccount)
	return err
}

// Delete deletes the object.
func (sau *ServiceAccountUpdater) Delete(options *metav1.DeleteOptions) error {
	return sau.client.DeleteServiceAccount(sau.GetNamespace(), sau.GetName(), options)
}

// List returns all the ServiceAccounts matching a selector in a namespace.
func (sau *ServiceAccountUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := sau.lister.ServiceAccounts(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewServiceAccountUpdater(sau.client, cms[i], sau.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the ServiceAccount if exists.
func (sau *ServiceAccountUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := sau.client.GetServiceAccount(sau.GetNamespace(), sau.GetName())
	if err != nil {
		return err
	}
	_, err = sau.client.UpdateServiceAccount(&sau.ServiceAccount)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (sau *ServiceAccountUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := sau.ServiceAccount.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the ServiceAccount.
func (sau *ServiceAccountUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := sau.client.GetServiceAccount(sau.GetNamespace(), sau.GetName())
	if apierrors.IsNotFound(err) {
		return sau.Create()
	}

	_, err = sau.client.UpdateServiceAccount(&sau.ServiceAccount)
	return err
}

// UpgradeIfExists upgrades the ServiceAccount if it exists.
func (sau *ServiceAccountUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := sau.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
