package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"path/filepath"

	"github.com/openshift/cluster-version-operator/lib/resourceapply"
	"github.com/openshift/cluster-version-operator/lib/resourceread"
)

const (
	deploymentRolloutPollInterval = time.Second
	deploymentRolloutTimeout      = 5 * time.Minute
)

func (optr *Operator) syncAll(config OperatorConfig) error {
	glog.Infof("Syncing ClusterOperatorStatus")

	if err := optr.statusProgressing(ReasonSyncing, "Running sync functions"); err != nil {
		glog.Errorf("Error synching ClusterOperatorStatus: %v", err)
		return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}

	if err := optr.syncClusterAPIController(config); err != nil {
		if err := optr.statusFailing(ReasonSyncFailed, err.Error()); err != nil {
			// Just log the error here.  We still want to
			// return the outer error.
			glog.Errorf("Error synching ClusterOperatorStatus: %v", err)
		}

		glog.Errorf("Failed sync-up cluster api controller: %v", err)
		return err
	}

	glog.Info("Synched up cluster api controller")

	if err := optr.statusAvailable(ReasonEmpty, "cluster-api ready"); err != nil {
		glog.Errorf("Error synching ClusterOperatorStatus: %v", err)
		return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}

	return nil
}

func (optr *Operator) syncClusterAPIController(config OperatorConfig) error {
	controllerBytes, err := PopulateTemplate(&config, filepath.Join(ownedManifestsDir, "clusterapi-manager-controllers.yaml"))
	if err != nil {
		return err
	}
	controller := resourceread.ReadDeploymentV1OrDie(controllerBytes)
	_, updated, err := resourceapply.ApplyDeployment(optr.kubeClient.AppsV1(), controller)
	if err != nil {
		return err
	}
	if updated {
		return optr.waitForDeploymentRollout(controller)
	}
	return nil
}

func (optr *Operator) waitForDeploymentRollout(resource *appsv1.Deployment) error {
	return wait.Poll(deploymentRolloutPollInterval, deploymentRolloutTimeout, func() (bool, error) {
		// TODO(vikas): When using deployLister, an issue is happening related to the apiVersion of cluster-api objects.
		// This will be debugged later on to find out the root cause. For now, working aound is to use kubeClient.AppsV1
		// d, err := optr.deployLister.Deployments(resource.Namespace).Get(resource.Name)
		d, err := optr.kubeClient.AppsV1().Deployments(resource.Namespace).Get(resource.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			// Do not return error here, as we could be updating the API Server itself, in which case we
			// want to continue waiting.
			glog.Errorf("error getting Deployment %s during rollout: %v", resource.Name, err)
			return false, nil
		}

		if d.DeletionTimestamp != nil {
			return false, fmt.Errorf("deployment %s is being deleted", resource.Name)
		}

		if d.Generation <= d.Status.ObservedGeneration && d.Status.UpdatedReplicas == d.Status.Replicas && d.Status.UnavailableReplicas == 0 {
			return true, nil
		}
		glog.V(4).Infof("Deployment %s is not ready. status: (replicas: %d, updated: %d, ready: %d, unavailable: %d)", d.Name, d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.UnavailableReplicas)
		return false, nil
	})
}
