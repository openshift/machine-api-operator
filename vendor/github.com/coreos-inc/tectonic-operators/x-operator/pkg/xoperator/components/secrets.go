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

// SecretUpdater can provide rolling updates on
// the given Secret.
type SecretUpdater struct {
	v1.Secret

	client opclient.Interface
	lister v1informers.SecretLister
}

// NewSecretUpdater is responsible for updating a Secret
func NewSecretUpdater(client opclient.Interface, cd *v1.Secret, lister v1informers.SecretLister) types.Component {
	return &SecretUpdater{
		Secret: *cd,
		client: client,
		lister: lister,
	}
}

// GetKind returns the kind of underlying object.
func (cmu *SecretUpdater) GetKind() string {
	return manifest.KindSecret
}

// Definition returns the underlying object.
func (cmu *SecretUpdater) Definition() metav1.Object {
	return &cmu.Secret
}

// Get fetches the cluster state for the underlying object.
func (cmu *SecretUpdater) Get() (types.Component, error) {
	cm, err := cmu.lister.Secrets(cmu.GetNamespace()).Get(cmu.GetName())
	if err != nil {
		return nil, err
	}
	return NewSecretUpdater(cmu.client, cm, cmu.lister), nil
}

// Create will create the Secret.
func (cmu *SecretUpdater) Create() error {
	_, err := cmu.client.CreateSecret(cmu.GetNamespace(), &cmu.Secret)
	return err
}

// Delete deletes the object.
func (cmu *SecretUpdater) Delete(options *metav1.DeleteOptions) error {
	return cmu.client.DeleteSecret(cmu.GetNamespace(), cmu.GetName(), options)
}

// List returns all the Secrets matching a selector in a namespace.
func (cmu *SecretUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := cmu.lister.Secrets(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewSecretUpdater(cmu.client, cms[i], cmu.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the Secret if exists.
func (cmu *SecretUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := cmu.client.GetSecret(cmu.GetNamespace(), cmu.GetName())
	if err != nil {
		return err
	}
	_, err = cmu.client.AtomicUpdateSecret(cmu.GetNamespace(), cmu.GetName(), func(cm *v1.Secret) error {
		cmu.DeepCopyInto(cm)
		return nil
	})
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (cmu *SecretUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := cmu.Secret.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the Secret.
// TODO(abhinav): operator-client doesn't support patch for secrets.
func (cmu *SecretUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := cmu.client.GetSecret(cmu.GetNamespace(), cmu.GetName())
	if apierrors.IsNotFound(err) {
		return cmu.Create()
	}

	_, err = cmu.client.AtomicUpdateSecret(cmu.GetNamespace(), cmu.GetName(), func(cm *v1.Secret) error {
		cm.Data = cmu.Data
		return nil
	})
	return err
}

// UpgradeIfExists upgrades the Secret if it exists.
func (cmu *SecretUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := cmu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
