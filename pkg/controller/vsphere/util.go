package vsphere

import (
	"context"
	"fmt"

	"gopkg.in/gcfg.v1"

	configv1 "github.com/openshift/api/config/v1"
	vspherev1 "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	globalInfrastuctureName  = "cluster"
	openshiftConfigNamespace = "openshift-config"
)

// vSphereConfig is a copy of the Kubernetes vSphere cloud provider config type
// that contains the fields we need.  Unfortunately, we can't easily import
// either the legacy or newer cloud provider code here, so we're just
// duplicating part of the type and parsing it ourselves using the same gcfg
// library for now.
type vSphereConfig struct {
	Labels struct {
		Zone   string `gcfg:"zone"`
		Region string `gcfg:"region"`
	}
}

func getInfrastructure(c runtimeclient.Reader) (*configv1.Infrastructure, error) {
	if c == nil {
		return nil, errors.New("no API reader -- will not fetch infrastructure config")
	}

	infra := &configv1.Infrastructure{}
	infraName := runtimeclient.ObjectKey{Name: globalInfrastuctureName}

	if err := c.Get(context.Background(), infraName, infra); err != nil {
		return nil, err
	}

	return infra, nil
}

func getVSphereConfig(c runtimeclient.Reader) (*vSphereConfig, error) {
	if c == nil {
		return nil, errors.New("no API reader -- will not fetch vSphere config")
	}

	infra, err := getInfrastructure(c)
	if err != nil {
		return nil, err
	}

	if infra.Spec.CloudConfig.Name == "" {
		return nil, fmt.Errorf("cluster infrastructure CloudConfig has empty name")
	}

	if infra.Spec.CloudConfig.Key == "" {
		return nil, fmt.Errorf("cluster infrastructure CloudConfig has empty key")
	}

	cm := &corev1.ConfigMap{}
	cmName := runtimeclient.ObjectKey{
		Name:      infra.Spec.CloudConfig.Name,
		Namespace: openshiftConfigNamespace,
	}

	if err := c.Get(context.Background(), cmName, cm); err != nil {
		return nil, err
	}

	cloudConfig, found := cm.Data[infra.Spec.CloudConfig.Key]
	if !found {
		return nil, fmt.Errorf("cloud-config ConfigMap has no %q key",
			infra.Spec.CloudConfig.Key,
		)
	}

	var vcfg vSphereConfig

	if err := gcfg.FatalOnly(gcfg.ReadStringInto(&vcfg, cloudConfig)); err != nil {
		return nil, err
	}

	return &vcfg, nil
}

func setVSphereMachineProviderConditions(condition vspherev1.VSphereMachineProviderCondition, conditions []vspherev1.VSphereMachineProviderCondition) []vspherev1.VSphereMachineProviderCondition {
	now := metav1.Now()

	if existingCondition := findProviderCondition(conditions, condition.Type); existingCondition == nil {
		condition.LastProbeTime = now
		condition.LastTransitionTime = now
		conditions = append(conditions, condition)
	} else {
		updateExistingCondition(&condition, existingCondition)
	}

	return conditions
}

func findProviderCondition(conditions []vspherev1.VSphereMachineProviderCondition, conditionType vspherev1.VSphereMachineProviderConditionType) *vspherev1.VSphereMachineProviderCondition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func updateExistingCondition(newCondition, existingCondition *vspherev1.VSphereMachineProviderCondition) {
	if !shouldUpdateCondition(newCondition, existingCondition) {
		return
	}

	if existingCondition.Status != newCondition.Status {
		existingCondition.LastTransitionTime = metav1.Now()
	}
	existingCondition.Status = newCondition.Status
	existingCondition.Reason = newCondition.Reason
	existingCondition.Message = newCondition.Message
	existingCondition.LastProbeTime = newCondition.LastProbeTime
}

func shouldUpdateCondition(newCondition, existingCondition *vspherev1.VSphereMachineProviderCondition) bool {
	return newCondition.Reason != existingCondition.Reason || newCondition.Message != existingCondition.Message
}

func conditionSuccess() vspherev1.VSphereMachineProviderCondition {
	return vspherev1.VSphereMachineProviderCondition{
		Type:    vspherev1.MachineCreation,
		Status:  corev1.ConditionTrue,
		Reason:  vspherev1.MachineCreationSucceeded,
		Message: "Machine successfully created",
	}
}

func conditionFailed() vspherev1.VSphereMachineProviderCondition {
	return vspherev1.VSphereMachineProviderCondition{
		Type:   vspherev1.MachineCreation,
		Status: corev1.ConditionFalse,
		Reason: vspherev1.MachineCreationFailed,
	}
}
