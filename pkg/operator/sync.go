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
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/openshift/machine-api-operator/lib/resourceapply"
	"github.com/openshift/machine-api-operator/lib/resourceread"
	"github.com/openshift/machine-api-operator/pkg/render"
)

type syncFunc func(config render.OperatorConfig) error

func (optr *Operator) syncAll(rconfig render.OperatorConfig) error {
	// syncFuncs is the list of sync functions that are executed in order.
	// any error marks sync as failure but continues to next syncFunc
	syncFuncs := []syncFunc{
		// TODO(alberto): implement this once https://github.com/kubernetes-sigs/cluster-api/pull/488/files gets in
		//optr.syncMachineClasses,
		optr.syncMachineSets,
		optr.syncCluster,
	}

	//if err := optr.syncStatus(v1.OperatorStatusCondition{
	//	Type:    v1.OperatorStatusConditionTypeWorking,
	//	Message: "Running sync functions",
	//}); err != nil {
	//	return fmt.Errorf("error syncing status: %v", err)
	//}

	var errs []error
	for _, f := range syncFuncs {
		errs = append(errs, f(rconfig))
	}

	agg := utilerrors.NewAggregate(errs)
	if agg != nil {
		errs = append(errs, optr.syncDegradedStatus(agg))
		agg = utilerrors.NewAggregate(errs)
		return fmt.Errorf("error syncing: %v", agg.Error())
	}

	return optr.syncStatus(v1.OperatorStatusCondition{
		Type:    v1.OperatorStatusConditionTypeDone,
		Message: "Done running sync functions",
	})
}

func (optr *Operator) syncCustomResourceDefinitions() error {
	// TODO(alberto): implement this once https://github.com/kubernetes-sigs/cluster-api/pull/494 gets in
	//crds := []string{
	//	"manifests/machine.crd.yaml",
	//	"manifests/machineSet.crd.yaml",
	//	"manifests/cluster.crd.yaml",
	//}
	//
	//for _, crd := range crds {
	//	crdBytes, err := assets.Asset(crd)
	//	if err != nil {
	//		return fmt.Errorf("error getting asset %s: %v", crd, err)
	//	}
	//	c := resourceread.ReadCustomResourceDefinitionV1Beta1OrDie(crdBytes)
	//	_, updated, err := resourceapply.ApplyCustomResourceDefinition(optr.apiExtClient.ApiextensionsV1beta1(), c)
	//	if err != nil {
	//		return err
	//	}
	//	if updated {
	//		if err := optr.waitForCustomResourceDefinition(c); err != nil {
	//			return err
	//		}
	//	}
	//}

	return nil
}

func (optr *Operator) syncMachineSets(config render.OperatorConfig) error {
	var machineSets []string
	switch provider := config.Provider; provider {
	case providerAWS:
		machineSets = []string{
			"machines/aws/worker.machineset.yaml",
		}
	case providerLibvirt:
		machineSets = []string{
			"machines/libvirt/worker.machineset.yaml",
		}
	}
	for _, machineSet := range machineSets {
		machineSetBytes, err := render.PopulateTemplate(&config, machineSet)
		if err != nil {
			return err
		}
		p := resourceread.ReadMachineSetV1alphaOrDie(machineSetBytes)
		_, _, err = resourceapply.ApplyMachineSet(optr.clusterAPIClient, p)
		if err != nil {
			return err
		}
	}

	return nil
}

func (optr *Operator) syncCluster(config render.OperatorConfig) error {
	var clusters []string
	switch provider := config.Provider; provider {
	case providerAWS:
		clusters = []string{
			"machines/aws/cluster.yaml",
		}
	case providerLibvirt:
		clusters = []string{
			"machines/libvirt/cluster.yaml",
		}
	}
	for _, cluster := range clusters {
		clusterBytes, err := render.PopulateTemplate(&config, cluster)
		if err != nil {
			return err
		}
		p := resourceread.ReadClusterV1alphaOrDie(clusterBytes)
		_, _, err = resourceapply.ApplyCluster(optr.clusterAPIClient, p)
		if err != nil {
			return err
		}
	}

	return nil
}

func (optr *Operator) syncClusterAPIServer(config render.OperatorConfig) error {
	crbBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-apiserver-cluster-role-binding.yaml")
	if err != nil {
		return err
	}
	crb := resourceread.ReadClusterRoleBindingV1OrDie(crbBytes)
	_, _, err = resourceapply.ApplyClusterRoleBinding(optr.kubeClient.RbacV1(), crb)
	if err != nil {
		return err
	}

	rbBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-apiserver-role-binding.yaml")
	if err != nil {
		return err
	}
	rb := resourceread.ReadRoleBindingV1OrDie(rbBytes)
	_, _, err = resourceapply.ApplyRoleBinding(optr.kubeClient.RbacV1(), rb)
	if err != nil {
		return err
	}

	svcBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-apiserver-svc.yaml")
	if err != nil {
		return err
	}
	svc := resourceread.ReadServiceV1OrDie(svcBytes)
	_, _, err = resourceapply.ApplyService(optr.kubeClient.CoreV1(), svc)
	if err != nil {
		return err
	}

	apiServiceBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-apiservice.yaml")
	if err != nil {
		return err
	}
	apiService := resourceread.ReadAPIServiceDefinitionV1Beta1OrDie(apiServiceBytes)
	_, _, err = resourceapply.ApplyAPIServiceDefinition(optr.apiregistrationClient, apiService)
	if err != nil {
		return err
	}

	controllerBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-apiserver.yaml")
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

func (optr *Operator) syncClusterAPIController(config render.OperatorConfig) error {
	crBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-controller-cluster-role.yaml")
	if err != nil {
		return err
	}
	cr := resourceread.ReadClusterRoleV1OrDie(crBytes)
	_, _, err = resourceapply.ApplyClusterRole(optr.kubeClient.RbacV1(), cr)
	if err != nil {
		return err
	}
	crbBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-controller-cluster-role-binding.yaml")
	if err != nil {
		return err
	}
	crb := resourceread.ReadClusterRoleBindingV1OrDie(crbBytes)
	_, _, err = resourceapply.ApplyClusterRoleBinding(optr.kubeClient.RbacV1(), crb)
	if err != nil {
		return err
	}
	controllerBytes, err := render.PopulateTemplate(&config, "manifests/clusterapi-controller.yaml")
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
			return false, fmt.Errorf("Deployment %s is being deleted", resource.Name)
		}

		if d.Generation <= d.Status.ObservedGeneration && d.Status.UpdatedReplicas == d.Status.Replicas && d.Status.UnavailableReplicas == 0 {
			return true, nil
		}
		glog.V(4).Infof("Deployment %s is not ready. status: (replicas: %d, updated: %d, ready: %d, unavailable: %d)", d.Name, d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.UnavailableReplicas)
		return false, nil
	})
}
