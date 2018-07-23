package components

import (
	"fmt"
	"time"

	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	appsv1beta2informers "k8s.io/client-go/listers/apps/v1beta2"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

const (
	daemonsetRolloutPollDuration = 1 * time.Second
	daemonsetUpdateTimeout       = 3 * time.Minute
)

// DaemonSetUpdater can provide rolling updates on
// the given DaemonSet.
type DaemonSetUpdater struct {
	appsv1beta2.DaemonSet

	client opclient.Interface
	lister appsv1beta2informers.DaemonSetLister
}

// NewDaemonSetUpdater is responsible for updating a DaemonSet
func NewDaemonSetUpdater(client opclient.Interface, cd *appsv1beta2.DaemonSet, lister appsv1beta2informers.DaemonSetLister) types.Component {
	return &DaemonSetUpdater{
		DaemonSet: *cd,
		client:    client,
		lister:    lister,
	}
}

// GetKind returns the kind of underlying object.
func (dsu *DaemonSetUpdater) GetKind() string {
	return manifest.KindDaemonSet
}

// Definition returns the underlying object.
func (dsu *DaemonSetUpdater) Definition() metav1.Object {
	return &dsu.DaemonSet
}

// Get fetches the cluster state for the underlying object.
func (dsu *DaemonSetUpdater) Get() (types.Component, error) {
	cm, err := dsu.lister.DaemonSets(dsu.GetNamespace()).Get(dsu.GetName())
	if err != nil {
		return nil, err
	}
	return NewDaemonSetUpdater(dsu.client, cm, dsu.lister), nil
}

// Create will create the DaemonSet.
func (dsu *DaemonSetUpdater) Create() error {
	_, err := dsu.client.CreateDaemonSet(&dsu.DaemonSet)
	return err
}

// Delete deletes the object.
//
// NOTE: setting the DeleteOptions can enable deleting replica sets and pods
// associated with the daemon set.
func (dsu *DaemonSetUpdater) Delete(options *metav1.DeleteOptions) error {
	return dsu.client.DeleteDaemonSet(dsu.GetNamespace(), dsu.GetName(), options)
}

// List returns all the DaemonSets matching a selector in a namespace.
func (dsu *DaemonSetUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := dsu.lister.DaemonSets(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewDaemonSetUpdater(dsu.client, cms[i], dsu.lister))
	}
	return cmps, nil
}

// Upgrade will perform a rolling update across the pods
// in this DaemonSet if exists.
func (dsu *DaemonSetUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := dsu.client.GetDaemonSet(dsu.GetNamespace(), dsu.GetName())
	if err != nil {
		return err
	}

	var ods, nds *appsv1beta2.DaemonSet
	nds = &dsu.DaemonSet
	if old != nil {
		ods = old.Definition().(*appsv1beta2.DaemonSet)
	}

	switch strategy {
	case constants.UpgradeStrategyPatch:
		_, _, err = dsu.client.RollingPatchDaemonSet(ods, nds)
	case constants.UpgradeStrategyReplace:
		_, _, err = dsu.client.RollingUpdateDaemonSet(nds)
	case constants.UpgradeStrategyDeleteAndRecreate:
		delPolicy := metav1.DeletePropagationBackground
		err = dsu.client.DeleteDaemonSet(nds.GetNamespace(), nds.GetName(), &metav1.DeleteOptions{PropagationPolicy: &delPolicy})
		if err != nil {
			return err
		}
		err = dsu.Create()
	default:
		return fmt.Errorf("unknown upgrade strategy")
	}
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (dsu *DaemonSetUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := dsu.DaemonSet.DeepCopy()
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	obj.Status = appsv1beta2.DaemonSetStatus{}
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the DaemonSet.
func (dsu *DaemonSetUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := dsu.client.GetDaemonSet(dsu.GetNamespace(), dsu.GetName())
	if apierrors.IsNotFound(err) {
		return dsu.Create()
	}

	return dsu.Upgrade(old, strategy)
}

// UpgradeIfExists upgrades the DaemonSet if it exists.
func (dsu *DaemonSetUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := dsu.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
