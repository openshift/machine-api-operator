package webhooks

import (
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const (
	DefaultMachineMutatingHookPath      = "/mutate-machine-openshift-io-v1beta1-machine"
	DefaultMachineValidatingHookPath    = "/validate-machine-openshift-io-v1beta1-machine"
	DefaultMachineSetMutatingHookPath   = "/mutate-machine-openshift-io-v1beta1-machineset"
	DefaultMachineSetValidatingHookPath = "/validate-machine-openshift-io-v1beta1-machineset"

	defaultWebhookConfigurationName = "machine-api"
	defaultWebhookServiceName       = "machine-api-operator-webhook"
	defaultWebhookServiceNamespace  = "openshift-machine-api"
	defaultWebhookServicePort       = 443
)

var (
	// webhookFailurePolicy is ignore so we don't want to block machine lifecycle on the webhook operational aspects.
	// This would be particularly problematic for chicken egg issues when bootstrapping a cluster.
	webhookFailurePolicy = admissionregistrationv1.Ignore
	webhookSideEffects   = admissionregistrationv1.SideEffectClassNone
)

// NewValidatingWebhookConfiguration creates a validation webhook configuration with configured Machine and MachineSet webhooks
func NewValidatingWebhookConfiguration() *admissionregistrationv1.ValidatingWebhookConfiguration {
	validatingWebhookConfiguration := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultWebhookConfigurationName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			MachineValidatingWebhook(),
			MachineSetValidatingWebhook(),
		},
	}

	// Setting group version is required for testEnv to create unstructured objects, as the new structure sets it on empty strings
	// Usual way to populate those values, is to create the resource in the cluster first, which we can't yet do.
	validatingWebhookConfiguration.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("ValidatingWebhookConfiguration"))
	return validatingWebhookConfiguration
}

// MachineValidatingWebhook returns validating webhooks for machine to populate the configuration
func MachineValidatingWebhook() admissionregistrationv1.ValidatingWebhook {
	serviceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineValidatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "validation.machine.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &serviceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machines"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// MachineSetValidatingWebhook returns validating webhooks for machineSet to populate the configuration
func MachineSetValidatingWebhook() admissionregistrationv1.ValidatingWebhook {
	machinesetServiceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineSetValidatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "validation.machineset.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &machinesetServiceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machinesets"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// NewMutatingWebhookConfiguration creates a mutating webhook configuration with configured Machine and MachineSet webhooks
func NewMutatingWebhookConfiguration() *admissionregistrationv1.MutatingWebhookConfiguration {
	mutatingWebhookConfiguration := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultWebhookConfigurationName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			MachineMutatingWebhook(),
			MachineSetMutatingWebhook(),
		},
	}

	// Setting group version is required for testEnv to create unstructured objects, as the new structure sets it on empty strings
	// Usual way to populate those values, is to create the resource in the cluster first, which we can't yet do.
	mutatingWebhookConfiguration.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("MutatingWebhookConfiguration"))
	return mutatingWebhookConfiguration
}

// MachineMutatingWebhook returns mutating webhooks for machine to apply in configuration
func MachineMutatingWebhook() admissionregistrationv1.MutatingWebhook {
	machineServiceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineMutatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.MutatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "default.machine.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &machineServiceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machines"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
			},
		},
	}
}

// MachineSetMutatingWebhook returns mutating webhook for machineSet to apply in configuration
func MachineSetMutatingWebhook() admissionregistrationv1.MutatingWebhook {
	machineSetServiceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineSetMutatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.MutatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "default.machineset.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &machineSetServiceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machinesets"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
			},
		},
	}
}
