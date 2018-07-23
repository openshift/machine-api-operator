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
	deploymentRolloutPollDuration = 1 * time.Second
	deploymentUpdateTimeout       = 3 * time.Minute
)

// DeploymentUpdater is responsible for updating a Deployment.
type DeploymentUpdater struct {
	appsv1beta2.Deployment

	client opclient.Interface
	lister appsv1beta2informers.DeploymentLister
}

// NewDeploymentUpdater returns a component that can update a deployment.
func NewDeploymentUpdater(client opclient.Interface, cd *appsv1beta2.Deployment, lister appsv1beta2informers.DeploymentLister) types.Component {
	return &DeploymentUpdater{
		Deployment: *cd,
		client:     client,
		lister:     lister,
	}
}

// GetKind returns the kind of underlying object.
func (du *DeploymentUpdater) GetKind() string {
	return manifest.KindDeployment
}

// Definition returns the underlying object.
func (du *DeploymentUpdater) Definition() metav1.Object {
	return &du.Deployment
}

// Get fetches the cluster state for the underlying object.
func (du *DeploymentUpdater) Get() (types.Component, error) {
	cm, err := du.lister.Deployments(du.GetNamespace()).Get(du.GetName())
	if err != nil {
		return nil, err
	}
	return NewDeploymentUpdater(du.client, cm, du.lister), nil
}

// Create will create the Deployment.
func (du *DeploymentUpdater) Create() error {
	_, err := du.client.CreateDeployment(&du.Deployment)
	return err
}

// Delete deletes the object.
//
// NOTE: setting the DeleteOptions can enable deleting replica sets and pods
// associated with the deployment.
func (du *DeploymentUpdater) Delete(options *metav1.DeleteOptions) error {
	return du.client.DeleteDeployment(du.GetNamespace(), du.GetName(), options)
}

// List returns all the Deployments matching a selector in a namespace.
func (du *DeploymentUpdater) List(namespace string, sel labels.Selector) ([]types.Component, error) {
	var cmps []types.Component
	cms, err := du.lister.Deployments(namespace).List(sel)
	if err != nil {
		return nil, err
	}
	for i := range cms {
		cmps = append(cmps, NewDeploymentUpdater(du.client, cms[i], du.lister))
	}
	return cmps, nil
}

// Upgrade will update the Deployment if exists.
func (du *DeploymentUpdater) Upgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := du.client.GetDeployment(du.GetNamespace(), du.GetName())
	if err != nil {
		return err
	}

	var od, nd *appsv1beta2.Deployment
	nd = &du.Deployment
	if old != nil {
		od = old.Definition().(*appsv1beta2.Deployment)
	}

	switch strategy {
	case constants.UpgradeStrategyPatch:
		_, _, err = du.client.RollingPatchDeployment(od, nd)
	case constants.UpgradeStrategyReplace:
		_, _, err = du.client.RollingUpdateDeployment(nd)
	case constants.UpgradeStrategyDeleteAndRecreate:
		delPolicy := metav1.DeletePropagationBackground
		err = du.client.DeleteDeployment(nd.GetNamespace(), nd.GetName(), &metav1.DeleteOptions{PropagationPolicy: &delPolicy})
		if err != nil {
			return err
		}
		err = du.Create()
	default:
		return fmt.Errorf("unknown upgrade strategy")
	}
	return err
}

// CloneAndSanitize returns a copy of the object which is sanitized for equality/derived comaparisons.
func (du *DeploymentUpdater) CloneAndSanitize() (metav1.Object, error) {
	obj := du.Deployment.DeepCopy()
	obj.Spec.Replicas = nil
	obj.ObjectMeta.CreationTimestamp = metav1.Time{}
	obj.ObjectMeta.Generation = 0
	obj.Status = appsv1beta2.DeploymentStatus{}
	return obj, nil
}

// CreateOrUpgrade creates or upgrades the Deployment.
func (du *DeploymentUpdater) CreateOrUpgrade(old types.Component, strategy constants.UpgradeStrategy) error {
	_, err := du.client.GetDeployment(du.GetNamespace(), du.GetName())
	if apierrors.IsNotFound(err) {
		return du.Create()
	}

	return du.Upgrade(old, strategy)
}

// UpgradeIfExists upgrades the Deployment if it exists.
func (du *DeploymentUpdater) UpgradeIfExists(old types.Component, strategy constants.UpgradeStrategy) error {
	err := du.Upgrade(old, strategy)
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}
