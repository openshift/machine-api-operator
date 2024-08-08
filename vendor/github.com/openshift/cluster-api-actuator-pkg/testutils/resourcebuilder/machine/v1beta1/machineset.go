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
	generateName        string
	name                string
	namespace           string
	replicas            int32
	labels              map[string]string
	creationTimestamp   metav1.Time
	providerSpecBuilder resourcebuilder.RawExtensionBuilder

	// status fields
	errorMessage *string
}

// Build builds a new machineSet based on the configuration provided.
func (m MachineSetBuilder) Build() *machinev1beta1.MachineSet {
	machineSet := &machinev1beta1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:      m.generateName,
			CreationTimestamp: m.creationTimestamp,
			Name:              m.name,
			Namespace:         m.namespace,
			Labels:            m.labels,
		},
		Spec: machinev1beta1.MachineSetSpec{
			Replicas: &m.replicas,
			Selector: metav1.LabelSelector{
				MatchLabels: m.labels,
			},
			Template: machinev1beta1.MachineTemplateSpec{
				ObjectMeta: machinev1beta1.ObjectMeta{
					Labels: m.labels,
				},
			},
		},
		Status: machinev1beta1.MachineSetStatus{
			ErrorMessage: m.errorMessage,
		},
	}

	if m.providerSpecBuilder != nil {
		machineSet.Spec.Template.Spec.ProviderSpec.Value = m.providerSpecBuilder.BuildRawExtension()
	}

	m.WithLabel(machinev1beta1.MachineClusterIDLabel, resourcebuilder.TestClusterIDValue)

	return machineSet
}

// AsWorker sets the worker role and type on the machineSet labels for the machineSet builder.
func (m MachineSetBuilder) AsWorker() MachineSetBuilder {
	return m.
		WithLabel(machineSetMachineRoleLabelName, "worker").
		WithLabel(machineSetMachineTypeLabelName, "worker")
}

// WithCreationTimestamp sets the creationTimestamp for the machineSet builder.
func (m MachineSetBuilder) WithCreationTimestamp(time metav1.Time) MachineSetBuilder {
	m.creationTimestamp = time
	return m
}

// WithGenerateName sets the generateName for the machineSet builder.
func (m MachineSetBuilder) WithGenerateName(generateName string) MachineSetBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the machineSet builder.
func (m MachineSetBuilder) WithLabel(key, value string) MachineSetBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the machineSet builder.
func (m MachineSetBuilder) WithLabels(labels map[string]string) MachineSetBuilder {
	m.labels = labels
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

// WithProviderSpecBuilder sets the providerSpec builder for the machineSet builder.
func (m MachineSetBuilder) WithProviderSpecBuilder(builder resourcebuilder.RawExtensionBuilder) MachineSetBuilder {
	m.providerSpecBuilder = builder
	return m
}

// WithReplicas sets the replicas for the machineSet builder.
func (m MachineSetBuilder) WithReplicas(replicas int32) MachineSetBuilder {
	m.replicas = replicas
	return m
}

// Status Fields

// WithErrorMessage sets the error message status field for the machineSet builder.
func (m MachineSetBuilder) WithErrorMessage(errorMsg string) MachineSetBuilder {
	m.errorMessage = &errorMsg
	return m
}
