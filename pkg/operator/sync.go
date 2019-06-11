package operator

import (
	"fmt"
	"time"

	"github.com/golang/glog"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"

	osev1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-version-operator/lib/resourceapply"
)

const (
	deploymentRolloutPollInterval = time.Second
	deploymentRolloutTimeout      = 5 * time.Minute
)

func (optr *Operator) syncAll(config *OperatorConfig) error {
	if err := optr.statusProgressing(); err != nil {
		glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}
	if config.Controllers.Provider != clusterAPIControllerNoOp {
		if err := optr.syncClusterAPIController(config); err != nil {
			if err := optr.statusDegraded(err.Error()); err != nil {
				// Just log the error here.  We still want to
				// return the outer error.
				glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
			}
			glog.Errorf("Error syncing cluster api controller: %v", err)
			return err
		}
		glog.V(3).Info("Synced up all components")
	}

	if err := optr.statusAvailable(); err != nil {
		glog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		return fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}

	return nil
}

func (optr *Operator) syncClusterAPIController(config *OperatorConfig) error {
	// Fetch the Feature
	featureGate, err := optr.featureGateLister.Get(MachineAPIFeatureGateName)

	var featureSet osev1.FeatureSet
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return err
		}
		glog.V(2).Infof("Failed to find feature gate %q, will use default feature set", MachineAPIFeatureGateName)
		featureSet = osev1.Default
	} else {
		featureSet = featureGate.Spec.FeatureSet
	}

	features, err := generateFeatureMap(featureSet)
	if err != nil {
		return err
	}

	controller := newDeployment(config, features)
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
		d, err := optr.deployLister.Deployments(resource.Namespace).Get(resource.Name)
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		if err != nil {
			// Do not return error here, as we could be updating the API Server itself, in which case we
			// want to continue waiting.
			glog.Errorf("Error getting Deployment %q during rollout: %v", resource.Name, err)
			return false, nil
		}

		if d.DeletionTimestamp != nil {
			return false, fmt.Errorf("deployment %q is being deleted", resource.Name)
		}

		if d.Generation <= d.Status.ObservedGeneration && d.Status.UpdatedReplicas == d.Status.Replicas && d.Status.UnavailableReplicas == 0 {
			return true, nil
		}
		glog.V(4).Infof("Deployment %q is not ready. status: (replicas: %d, updated: %d, ready: %d, unavailable: %d)", d.Name, d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.UnavailableReplicas)
		return false, nil
	})
}

func newDeployment(config *OperatorConfig, features map[string]bool) *appsv1.Deployment {
	replicas := int32(1)
	template := newPodTemplateSpec(config, features)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-api-controllers",
			Namespace: config.TargetNamespace,
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
			Containers:        containers,
			PriorityClassName: "system-node-critical",
			NodeSelector:      map[string]string{"node-role.kubernetes.io/master": ""},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: pointer.BoolPtr(true),
				RunAsUser:    pointer.Int64Ptr(65534),
			},
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
			Name:      "controller-manager",
			Image:     config.Controllers.Provider,
			Command:   []string{"/manager"},
			Args:      args,
			Resources: resources,
		},
		{
			Name:    "machine-controller",
			Image:   config.Controllers.Provider,
			Command: []string{"/machine-controller-manager"},
			Args:    args,
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
	}
	// add machine-health-check controller container if it exists and enabled under feature gates
	if enabled, ok := features[FeatureGateMachineHealthCheck]; ok && enabled {
		containers = append(containers, corev1.Container{
			Name:      "machine-healthcheck-controller",
			Image:     config.Controllers.MachineHealthCheck,
			Command:   []string{"/machine-healthcheck"},
			Args:      args,
			Resources: resources,
		})
	}
	return containers
}
