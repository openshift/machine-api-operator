package framework

import (
	"context"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	osconfigv1 "github.com/openshift/api/config/v1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/operator"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// KubeletKillerPodName contains the name of the pod that stops kubelet process
	KubeletKillerPodName = "kubelet-killer"
	// MachineHealthCheckName contains the name of the machinehealthcheck used for tests
	MachineHealthCheckName = "workers-check"
)

// CreateUnhealthyConditionsConfigMap creates node-unhealthy-conditions configmap with relevant conditions
func CreateUnhealthyConditionsConfigMap(unhealthyConditions *conditions.UnhealthyConditions) error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: TestContext.MachineApiNamespace,
			Name:      healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		},
	}

	conditionsData, err := yaml.Marshal(unhealthyConditions)
	if err != nil {
		return err
	}

	cm.Data = map[string]string{"conditions": string(conditionsData)}
	return client.Create(context.TODO(), cm)
}

// DeleteUnhealthyConditionsConfigMap deletes node-unhealthy-conditions configmap
func DeleteUnhealthyConditionsConfigMap() error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	key := types.NamespacedName{
		Name:      healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions,
		Namespace: TestContext.MachineApiNamespace,
	}
	cm := &corev1.ConfigMap{}
	err = client.Get(context.TODO(), key, cm)
	if err != nil {
		return err
	}

	return client.Delete(context.TODO(), cm)
}

// CreateMachineHealthCheck will create MachineHealthCheck CR with the relevant selector
func CreateMachineHealthCheck(labels map[string]string) error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	mhc := &healthcheckingv1alpha1.MachineHealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      MachineHealthCheckName,
			Namespace: TestContext.MachineApiNamespace,
		},
		Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: labels,
			},
		},
	}
	return client.Create(context.TODO(), mhc)
}

// StopKubelet creates pod in the node PID namespace that stops kubelet process
func StopKubelet(nodeName string) error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	_true := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      KubeletKillerPodName + rand.String(5),
			Namespace: TestContext.MachineApiNamespace,
			Labels: map[string]string{
				KubeletKillerPodName: "",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    KubeletKillerPodName,
					Image:   "busybox",
					Command: []string{"pkill", "-STOP", "hyperkube"},
					SecurityContext: &corev1.SecurityContext{
						Privileged: &_true,
					},
				},
			},
			NodeName: nodeName,
			HostPID:  true,
		},
	}
	return client.Create(context.TODO(), pod)
}

// NewMachineDisruptionBudget returns new MachineDisruptionObject with specified parameters
func NewMachineDisruptionBudget(name string, machineLabels map[string]string, minAvailable *int32, maxUnavailable *int32) *healthcheckingv1alpha1.MachineDisruptionBudget {
	selector := &metav1.LabelSelector{MatchLabels: machineLabels}
	return &healthcheckingv1alpha1.MachineDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: TestContext.MachineApiNamespace,
		},
		Spec: healthcheckingv1alpha1.MachineDisruptionBudgetSpec{
			MinAvailable:   minAvailable,
			MaxUnavailable: maxUnavailable,
			Selector:       selector,
		},
	}
}

// DeleteMachineHealthCheck deletes machine health check by name
func DeleteMachineHealthCheck(healthcheckName string) error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	key := types.NamespacedName{
		Name:      healthcheckName,
		Namespace: TestContext.MachineApiNamespace,
	}
	healthcheck := &healthcheckingv1alpha1.MachineHealthCheck{}
	err = client.Get(context.TODO(), key, healthcheck)
	if err != nil {
		return err
	}

	glog.V(2).Infof("Delete machine health check %s", healthcheck.Name)
	err = client.Delete(context.TODO(), healthcheck)
	return err
}

// DeleteKubeletKillerPods deletes kubelet killer pod
func DeleteKubeletKillerPods() error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	listOptions := runtimeclient.ListOptions{
		Namespace: TestContext.MachineApiNamespace,
	}
	listOptions.MatchingLabels(map[string]string{KubeletKillerPodName: ""})
	podList := &corev1.PodList{}
	err = client.List(context.TODO(), &listOptions, podList)
	if err != nil {
		return err
	}

	for _, pod := range podList.Items {
		glog.V(2).Infof("Delete kubelet killer pod %s", pod.Name)
		err = client.Delete(context.TODO(), &pod)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateOrUpdateTechPreviewFeatureGate creates or updates if it already exists the cluster feature gate with tech preview features
func CreateOrUpdateTechPreviewFeatureGate() error {
	client, err := LoadClient()
	if err != nil {
		return err
	}

	fg := &osconfigv1.FeatureGate{}
	key := types.NamespacedName{
		Name:      operator.MachineAPIFeatureGateName,
		Namespace: TestContext.MachineApiNamespace,
	}
	err = client.Get(context.TODO(), key, fg)
	if err != nil {
		if apierrors.IsNotFound(err) {
			fg.Name = operator.MachineAPIFeatureGateName
			fg.Namespace = TestContext.MachineApiNamespace
			fg.Spec = osconfigv1.FeatureGateSpec{FeatureSet: osconfigv1.TechPreviewNoUpgrade}
			return client.Create(context.TODO(), fg)
		}
		return err
	}
	fg.Spec = osconfigv1.FeatureGateSpec{FeatureSet: osconfigv1.TechPreviewNoUpgrade}
	return client.Update(context.TODO(), fg)
}
