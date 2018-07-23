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

// ConfigMapUpdater can provide rolling updates on
// the given ConfigMap.
type ConfigMapUpdater struct {
	v1.ConfigMap

	client opclient.Interface
	lister v1informers.ConfigMapLister
}

// NewConfigMapUpdater is responsible for updating a ConfigMap
func NewConfigMapUpdater(client opclient.Interface, cd *v1.ConfigMap, lister v1informers.ConfigMapLister) types.Component {
	return &ConfigMapUpdater{
		ConfigMap: *cd,
		client:    client,
		lister:    lister,
	}
}

// GetKind returns the kind of underlying object.
func (cmu *ConfigMapUpdater) GetKind() string {
	return manifest.KindConfigMap
}

// Definition returns the underlying object.
func (cmu *ConfigMapUpdater) Definition() metav1.Object {
	return &cmu.ConfigMap
}

// Get fetches the cluster state for the underlying object.
func (cmu *ConfigMapUpdater) Get() (types.Component, error) {
	cm, err := cmu.lister.ConfigMaps(cmu.GetNamespace()).Get(cmu.GetName())
	if err != nil {
		return nil, err
	}
	return NewConfigMapUpdater(cmu.client, cm, cmu.lister), nil
}

// Create will create the ConfigMap.
func (cmu *ConfigMapUpdater) Create() error {
	_, err := cmu.client.CreateConfigMap(cmu.GetNamespace(), &cmu.ConfigMap)
	return err
}

// Delete deletes the object.
func (cmu *ConfigMapUpdater) Delete(options *metav1.DeleteOptions) error {
	return cmu.client.DeleteConfigMap(cmu.GetNamespace(), cmu.GetName(), options)
}

// List returns all the ConfigMaps matching a selector in a namespace.
func (cmu *ConfigMapUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := cmu.lister.ConfigMaps(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewConfigMapUpdater(cmu.client, cms[i], cmu.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the ConfigMap if exists.
func (cmu *ConfigMapUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := cmu.client.GetConfigMap(cmu.GetNamespace(), cmu.GetName())
	if err != nil {
		return err
	}
	_, err = cmu.client.AtomicUpdateConfigMap(cmu.GetNamespace(), cmu.GetName(), func(cm *v1.ConfigMap) error {
		cmu.DeepCopyInto(cm)
		return nil
	})
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (cmu *ConfigMapUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := cmu.ConfigMap.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the ConfigMap.
// TODO(abhinav): operator-client doesn't support patch for configmaps.
func (cmu *ConfigMapUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := cmu.client.GetConfigMap(cmu.GetNamespace(), cmu.GetName())
	if apierrors.IsNotFound(err) {
		return cmu.Create()
	}

	_, err = cmu.client.AtomicUpdateConfigMap(cmu.GetNamespace(), cmu.GetName(), func(cm *v1.ConfigMap) error {
		cm.Data = cmu.Data
		return nil
	})
	return err
}

// UpgradeIfExists upgrades the ConfigMap if it exists.
func (cmu *ConfigMapUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := cmu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
