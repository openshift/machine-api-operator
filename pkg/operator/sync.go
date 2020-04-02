package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
)

const (
	deploymentRolloutPollInterval = time.Second
	deploymentRolloutTimeout      = 5 * time.Minute
	daemonsetRolloutPollInterval  = time.Second
	daemonsetRolloutTimeout       = 5 * time.Minute
	machineAPITerminationHandler  = "machine-api-termination-handler"
)

func (optr *Operator) syncAll(config *OperatorConfig) error {
	if err := optr.statusProgressing(); err != nil {
		glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}

	if config.Controllers.Provider == clusterAPIControllerNoOp {
		glog.V(3).Info("Provider is NoOp, skipping synchronisation")
		if err := optr.statusAvailable(); err != nil {
			glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
			return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
		}
		return nil
	}

	if err := optr.syncClusterAPIController(config); err != nil {
		if err := optr.statusDegraded(err.Error()); err != nil {
			// Just log the error here.  We still want to
			// return the outer error.
			glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		}
		glog.Errorf("Error syncing machine-api-controller: %v", err)
		return err
	}
	glog.V(3).Info("Synced up all machine-api-controller components")

	// In addition, if the Provider is BareMetal, then bring up
	// the baremetal-operator pod
	if config.BaremetalControllers.BaremetalOperator != "" {
		if err := optr.syncBaremetalControllers(config); err != nil {
			if err := optr.statusDegraded(err.Error()); err != nil {
				// Just log the error here.  We still want to
				// return the outer error.
				glog.Errorf("Error syncing BaremetalOperatorStatus: %v", err)
			}
			glog.Errorf("Error syncing metal3-controller: %v", err)
			return err
		}
		glog.V(3).Info("Synced up all metal3 components")
	}

	if err := optr.statusAvailable(); err != nil {
		glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}
	return nil
}

func (optr *Operator) syncClusterAPIController(config *OperatorConfig) error {
	controllersDeployment := newDeployment(config, nil)
	expectedGeneration := resourcemerge.ExpectedDeploymentGeneration(controllersDeployment, optr.generations)
	d, updated, err := resourceapply.ApplyDeployment(optr.kubeClient.AppsV1(),
		events.NewLoggingEventRecorder(optr.name), controllersDeployment, expectedGeneration, false)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetDeploymentGeneration(&optr.generations, d)
		err := optr.waitForDeploymentRollout(controllersDeployment)
		if err != nil {
			return err
		}
	}

	// Sync Termination Handler DaemonSet if supported
	if config.Controllers.TerminationHandler != clusterAPIControllerNoOp {
		if err := optr.syncTerminationHandler(config); err != nil {
			return err
		}
	}
	return nil
}

func (optr *Operator) syncTerminationHandler(config *OperatorConfig) error {
	terminationDaemonSet := newTerminationDaemonSet(config)
	expectedGeneration := resourcemerge.ExpectedDaemonSetGeneration(terminationDaemonSet, optr.generations)
	ds, updated, err := resourceapply.ApplyDaemonSet(optr.kubeClient.AppsV1(),
		events.NewLoggingEventRecorder(optr.name), terminationDaemonSet, expectedGeneration, false)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetDaemonSetGeneration(&optr.generations, ds)
		return optr.waitForDaemonSetRollout(terminationDaemonSet)
	}
	return nil
}

func (optr *Operator) syncBaremetalControllers(config *OperatorConfig) error {
	// Try to get baremetal provisioning config from a CR
	baremetalProvisioningConfig, err := getBaremetalProvisioningConfig(optr.dynamicClient, baremetalProvisioningCR)
	if err != nil {
		glog.Errorf("Unable to read Baremetal Provisioning config from CR %s.", baremetalProvisioningCR)
		glog.Infof("Will try to read Baremetal Provisioning config from ConfigMap %s instead", baremetalConfigmap)
	}
	// Create a Secret needed for the Metal3 deployment
	if err := createMariadbPasswordSecret(optr.kubeClient.CoreV1(), config); err != nil {
		glog.Error("Not proceeding with Metal3 deployment. Failed to create Mariadb password.")
		return err
	}

	metal3Deployment := newMetal3Deployment(config, baremetalProvisioningConfig)
	expectedGeneration := resourcemerge.ExpectedDeploymentGeneration(metal3Deployment, optr.generations)
	d, updated, err := resourceapply.ApplyDeployment(optr.kubeClient.AppsV1(),
		events.NewLoggingEventRecorder(optr.name), metal3Deployment, expectedGeneration, false)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetDeploymentGeneration(&optr.generations, d)
		return optr.waitForDeploymentRollout(metal3Deployment)
	}

	return nil
}

func (optr *Operator) waitForDeploymentRollout(resource *appsv1.Deployment) error {
	var lastError error
	err := wait.Poll(deploymentRolloutPollInterval, deploymentRolloutTimeout, func() (bool, error) {
		d, err := optr.deployLister.Deployments(resource.Namespace).Get(resource.Name)
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			// Do not return error here, as we could be updating the API Server itself, in which case we
			// want to continue waiting.
			lastError = fmt.Errorf("getting Deployment %s during rollout: %v", resource.Name, err)
			glog.Error(lastError)
			return false, nil
		}

		if d.DeletionTimestamp != nil {
			lastError = nil
			return false, fmt.Errorf("deployment %s is being deleted", resource.Name)
		}

		if d.Generation <= d.Status.ObservedGeneration && d.Status.UpdatedReplicas == d.Status.Replicas && d.Status.UnavailableReplicas == 0 {
			lastError = nil
			return true, nil
		}
		lastError = fmt.Errorf("deployment %s is not ready. status: (replicas: %d, updated: %d, ready: %d, unavailable: %d)", d.Name, d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.UnavailableReplicas)
		glog.V(4).Info(lastError)
		return false, nil
	})
	if lastError != nil {
		return lastError
	}
	return err
}

func (optr *Operator) waitForDaemonSetRollout(resource *appsv1.DaemonSet) error {
	var lastError error
	err := wait.Poll(daemonsetRolloutPollInterval, daemonsetRolloutTimeout, func() (bool, error) {
		d, err := optr.daemonsetLister.DaemonSets(resource.Namespace).Get(resource.Name)
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			// Do not return error here, as we could be updating the API Server itself, in which case we
			// want to continue waiting.
			lastError = fmt.Errorf("getting DaemonSet %s during rollout: %v", resource.Name, err)
			glog.Error(lastError)
			return false, nil
		}

		if d.DeletionTimestamp != nil {
			lastError = nil
			return false, fmt.Errorf("daemonset %s is being deleted", resource.Name)
		}

		if d.Generation <= d.Status.ObservedGeneration && d.Status.UpdatedNumberScheduled == d.Status.DesiredNumberScheduled && d.Status.NumberUnavailable == 0 {
			lastError = nil
			return true, nil
		}
		lastError = fmt.Errorf("daemonset %s is not ready. status: (desired: %d, updated: %d, available: %d, unavailable: %d)", d.Name, d.Status.DesiredNumberScheduled, d.Status.UpdatedNumberScheduled, d.Status.NumberAvailable, d.Status.NumberUnavailable)
		glog.V(4).Info(lastError)
		return false, nil
	})
	if lastError != nil {
		return lastError
	}
	return err
}

func newDeployment(config *OperatorConfig, features map[string]bool) *appsv1.Deployment {
	replicas := int32(1)
	template := newPodTemplateSpec(config, features)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-api-controllers",
			Namespace: config.TargetNamespace,
			Annotations: map[string]string{
				maoOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "controller",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"api":     "clusterapi",
					"k8s-app": "controller",
				},
			},
			Template: *template,
		},
	}
}

func newPodTemplateSpec(config *OperatorConfig, features map[string]bool) *corev1.PodTemplateSpec {
	containers := newContainers(config, features)
	tolerations := []corev1.Toleration{
		{
			Key:    "node-role.kubernetes.io/master",
			Effect: corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
		{
			Key:               "node.kubernetes.io/not-ready",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: pointer.Int64Ptr(120),
		},
		{
			Key:               "node.kubernetes.io/unreachable",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: pointer.Int64Ptr(120),
		},
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "controller",
			},
		},
		Spec: corev1.PodSpec{
			Containers:         containers,
			PriorityClassName:  "system-node-critical",
			NodeSelector:       map[string]string{"node-role.kubernetes.io/master": ""},
			ServiceAccountName: "machine-api-controllers",
			Tolerations:        tolerations,
		},
	}
}

func newContainers(config *OperatorConfig, features map[string]bool) []corev1.Container {
	resources := corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceMemory: resource.MustParse("20Mi"),
			corev1.ResourceCPU:    resource.MustParse("10m"),
		},
	}
	args := []string{
		"--logtostderr=true",
		"--v=3",
		fmt.Sprintf("--namespace=%s", config.TargetNamespace),
	}

	containers := []corev1.Container{
		{
			Name:      "machineset-controller",
			Image:     config.Controllers.MachineSet,
			Command:   []string{"/machineset-controller"},
			Args:      args,
			Resources: resources,
		},
		{
			Name:      "machine-controller",
			Image:     config.Controllers.Provider,
			Command:   []string{"/machine-controller-manager"},
			Args:      args,
			Resources: resources,
			Env: []corev1.EnvVar{
				{
					Name: "NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
			},
		},
		{
			Name:      "nodelink-controller",
			Image:     config.Controllers.NodeLink,
			Command:   []string{"/nodelink-controller"},
			Args:      args,
			Resources: resources,
		},
		{
			Name:      "machine-healthcheck-controller",
			Image:     config.Controllers.MachineHealthCheck,
			Command:   []string{"/machine-healthcheck"},
			Args:      args,
			Resources: resources,
		},
	}
	return containers
}

func newTerminationDaemonSet(config *OperatorConfig) *appsv1.DaemonSet {
	template := newTerminationPodTemplateSpec(config)

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineAPITerminationHandler,
			Namespace: config.TargetNamespace,
			Annotations: map[string]string{
				maoOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "termination-handler",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"api":     "clusterapi",
					"k8s-app": "termination-handler",
				},
			},
			Template: *template,
		},
	}
}

func newTerminationPodTemplateSpec(config *OperatorConfig) *corev1.PodTemplateSpec {
	containers := newTerminationContainers(config)

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "termination-handler",
			},
		},
		Spec: corev1.PodSpec{
			Containers:         containers,
			PriorityClassName:  "system-node-critical",
			NodeSelector:       map[string]string{machinecontroller.MachineInterruptibleInstanceLabelName: ""},
			ServiceAccountName: machineAPITerminationHandler,
			HostNetwork:        true,
		},
	}
}

func newTerminationContainers(config *OperatorConfig) []corev1.Container {
	resources := corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceMemory: resource.MustParse("20Mi"),
			corev1.ResourceCPU:    resource.MustParse("10m"),
		},
	}
	terminationArgs := []string{
		"--logtostderr=true",
		"--v=3",
		"--node-name=$(NODE_NAME)",
		fmt.Sprintf("--namespace=%s", config.TargetNamespace),
		"--poll-interval-seconds=5",
	}
	return []corev1.Container{
		{
			Name:      "termination-handler",
			Image:     config.Controllers.TerminationHandler,
			Command:   []string{"/termination-handler"},
			Args:      terminationArgs,
			Resources: resources,
			Env: []corev1.EnvVar{
				{
					Name: "NODE_NAME",
					ValueFrom: &corev1.EnvVarSource{
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "spec.nodeName",
						},
					},
				},
			},
		},
	}
}
