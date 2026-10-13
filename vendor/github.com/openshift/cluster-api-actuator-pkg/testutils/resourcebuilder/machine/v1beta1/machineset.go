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
)

const (
	machineSetMachineRoleLabelName = "machine.openshift.io/cluster-api-machine-role"
	machineSetMachineTypeLabelName = "machine.openshift.io/cluster-api-machine-type"
)

// MachineSet creates a new machineSet builder.
func MachineSet() MachineSetBuilder {
	return MachineSetBuilder{}
}

// MachineSetBuilder is used to build out a machineSet object.
type MachineSetBuilder struct {
	annotations                map[string]string
	authoritativeAPI           machinev1beta1.MachineAuthority
	creationTimestamp          metav1.Time
	deletePolicy               string
	deletionTimestamp          *metav1.Time
	generateName               string
	labels                     map[string]string
	lifecycleHooks             machinev1beta1.LifecycleHooks
	machineSpec                *machinev1beta1.MachineSpec
	machineSpecObjectMeta      machinev1beta1.ObjectMeta
	machineSetSpecSelector     *metav1.LabelSelector
	machineTemplateAnnotations map[string]string
	machineTemplateLabels      map[string]string
	minReadySeconds            int32
	name                       string
	namespace                  string
	ownerReferences            []metav1.OwnerReference
	providerSpec               *machinev1beta1.ProviderSpec
	providerSpecBuilder        *resourcebuilder.RawExtensionBuilder
	replicas                   *int32
	taints                     []corev1.Taint

	// status fields
	authoritativeAPIStatus machinev1beta1.MachineAuthority
	availableReplicas      int32
	conditions             []machinev1beta1.Condition
	errorMessage           *string
	errorReason            *machinev1beta1.MachineSetStatusError
	fullyLabeledReplicas   int32
	observedGeneration     int64
	readyReplicas          int32
	replicasStatus         int32
	synchronizedGeneration int64
}

// Build builds a new machineSet based on the configuration provided.
func (m MachineSetBuilder) Build() *machinev1beta1.MachineSet {
	machineSet := &machinev1beta1.MachineSet{
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
		Spec: machinev1beta1.MachineSetSpec{
			AuthoritativeAPI: m.authoritativeAPI,
			DeletePolicy:     m.deletePolicy,
			MinReadySeconds:  m.minReadySeconds,
			Replicas:         m.replicas,
			Selector: coalesceMachineSetSpecSelector(m.machineSetSpecSelector, metav1.LabelSelector{
				MatchLabels: m.machineTemplateLabels,
			}),
			Template: machinev1beta1.MachineTemplateSpec{
				ObjectMeta: machinev1beta1.ObjectMeta{
					Labels:      m.machineTemplateLabels,
					Annotations: m.machineTemplateAnnotations,
				},
				Spec: coalesceMachineSpec(m.machineSpec, machinev1beta1.MachineSpec{
					AuthoritativeAPI: m.authoritativeAPI,
					LifecycleHooks:   m.lifecycleHooks,
					ObjectMeta:       m.machineSpecObjectMeta,
					// ProviderID: not populated as it should be unique per machine.
					ProviderSpec: resourcebuilder.Coalesce(m.providerSpec, machinev1beta1.ProviderSpec{
						Value: coalesceProviderSpecValue(m.providerSpecBuilder),
					}),
					Taints: m.taints,
				}),
			},
		},
		Status: machinev1beta1.MachineSetStatus{
			AuthoritativeAPI:       m.authoritativeAPIStatus,
			AvailableReplicas:      m.availableReplicas,
			Conditions:             m.conditions,
			ErrorMessage:           m.errorMessage,
			ErrorReason:            m.errorReason,
			FullyLabeledReplicas:   m.fullyLabeledReplicas,
			ObservedGeneration:     m.observedGeneration,
			ReadyReplicas:          m.readyReplicas,
			Replicas:               m.replicasStatus,
			SynchronizedGeneration: m.synchronizedGeneration,
		},
	}

	m.WithLabel(machinev1beta1.MachineClusterIDLabel, resourcebuilder.TestClusterIDValue)

	return machineSet
}

// AsWorker sets the worker role and type on the machineSet labels for the machineSet builder.
func (m MachineSetBuilder) AsWorker() MachineSetBuilder {
	return m.
		WithLabel(machineSetMachineRoleLabelName, "worker").
		WithLabel(machineSetMachineTypeLabelName, "worker").
		WithMachineTemplateLabel(machineSetMachineRoleLabelName, "worker").
		WithMachineTemplateLabel(machineSetMachineTypeLabelName, "worker")
}

// WithAnnotations sets the annotations for the machineSet on the machineSet builder.
func (m MachineSetBuilder) WithAnnotations(annotations map[string]string) MachineSetBuilder {
	m.annotations = annotations
	return m
}

// WithAuthoritativeAPI sets the authoritativeAPI for the machineSet builder.
func (m MachineSetBuilder) WithAuthoritativeAPI(authority machinev1beta1.MachineAuthority) MachineSetBuilder {
	m.authoritativeAPI = authority
	return m
}

// WithCreationTimestamp sets the creationTimestamp for the machineSet builder.
func (m MachineSetBuilder) WithCreationTimestamp(time metav1.Time) MachineSetBuilder {
	m.creationTimestamp = time
	return m
}

// WithDeletePolicy sets the deletePolicy for the machineSet builder.
func (m MachineSetBuilder) WithDeletePolicy(policy string) MachineSetBuilder {
	m.deletePolicy = policy
	return m
}

// WithDeletionTimestamp sets the deletionTimestamp for the machineSet builder.
// Note: This can only be used in unit testing as the API server will drop this
// field if a create/update request tries to set it.
func (m MachineSetBuilder) WithDeletionTimestamp(time *metav1.Time) MachineSetBuilder {
	m.deletionTimestamp = time
	return m
}

// WithGenerateName sets the generateName for the machineSet builder.
func (m MachineSetBuilder) WithGenerateName(generateName string) MachineSetBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the for the machineSet on the machineSet builder.
func (m MachineSetBuilder) WithLabel(key, value string) MachineSetBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the machineSet on the machineSet builder.
func (m MachineSetBuilder) WithLabels(labels map[string]string) MachineSetBuilder {
	m.labels = labels
	return m
}

// WithLifecycleHooks sets the lifecycleHooks for the machineSet builder.
func (m MachineSetBuilder) WithLifecycleHooks(lh machinev1beta1.LifecycleHooks) MachineSetBuilder {
	m.lifecycleHooks = lh
	return m
}

// WithMachineSpec sets the MachineSpec field for the machineSet builder.
func (m MachineSetBuilder) WithMachineSpec(machineSpec machinev1beta1.MachineSpec) MachineSetBuilder {
	m.machineSpec = &machineSpec
	return m
}

// WithMachineSpecObjectMeta sets the ObjectMeta on the machine spec field for the machineSet builder.
func (m MachineSetBuilder) WithMachineSpecObjectMeta(machineSpecObjectMeta machinev1beta1.ObjectMeta) MachineSetBuilder {
	m.machineSpecObjectMeta = machineSpecObjectMeta
	return m
}

// WithMachineSetSpecSelector sets the machine label selector on the machineSet builder.
func (m MachineSetBuilder) WithMachineSetSpecSelector(selector metav1.LabelSelector) MachineSetBuilder {
	m.machineSetSpecSelector = &selector
	return m
}

// WithMachineTemplateAnnotations sets the annotations for the machine template on the machineSet builder.
func (m MachineSetBuilder) WithMachineTemplateAnnotations(annotations map[string]string) MachineSetBuilder {
	m.machineTemplateAnnotations = annotations
	return m
}

// WithMachineTemplateLabel sets the labels for the machine template on the machineSet builder.
func (m MachineSetBuilder) WithMachineTemplateLabel(key, value string) MachineSetBuilder {
	if m.machineTemplateLabels == nil {
		m.machineTemplateLabels = make(map[string]string)
	}

	m.machineTemplateLabels[key] = value

	return m
}

// WithMachineTemplateLabels sets the labels for the machine template on the machineSet builder.
func (m MachineSetBuilder) WithMachineTemplateLabels(labels map[string]string) MachineSetBuilder {
	m.machineTemplateLabels = labels
	return m
}

// WithMinReadySeconds sets the minReadySeconds for the machine template on the machineSet builder.
func (m MachineSetBuilder) WithMinReadySeconds(minReadySeconds int32) MachineSetBuilder {
	m.minReadySeconds = minReadySeconds
	return m
}

// WithName sets the name for the machineSet builder.
func (m MachineSetBuilder) WithName(name string) MachineSetBuilder {
	m.name = name
	return m
}

// WithNamespace sets the namespace for the machineSet builder.
func (m MachineSetBuilder) WithNamespace(namespace string) MachineSetBuilder {
	m.namespace = namespace
	return m
}

// WithOwnerReferences sets the OwnerReferences for the machineSet builder.
func (m MachineSetBuilder) WithOwnerReferences(ownerRefs []metav1.OwnerReference) MachineSetBuilder {
	m.ownerReferences = ownerRefs
	return m
}

// WithProviderSpec sets the ProviderSpec field for the machineSet builder.
func (m MachineSetBuilder) WithProviderSpec(providerSpec machinev1beta1.ProviderSpec) MachineSetBuilder {
	m.providerSpec = &providerSpec
	return m
}

// WithProviderSpecBuilder sets the providerSpec builder for the machineSet builder.
func (m MachineSetBuilder) WithProviderSpecBuilder(builder resourcebuilder.RawExtensionBuilder) MachineSetBuilder {
	m.providerSpecBuilder = &builder
	return m
}

// WithReplicas sets the replicas for the machineSet builder.
func (m MachineSetBuilder) WithReplicas(replicas int32) MachineSetBuilder {
	m.replicas = &replicas
	return m
}

// WithTaints sets the taints field for the machineSet builder.
func (m MachineSetBuilder) WithTaints(taints []corev1.Taint) MachineSetBuilder {
	m.taints = taints
	return m
}

// Status Fields

// WithAuthoritativeAPIStatus sets the authoritativeAPIStatus for the machine builder.
func (m MachineSetBuilder) WithAuthoritativeAPIStatus(authority machinev1beta1.MachineAuthority) MachineSetBuilder {
	m.authoritativeAPIStatus = authority
	return m
}

// WithAvailableReplicas sets the availableReplicas for the machineSet builder.
func (m MachineSetBuilder) WithAvailableReplicas(n int32) MachineSetBuilder {
	m.availableReplicas = n
	return m
}

// WithConditions sets the conditions status field for the machine builder.
func (m MachineSetBuilder) WithConditions(c []machinev1beta1.Condition) MachineSetBuilder {
	m.conditions = c
	return m
}

// WithErrorMessage sets the error message status field for the machine builder.
func (m MachineSetBuilder) WithErrorMessage(errorMsg string) MachineSetBuilder {
	m.errorMessage = &errorMsg
	return m
}

// WithErrorReason sets the error reason status field for the machine builder.
func (m MachineSetBuilder) WithErrorReason(errorReason machinev1beta1.MachineSetStatusError) MachineSetBuilder {
	m.errorReason = &errorReason
	return m
}

// WithFullyLabeledReplicas sets the fullyLabeledReplicas for the machineSet builder.
func (m MachineSetBuilder) WithFullyLabeledReplicas(n int32) MachineSetBuilder {
	m.fullyLabeledReplicas = n
	return m
}

// WithObservedGeneration sets the observedGeneration for the machineSet builder.
func (m MachineSetBuilder) WithObservedGeneration(n int64) MachineSetBuilder {
	m.observedGeneration = n
	return m
}

// WithReadyReplicas sets the readyReplicas for the machineSet builder.
func (m MachineSetBuilder) WithReadyReplicas(r int32) MachineSetBuilder {
	m.readyReplicas = r
	return m
}

// WithReplicasStatus sets the replicas status field for the machineSet builder.
func (m MachineSetBuilder) WithReplicasStatus(r int32) MachineSetBuilder {
	m.replicasStatus = r
	return m
}

// WithSynchronizedGeneration sets the synchronizedGeneration for the machineSet builder.
func (m MachineSetBuilder) WithSynchronizedGeneration(n int64) MachineSetBuilder {
	m.synchronizedGeneration = n
	return m
}
