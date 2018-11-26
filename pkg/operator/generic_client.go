package operator

import (
	"k8s.io/client-go/tools/cache"

	operatorv1 "github.com/openshift/api/operator/v1"
	operatorconfigclientv1alpha1 "github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned/typed/machineapi/v1alpha1"
	operatorclientinformers "github.com/openshift/machine-api-operator/pkg/generated/informers/externalversions"
)

type operatorClient struct {
	informers operatorclientinformers.SharedInformerFactory
	client    operatorconfigclientv1alpha1.MachineapiV1alpha1Interface
}

func (p *operatorClient) Informer() cache.SharedIndexInformer {
	return p.informers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Informer()
}

func (p *operatorClient) CurrentStatus() (operatorv1.OperatorStatus, error) {
	instance, err := p.informers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Lister().Get("instance")
	if err != nil {
		return operatorv1.OperatorStatus{}, err
	}

	return instance.Status.OperatorStatus, nil
}

func (c *operatorClient) GetOperatorState() (*operatorv1.OperatorSpec, *operatorv1.OperatorStatus, string, error) {
	instance, err := c.informers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Lister().Get("instance")
	if err != nil {
		return nil, nil, "", err
	}

	return &instance.Spec.OperatorSpec, &instance.Status.OperatorStatus, instance.ResourceVersion, nil
}

func (c *operatorClient) UpdateOperatorSpec(resourceVersion string, spec *operatorv1.OperatorSpec) (*operatorv1.OperatorSpec, string, error) {
	original, err := c.informers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Lister().Get("instance")
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Spec.OperatorSpec = *spec

	ret, err := c.client.MachineAPIOperatorConfigs().Update(copy)
	if err != nil {
		return nil, "", err
	}

	return &ret.Spec.OperatorSpec, ret.ResourceVersion, nil
}
func (c *operatorClient) UpdateOperatorStatus(resourceVersion string, status *operatorv1.OperatorStatus) (*operatorv1.OperatorStatus, string, error) {
	original, err := c.informers.Machineapi().V1alpha1().MachineAPIOperatorConfigs().Lister().Get("instance")
	if err != nil {
		return nil, "", err
	}
	copy := original.DeepCopy()
	copy.ResourceVersion = resourceVersion
	copy.Status.OperatorStatus = *status

	ret, err := c.client.MachineAPIOperatorConfigs().UpdateStatus(copy)
	if err != nil {
		return nil, "", err
	}

	return &ret.Status.OperatorStatus, ret.ResourceVersion, nil
}
