// Code generated by applyconfiguration-gen. DO NOT EDIT.

package v1beta1

import (
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/client-go/applyconfigurations/meta/v1"
)

// MachineSpecApplyConfiguration represents a declarative configuration of the MachineSpec type for use
// with apply.
type MachineSpecApplyConfiguration struct {
	*ObjectMetaApplyConfiguration `json:"metadata,omitempty"`
	LifecycleHooks                *LifecycleHooksApplyConfiguration `json:"lifecycleHooks,omitempty"`
	Taints                        []v1.Taint                        `json:"taints,omitempty"`
	ProviderSpec                  *ProviderSpecApplyConfiguration   `json:"providerSpec,omitempty"`
	ProviderID                    *string                           `json:"providerID,omitempty"`
	AuthoritativeAPI              *machinev1beta1.MachineAuthority  `json:"authoritativeAPI,omitempty"`
}

// MachineSpecApplyConfiguration constructs a declarative configuration of the MachineSpec type for use with
// apply.
func MachineSpec() *MachineSpecApplyConfiguration {
	return &MachineSpecApplyConfiguration{}
}

// WithName sets the Name field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Name field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithName(value string) *MachineSpecApplyConfiguration {
	b.ensureObjectMetaApplyConfigurationExists()
	b.ObjectMetaApplyConfiguration.Name = &value
	return b
}

// WithGenerateName sets the GenerateName field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the GenerateName field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithGenerateName(value string) *MachineSpecApplyConfiguration {
	b.ensureObjectMetaApplyConfigurationExists()
	b.ObjectMetaApplyConfiguration.GenerateName = &value
	return b
}

// WithNamespace sets the Namespace field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the Namespace field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithNamespace(value string) *MachineSpecApplyConfiguration {
	b.ensureObjectMetaApplyConfigurationExists()
	b.ObjectMetaApplyConfiguration.Namespace = &value
	return b
}

// WithLabels puts the entries into the Labels field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, the entries provided by each call will be put on the Labels field,
// overwriting an existing map entries in Labels field with the same key.
func (b *MachineSpecApplyConfiguration) WithLabels(entries map[string]string) *MachineSpecApplyConfiguration {
	b.ensureObjectMetaApplyConfigurationExists()
	if b.ObjectMetaApplyConfiguration.Labels == nil && len(entries) > 0 {
		b.ObjectMetaApplyConfiguration.Labels = make(map[string]string, len(entries))
	}
	for k, v := range entries {
		b.ObjectMetaApplyConfiguration.Labels[k] = v
	}
	return b
}

// WithAnnotations puts the entries into the Annotations field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, the entries provided by each call will be put on the Annotations field,
// overwriting an existing map entries in Annotations field with the same key.
func (b *MachineSpecApplyConfiguration) WithAnnotations(entries map[string]string) *MachineSpecApplyConfiguration {
	b.ensureObjectMetaApplyConfigurationExists()
	if b.ObjectMetaApplyConfiguration.Annotations == nil && len(entries) > 0 {
		b.ObjectMetaApplyConfiguration.Annotations = make(map[string]string, len(entries))
	}
	for k, v := range entries {
		b.ObjectMetaApplyConfiguration.Annotations[k] = v
	}
	return b
}

// WithOwnerReferences adds the given value to the OwnerReferences field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the OwnerReferences field.
func (b *MachineSpecApplyConfiguration) WithOwnerReferences(values ...*metav1.OwnerReferenceApplyConfiguration) *MachineSpecApplyConfiguration {
	b.ensureObjectMetaApplyConfigurationExists()
	for i := range values {
		if values[i] == nil {
			panic("nil value passed to WithOwnerReferences")
		}
		b.ObjectMetaApplyConfiguration.OwnerReferences = append(b.ObjectMetaApplyConfiguration.OwnerReferences, *values[i])
	}
	return b
}

func (b *MachineSpecApplyConfiguration) ensureObjectMetaApplyConfigurationExists() {
	if b.ObjectMetaApplyConfiguration == nil {
		b.ObjectMetaApplyConfiguration = &ObjectMetaApplyConfiguration{}
	}
}

// WithLifecycleHooks sets the LifecycleHooks field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the LifecycleHooks field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithLifecycleHooks(value *LifecycleHooksApplyConfiguration) *MachineSpecApplyConfiguration {
	b.LifecycleHooks = value
	return b
}

// WithTaints adds the given value to the Taints field in the declarative configuration
// and returns the receiver, so that objects can be build by chaining "With" function invocations.
// If called multiple times, values provided by each call will be appended to the Taints field.
func (b *MachineSpecApplyConfiguration) WithTaints(values ...v1.Taint) *MachineSpecApplyConfiguration {
	for i := range values {
		b.Taints = append(b.Taints, values[i])
	}
	return b
}

// WithProviderSpec sets the ProviderSpec field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ProviderSpec field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithProviderSpec(value *ProviderSpecApplyConfiguration) *MachineSpecApplyConfiguration {
	b.ProviderSpec = value
	return b
}

// WithProviderID sets the ProviderID field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the ProviderID field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithProviderID(value string) *MachineSpecApplyConfiguration {
	b.ProviderID = &value
	return b
}

// WithAuthoritativeAPI sets the AuthoritativeAPI field in the declarative configuration to the given value
// and returns the receiver, so that objects can be built by chaining "With" function invocations.
// If called multiple times, the AuthoritativeAPI field is set to the value of the last call.
func (b *MachineSpecApplyConfiguration) WithAuthoritativeAPI(value machinev1beta1.MachineAuthority) *MachineSpecApplyConfiguration {
	b.AuthoritativeAPI = &value
	return b
}
