package components

import (
	"fmt"

	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	extensionsv1beta1informers "k8s.io/client-go/listers/extensions/v1beta1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// IngressUpdater can provide rolling updates on
// the given Ingress.
type IngressUpdater struct {
	extensionsv1beta1.Ingress

	client opclient.Interface
	lister extensionsv1beta1informers.IngressLister
}

// NewIngressUpdater is responsible for updating a Ingress
func NewIngressUpdater(client opclient.Interface, ig *extensionsv1beta1.Ingress, lister extensionsv1beta1informers.IngressLister) types.Component {
	return &IngressUpdater{
		Ingress: *ig,
		client:  client,
		lister:  lister,
	}
}

// GetKind returns the kind of underlying object.
func (igu *IngressUpdater) GetKind() string {
	return manifest.KindIngress
}

// Definition returns the underlying object.
func (igu *IngressUpdater) Definition() metav1.Object {
	return &igu.Ingress
}

// Get fetches the cluster state for the underlying object.
func (igu *IngressUpdater) Get() (types.Component, error) {
	cm, err := igu.lister.Ingresses(igu.GetNamespace()).Get(igu.GetName())
	if err != nil {
		return nil, err
	}
	return NewIngressUpdater(igu.client, cm, igu.lister), nil
}

// Create will create the Ingress.
// TODO(yifan): Maybe wrap the interface in op-client to remove all 'KubernetsInterface()' calls.
func (igu *IngressUpdater) Create() error {
	_, err := igu.client.CreateIngress(&igu.Ingress)
	return err
}

// Delete deletes the object.
func (igu *IngressUpdater) Delete(options *metav1.DeleteOptions) error {
	return igu.client.DeleteIngress(igu.GetNamespace(), igu.GetName(), options)
}

// List returns all the Ingresss matching a selector in a namespace.
func (igu *IngressUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := igu.lister.Ingresses(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewIngressUpdater(igu.client, cms[i], igu.lister))
	}
	return cmps, nil
}

// Upgrade will perform a rolling update across the pods
// in this Ingress if exists.
func (igu *IngressUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := igu.client.GetIngress(igu.GetNamespace(), igu.GetName())
	if err != nil {
		return err
	}

	var oig, nig *extensionsv1beta1.Ingress
	nig = &igu.Ingress
	if old != nil {
		oig = old.Definition().(*extensionsv1beta1.Ingress)
	}

	switch strategy {
	case constants.UpgradeStrategyPatch:
		_, _, err = igu.client.UpdateIngress(oig, nig)
	case constants.UpgradeStrategyReplace:
		_, _, err = igu.client.UpdateIngress(nil, nig)
	case constants.UpgradeStrategyDeleteAndRecreate:
		delPolicy := metav1.DeletePropagationBackground
		err = igu.Delete(&metav1.DeleteOptions{PropagationPolicy: &delPolicy})
		if err != nil {
			return err
		}
		err = igu.Create()
	default:
		return fmt.Errorf("unknown upgrade strategy")
	}
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (igu *IngressUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := igu.Ingress.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	obj.Status = extensionsv1beta1.IngressStatus{}
	return obj, nil
}

// CreateOrUpgrade creates the ingress if it doesn't exist, otherwise it updates it.
func (igu *IngressUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := igu.client.GetIngress(igu.GetNamespace(), igu.GetName())
	if apierrors.IsNotFound(err) {
		return igu.Create()
	}
	return igu.Upgrade(old, strategy)
}

// UpgradeIfExists upgrades the ingress iff it exists.
func (igu *IngressUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := igu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
