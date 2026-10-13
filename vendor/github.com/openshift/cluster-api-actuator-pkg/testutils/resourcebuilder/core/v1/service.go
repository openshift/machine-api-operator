/*
Copyright 2023 Red Hat, Inc.

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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Service creates a new Service builder.
func Service() ServiceBuilder {
	return ServiceBuilder{}
}

// ServiceBuilder is used to build out a Service object.
type ServiceBuilder struct {
	generateName string
	name         string
	namespace    string
	labels       map[string]string
	selector     map[string]string
	ports        []corev1.ServicePort
}

// Build builds a new Service based on the configuration provided.
func (m ServiceBuilder) Build() *corev1.Service {
	Service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: m.generateName,
			Name:         m.name,
			Namespace:    m.namespace,
			Labels:       m.labels,
		},
		Spec: corev1.ServiceSpec{
			Ports:    m.ports,
			Selector: m.selector,
		},
	}

	return Service
}

// WithGenerateName sets the generateName for the Service builder.
func (m ServiceBuilder) WithGenerateName(generateName string) ServiceBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the Service builder.
func (m ServiceBuilder) WithLabel(key, value string) ServiceBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the Service builder.
func (m ServiceBuilder) WithLabels(labels map[string]string) ServiceBuilder {
	m.labels = labels
	return m
}

// WithName sets the name for the Service builder.
func (m ServiceBuilder) WithName(name string) ServiceBuilder {
	m.name = name
	return m
}

// WithNamespace sets the namespace for the Service builder.
func (m ServiceBuilder) WithNamespace(namespace string) ServiceBuilder {
	m.namespace = namespace
	return m
}

// WithPorts sets the ports for the Service builder.
func (m ServiceBuilder) WithPorts(ports []corev1.ServicePort) ServiceBuilder {
	m.ports = ports
	return m
}

// WithSelector sets the selector for the Service builder.
func (m ServiceBuilder) WithSelector(selector map[string]string) ServiceBuilder {
	m.selector = selector
	return m
}
