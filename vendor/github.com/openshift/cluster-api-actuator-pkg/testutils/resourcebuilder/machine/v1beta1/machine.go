/*
Copyright 2022 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Machine creates a new machine builder.
func Machine() MachineBuilder {
	return MachineBuilder{}
}

// MachineBuilder is used to build out a machine object.
type MachineBuilder struct {
	authoritativeAPI      machinev1beta1.MachineAuthority
	annotations           map[string]string
	creationTimestamp     metav1.Time
	deletionTimestamp     *metav1.Time
	generateName          string
	labels                map[string]string
	lifecycleHooks        machinev1beta1.LifecycleHooks
	machineSpec           *machinev1beta1.MachineSpec
	machineSpecObjectMeta machinev1beta1.ObjectMeta
	name                  string
	namespace             string
	ownerReferences       []metav1.OwnerReference
	providerID            **string
	providerSpecBuilder   *resourcebuilder.RawExtensionBuilder
	providerSpec          *machinev1beta1.ProviderSpec
	taints                []corev1.Taint

	// status fields
	addresses              []corev1.NodeAddress
	authoritativeAPIStatus machinev1beta1.MachineAuthority
	conditions             []machinev1beta1.Condition
	errorMessage           *string
	errorReason            *machinev1beta1.MachineStatusError
	lastOperation          *machinev1beta1.LastOperation
	lastUpdated            *metav1.Time
	nodeRef                *corev1.ObjectReference
	phase                  *string
	providerStatus         *runtime.RawExtension
}

// Build builds a new machine based on the configuration provided.
func (m MachineBuilder) Build() *machinev1beta1.Machine {
	machine := &machinev1beta1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Annotations:       m.annotations,
			CreationTimestamp: m.creationTimestamp,
			DeletionTimestamp: m.deletionTimestamp,
			GenerateName:      m.generateName,
			Labels:            m.labels,
			Name:              m.name,
			Namespace:         m.namespace,
			OwnerReferences:   m.ownerReferences,
		},
		Spec: coalesceMachineSpec(m.machineSpec, machinev1beta1.MachineSpec{
			AuthoritativeAPI: m.authoritativeAPI,
			LifecycleHooks:   m.lifecycleHooks,
			ObjectMeta:       m.machineSpecObjectMeta,
			ProviderID:       resourcebuilder.Coalesce(m.providerID, nil),
			ProviderSpec: resourcebuilder.Coalesce(m.providerSpec, machinev1beta1.ProviderSpec{
				Value: coalesceProviderSpecValue(m.providerSpecBuilder),
			}),
			Taints: m.taints,
		}),
		Status: machinev1beta1.MachineStatus{
			AuthoritativeAPI: m.authoritativeAPIStatus,
			Addresses:        m.addresses,
			Conditions:       m.conditions,
			ErrorMessage:     m.errorMessage,
			ErrorReason:      m.errorReason,
			LastOperation:    m.lastOperation,
			LastUpdated:      m.lastUpdated,
			NodeRef:          m.nodeRef,
			Phase:            m.phase,
			ProviderStatus:   m.providerStatus,
		},
	}

	m.WithLabel(machinev1beta1.MachineClusterIDLabel, resourcebuilder.TestClusterIDValue)

	return machine
}

// AsWorker sets the worker role and type on the machine labels for the machine builder.
func (m MachineBuilder) AsWorker() MachineBuilder {
	return m.
		WithLabel(resourcebuilder.MachineRoleLabelName, "worker").
		WithLabel(resourcebuilder.MachineTypeLabelName, "worker")
}

// AsMaster sets the master role and type on the machine labels for the machine builder.
func (m MachineBuilder) AsMaster() MachineBuilder {
	return m.
		WithLabel(resourcebuilder.MachineRoleLabelName, "master").
		WithLabel(resourcebuilder.MachineTypeLabelName, "master")
}

// WithAnnotations sets the annotations for the machine builder.
func (m MachineBuilder) WithAnnotations(annotations map[string]string) MachineBuilder {
	m.annotations = annotations
	return m
}

// WithAuthoritativeAPI sets the authoritativeAPI for the machine builder.
func (m MachineBuilder) WithAuthoritativeAPI(authority machinev1beta1.MachineAuthority) MachineBuilder {
	m.authoritativeAPI = authority
	return m
}

// WithCreationTimestamp sets the creationTimestamp for the machine builder.
func (m MachineBuilder) WithCreationTimestamp(time metav1.Time) MachineBuilder {
	m.creationTimestamp = time
	return m
}

// WithDeletionTimestamp sets the deletionTimestamp for the machine builder.
// Note: This can only be used in unit testing as the API server will drop this
// field if a create/update request tries to set it.
func (m MachineBuilder) WithDeletionTimestamp(time *metav1.Time) MachineBuilder {
	m.deletionTimestamp = time
	return m
}

// WithGenerateName sets the generateName for the machine builder.
func (m MachineBuilder) WithGenerateName(generateName string) MachineBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the machine builder.
func (m MachineBuilder) WithLabel(key, value string) MachineBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the machine builder.
func (m MachineBuilder) WithLabels(labels map[string]string) MachineBuilder {
	m.labels = labels
	return m
}

// WithLifecycleHooks sets the lifecycleHooks for the machine builder.
func (m MachineBuilder) WithLifecycleHooks(lh machinev1beta1.LifecycleHooks) MachineBuilder {
	m.lifecycleHooks = lh
	return m
}

// WithMachineSpec sets the MachineSpec field for the machine builder.
func (m MachineBuilder) WithMachineSpec(machineSpec machinev1beta1.MachineSpec) MachineBuilder {
	m.machineSpec = &machineSpec
	return m
}

// WithMachineSpecObjectMeta sets the ObjectMeta on the machine spec field for the machine builder.
func (m MachineBuilder) WithMachineSpecObjectMeta(machineSpecObjectMeta machinev1beta1.ObjectMeta) MachineBuilder {
	m.machineSpecObjectMeta = machineSpecObjectMeta
	return m
}

// WithName sets the name for the machine builder.
func (m MachineBuilder) WithName(name string) MachineBuilder {
	m.name = name
	return m
}

// WithNamespace sets the namespace for the machine builder.
func (m MachineBuilder) WithNamespace(namespace string) MachineBuilder {
	m.namespace = namespace
	return m
}

// WithOwnerReferences sets the OwnerReferences for the machine builder.
func (m MachineBuilder) WithOwnerReferences(ownerRefs []metav1.OwnerReference) MachineBuilder {
	m.ownerReferences = ownerRefs
	return m
}

// WithProviderID sets the providerID builder for the machine builder.
func (m MachineBuilder) WithProviderID(id *string) MachineBuilder {
	m.providerID = &id
	return m
}

// WithProviderSpec sets the ProviderSpec field for the machine builder.
func (m MachineBuilder) WithProviderSpec(providerSpec machinev1beta1.ProviderSpec) MachineBuilder {
	m.providerSpec = &providerSpec
	return m
}

// WithProviderSpecBuilder sets the providerSpec builder for the machine builder.
func (m MachineBuilder) WithProviderSpecBuilder(builder resourcebuilder.RawExtensionBuilder) MachineBuilder {
	m.providerSpecBuilder = &builder
	return m
}

// WithTaints sets the taints field for the machine builder.
func (m MachineBuilder) WithTaints(taints []corev1.Taint) MachineBuilder {
	m.taints = taints
	return m
}

// Status Fields

// WithAddresses sets the addresses status field for the machine builder.
func (m MachineBuilder) WithAddresses(addrs []corev1.NodeAddress) MachineBuilder {
	m.addresses = addrs
	return m
}

// WithAuthoritativeAPIStatus sets the authoritativeAPIStatus for the machine builder.
func (m MachineBuilder) WithAuthoritativeAPIStatus(authority machinev1beta1.MachineAuthority) MachineBuilder {
	m.authoritativeAPIStatus = authority
	return m
}

// WithConditions sets the conditions status field for the machine builder.
func (m MachineBuilder) WithConditions(c []machinev1beta1.Condition) MachineBuilder {
	m.conditions = c
	return m
}

// WithErrorMessage sets the error message status field for the machine builder.
func (m MachineBuilder) WithErrorMessage(errorMsg string) MachineBuilder {
	m.errorMessage = &errorMsg
	return m
}

// WithErrorReason sets the error reason status field for the machine builder.
func (m MachineBuilder) WithErrorReason(errorReason machinev1beta1.MachineStatusError) MachineBuilder {
	m.errorReason = &errorReason
	return m
}

// WithLastOperation sets the lastOperation for the machine builder.
func (m MachineBuilder) WithLastOperation(l machinev1beta1.LastOperation) MachineBuilder {
	m.lastOperation = &l
	return m
}

// WithLastUpdated sets the lastUpdated for the machine builder.
func (m MachineBuilder) WithLastUpdated(l metav1.Time) MachineBuilder {
	m.lastUpdated = &l
	return m
}

// WithPhase sets the phase status field for the machine builder.
func (m MachineBuilder) WithPhase(phase string) MachineBuilder {
	m.phase = &phase
	return m
}

// WithProviderStatus sets the providerStatus builder for the machine builder.
func (m MachineBuilder) WithProviderStatus(ps runtime.RawExtension) MachineBuilder {
	m.providerStatus = &ps
	return m
}

// WithNodeRef sets the node ref status field for the machine builder.
func (m MachineBuilder) WithNodeRef(nodeRef corev1.ObjectReference) MachineBuilder {
	m.nodeRef = &nodeRef
	return m
}
