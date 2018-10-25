package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/openshift/cluster-version-operator/pkg/apis/operatorstatus.openshift.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/machine-api-operator/lib/resourceapply"
	"github.com/openshift/machine-api-operator/lib/resourceread"
)

type syncFunc func(config OperatorConfig) error

func (optr *Operator) syncAll(config OperatorConfig) error {
	glog.Infof("Syncing operatorstatus")

	if err := optr.syncStatus(v1.OperatorStatusCondition{
		Type:    v1.OperatorStatusConditionTypeWorking,
		Message: "Running sync functions",
	}); err != nil {
		glog.Errorf("Error synching operatorstatus: %v", err)
		return fmt.Errorf("error syncing status: %v", err)
	}

	return optr.syncStatus(v1.OperatorStatusCondition{
		Type:    v1.OperatorStatusConditionTypeDone,
		Message: "Done running sync functions",
	})
}

func (optr *Operator) syncCustomResourceDefinitions(config OperatorConfig) error {
	crds := []string{
		"/machine.crd.yaml",
		"/machineset.crd.yaml",
		"/machinedeployment.crd.yaml",
		"/cluster.crd.yaml",
	}

	for _, crd := range crds {
		crdBytes, err := PopulateTemplate(&config, ownedManifestsDir+crd)
		if err != nil {
			return fmt.Errorf("error getting asset %s: %v", crd, err)
		}
		c := resourceread.ReadCustomResourceDefinitionV1Beta1OrDie(crdBytes)
		_, updated, err := resourceapply.ApplyCustomResourceDefinition(optr.apiExtClient.ApiextensionsV1beta1(), c)
		if err != nil {
			return err
		}
		if updated {
			if err := optr.waitForCustomResourceDefinition(c); err != nil {
				return err
			}
		}
	}
	return nil
}

func (optr *Operator) syncClusterAPIController(config OperatorConfig) error {
	crBytes, err := PopulateTemplate(&config, fmt.Sprintf("%s/clusterapi-manager-cluster-role.yaml", ownedManifestsDir))
	if err != nil {
		return err
	}
	cr := resourceread.ReadClusterRoleV1OrDie(crBytes)
	_, _, err = resourceapply.ApplyClusterRole(optr.kubeClient.RbacV1(), cr)
	if err != nil {
		return err
	}
	crbBytes, err := PopulateTemplate(&config, fmt.Sprintf("%s/clusterapi-manager-cluster-role-binding.yaml", ownedManifestsDir))
	if err != nil {
		return err
	}
	crb := resourceread.ReadClusterRoleBindingV1OrDie(crbBytes)
	_, _, err = resourceapply.ApplyClusterRoleBinding(optr.kubeClient.RbacV1(), crb)
	if err != nil {
		return err
	}
	controllerBytes, err := PopulateTemplate(&config, fmt.Sprintf("%s/clusterapi-manager-controllers.yaml", ownedManifestsDir))
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

const (
	deploymentRolloutPollInterval = time.Second
	deploymentRolloutTimeout      = 5 * time.Minute

	customResourceReadyInterval = time.Second
	customResourceReadyTimeout  = 5 * time.Minute
)

func (optr *Operator) waitForCustomResourceDefinition(resource *apiextv1beta1.CustomResourceDefinition) error {
	return wait.Poll(customResourceReadyInterval, customResourceReadyTimeout, func() (bool, error) {
		crd, err := optr.crdLister.Get(resource.Name)
		if err != nil {
			glog.Errorf("error getting CustomResourceDefinition %s: %v", resource.Name, err)
			return false, nil
		}

		for _, condition := range crd.Status.Conditions {
			if condition.Type == apiextv1beta1.Established && condition.Status == apiextv1beta1.ConditionTrue {
				return true, nil
			}
		}
		glog.V(4).Infof("CustomResourceDefinition %s is not ready. conditions: %v", crd.Name, crd.Status.Conditions)
		return false, nil
	})
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
