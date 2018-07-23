package components

import (
	netv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	netinformers "k8s.io/client-go/listers/networking/v1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// NetworkPolicyUpdater can provide rolling updates on
// the given NetworkPolicy
type NetworkPolicyUpdater struct {
	netv1.NetworkPolicy

	client opclient.Interface
	lister netinformers.NetworkPolicyLister
}

// NewNetworkPolicyUpdater is responsible for updating a NetworkPolicy
func NewNetworkPolicyUpdater(client opclient.Interface, p *netv1.NetworkPolicy, lister netinformers.NetworkPolicyLister) types.Component {
	return &NetworkPolicyUpdater{
		NetworkPolicy: *p,
		client:        client,
		lister:        lister,
	}
}

// GetKind returns the kind of underlying object.
func (nu *NetworkPolicyUpdater) GetKind() string {
	return manifest.KindNetworkPolicy
}

// Definition returns the underlying object.
func (nu *NetworkPolicyUpdater) Definition() metav1.Object {
	return &nu.NetworkPolicy
}

// Get fetches the cluster state for the underlying object.
func (nu *NetworkPolicyUpdater) Get() (types.Component, error) {
	cr, err := nu.lister.NetworkPolicies(nu.GetNamespace()).Get(nu.GetName())
	if err != nil {
		return nil, err
	}
	return NewNetworkPolicyUpdater(nu.client, cr, nu.lister), nil
}

// Create will create the NetworkPolicy.
func (nu *NetworkPolicyUpdater) Create() error {
	_, err := nu.client.CreateNetworkPolicy(&nu.NetworkPolicy)
	return err
}

// Delete deletes the object.
func (nu *NetworkPolicyUpdater) Delete(options *metav1.DeleteOptions) error {
	return nu.client.DeleteNetworkPolicy(nu.GetNamespace(), nu.GetName(), options)
}

// List returns all the NetworkPolicy matching a selector in a namespace.
func (nu *NetworkPolicyUpdater) List(_ string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	crs, err := nu.lister.List(sel)
	if err != nil {
		return nil, err
	}
	for i := range crs {
		cmps = append(cmps, NewNetworkPolicyUpdater(nu.client, crs[i], nu.lister))
	}
	return cmps, nil
}

// Upgrade will upgrade the NetworkPolicy if exists.
func (nu *NetworkPolicyUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := nu.client.GetNetworkPolicy(nu.GetNamespace(), nu.GetName())
	if err != nil {
		return err
	}
	_, err = nu.client.UpdateNetworkPolicy(&nu.NetworkPolicy)
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (nu *NetworkPolicyUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := nu.NetworkPolicy.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the NetworkPolicy.
func (nu *NetworkPolicyUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := nu.client.GetNetworkPolicy(nu.GetNamespace(), nu.GetName())
	if apierrors.IsNotFound(err) {
		return nu.Create()
	}

	_, err = nu.client.UpdateNetworkPolicy(&nu.NetworkPolicy)
	return err
}

// UpgradeIfExists upgrades the NetworkPolicy if it exists.
func (nu *NetworkPolicyUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := nu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
