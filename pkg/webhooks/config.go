package webhooks

import (
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
)

const (
	DefaultMachineMutatingHookPath                     = "/mutate-machine-openshift-io-v1beta1-machine"
	DefaultMachineValidatingHookPath                   = "/validate-machine-openshift-io-v1beta1-machine"
	DefaultMachineSetMutatingHookPath                  = "/mutate-machine-openshift-io-v1beta1-machineset"
	DefaultMachineSetValidatingHookPath                = "/validate-machine-openshift-io-v1beta1-machineset"
	DefaultMetal3RemediationMutatingHookPath           = "/mutate-infrastructure-cluster-x-k8s-io-v1beta1-metal3remediation"
	DefaultMetal3RemediationValidatingHookPath         = "/validate-infrastructure-cluster-x-k8s-io-v1beta1-metal3remediation"
	DefaultMetal3RemediationTemplateMutatingHookPath   = "/mutate-infrastructure-cluster-x-k8s-io-v1beta1-metal3remediationtemplate"
	DefaultMetal3RemediationTemplateValidatingHookPath = "/validate-infrastructure-cluster-x-k8s-io-v1beta1-metal3remediationtemplate"

	machineWebhookConfigurationName           = "machine-api"
	metal3RemediationWebhookConfigurationName = "machine-api-metal3-remediation"
	defaultWebhookServiceNamespace            = "openshift-machine-api"
	defaultWebhookServicePort                 = 443
	// Name and port of the webhook service pointing to the machineset-controller container
	defaultWebhookServiceName = "machine-api-operator-webhook"
	// Name and port of the webhook service pointing to the machine-controller / provider container
	// Used by metal3 remediation webhooks in the CAPBM image
	defaultProviderWebhookServiceName = "machine-api-operator-machine-webhook"
)

var (
	// capm3apisGroupVersion is the group version for the capm3 API group.
	// Copy this here to avoid a dependency on the capm3 repo.
	capm3apisGroupVersion = schema.GroupVersion{Group: "infrastructure.cluster.x-k8s.io", Version: "v1beta1"}
)

var (
	// webhookFailurePolicy is ignore so we don't want to block machine lifecycle on the webhook operational aspects.
	// This would be particularly problematic for chicken egg issues when bootstrapping a cluster.
	webhookFailurePolicy = admissionregistrationv1.Ignore
	webhookSideEffects   = admissionregistrationv1.SideEffectClassNone
)

// NewMachineValidatingWebhookConfiguration creates a validation webhook configuration with configured Machine and MachineSet webhooks
func NewMachineValidatingWebhookConfiguration() *admissionregistrationv1.ValidatingWebhookConfiguration {
	validatingWebhookConfiguration := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: machineWebhookConfigurationName,
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
		Path:      ptr.To[string](DefaultMachineValidatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
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
		Path:      ptr.To[string](DefaultMachineSetValidatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
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

// NewMetal3RemediationValidatingWebhookConfiguration creates a validation webhook configuration with configured
// metal3remediation(template) webhooks. Metal3Remediation(Templates) were backported from metal3, their CRDs and the
// actual webhook implementation can be found in cluster-api-provider-baremetal
func NewMetal3RemediationValidatingWebhookConfiguration() *admissionregistrationv1.ValidatingWebhookConfiguration {
	validatingWebhookConfiguration := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: metal3RemediationWebhookConfigurationName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			Metal3RemediationValidatingWebhook(),
			Metal3RemediationTemplateValidatingWebhook(),
		},
	}

	// Setting group version is required for testEnv to create unstructured objects, as the new structure sets it on empty strings
	// Usual way to populate those values, is to create the resource in the cluster first, which we can't yet do.
	validatingWebhookConfiguration.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("ValidatingWebhookConfiguration"))
	return validatingWebhookConfiguration
}

// Metal3RemediationValidatingWebhook returns validating webhooks for metal3remediation to populate the configuration
func Metal3RemediationValidatingWebhook() admissionregistrationv1.ValidatingWebhook {
	serviceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultProviderWebhookServiceName,
		Path:      ptr.To[string](DefaultMetal3RemediationValidatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
	}
	return admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "validation.metal3remediation.k8s.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &serviceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{capm3apisGroupVersion.Group},
					APIVersions: []string{capm3apisGroupVersion.Version},
					Resources:   []string{"metal3remediations"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// Metal3RemediationTemplateValidatingWebhook returns validating webhooks for metal3remediationtemplate to populate the configuration
func Metal3RemediationTemplateValidatingWebhook() admissionregistrationv1.ValidatingWebhook {
	serviceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultProviderWebhookServiceName,
		Path:      ptr.To[string](DefaultMetal3RemediationTemplateValidatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
	}
	return admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "validation.metal3remediationtemplate.k8s.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &serviceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{capm3apisGroupVersion.Group},
					APIVersions: []string{capm3apisGroupVersion.Version},
					Resources:   []string{"metal3remediationtemplates"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// NewMachineMutatingWebhookConfiguration creates a mutating webhook configuration with configured Machine and MachineSet webhooks
func NewMachineMutatingWebhookConfiguration() *admissionregistrationv1.MutatingWebhookConfiguration {
	mutatingWebhookConfiguration := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: machineWebhookConfigurationName,
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
		Path:      ptr.To[string](DefaultMachineMutatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
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
		Path:      ptr.To[string](DefaultMachineSetMutatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
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

// NewMetal3RemediationMutatingWebhookConfiguration creates a mutating webhook configuration with configured
// metal3remediation(template) webhooks. Metal3Remediation(Templates) were backported from metal3, their CRDs and the
// actual webhook implementation can be found in cluster-api-provider-baremetal
func NewMetal3RemediationMutatingWebhookConfiguration() *admissionregistrationv1.MutatingWebhookConfiguration {
	mutatingWebhookConfiguration := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: metal3RemediationWebhookConfigurationName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			Metal3RemediationMutatingWebhook(),
			Metal3RemediationTemplateMutatingWebhook(),
		},
	}

	// Setting group version is required for testEnv to create unstructured objects, as the new structure sets it on empty strings
	// Usual way to populate those values, is to create the resource in the cluster first, which we can't yet do.
	mutatingWebhookConfiguration.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("MutatingWebhookConfiguration"))
	return mutatingWebhookConfiguration
}

// Metal3RemediationMutatingWebhook returns mutating webhook for metal3remediation to apply in configuration
func Metal3RemediationMutatingWebhook() admissionregistrationv1.MutatingWebhook {
	serviceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultProviderWebhookServiceName,
		Path:      ptr.To[string](DefaultMetal3RemediationMutatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
	}
	return admissionregistrationv1.MutatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "default.metal3remediation.k8s.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &serviceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{capm3apisGroupVersion.Group},
					APIVersions: []string{capm3apisGroupVersion.Version},
					Resources:   []string{"metal3remediations"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// Metal3RemediationTemplateMutatingWebhook returns mutating webhook for metal3remediationtemplate to apply in configuration
func Metal3RemediationTemplateMutatingWebhook() admissionregistrationv1.MutatingWebhook {
	serviceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultProviderWebhookServiceName,
		Path:      ptr.To[string](DefaultMetal3RemediationTemplateMutatingHookPath),
		Port:      ptr.To[int32](defaultWebhookServicePort),
	}
	return admissionregistrationv1.MutatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "default.metal3remediationtemplate.k8s.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &serviceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{capm3apisGroupVersion.Group},
					APIVersions: []string{capm3apisGroupVersion.Version},
					Resources:   []string{"metal3remediationtemplates"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}
