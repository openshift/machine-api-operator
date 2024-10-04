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

// ConfigMap creates a new ConfigMap builder.
func ConfigMap() ConfigMapBuilder {
	return ConfigMapBuilder{}
}

// ConfigMapBuilder is used to build out a ConfigMap object.
type ConfigMapBuilder struct {
	generateName string
	name         string
	namespace    string
	labels       map[string]string
	data         map[string]string
}

// Build builds a new ConfigMap based on the configuration provided.
func (m ConfigMapBuilder) Build() *corev1.ConfigMap {
	ConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: m.generateName,
			Name:         m.name,
			Namespace:    m.namespace,
			Labels:       m.labels,
		},
		Data: m.data,
	}

	return ConfigMap
}

// WithData sets the data for the ConfigMap builder.
func (m ConfigMapBuilder) WithData(data map[string]string) ConfigMapBuilder {
	m.data = data
	return m
}

// WithGenerateName sets the generateName for the ConfigMap builder.
func (m ConfigMapBuilder) WithGenerateName(generateName string) ConfigMapBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the ConfigMap builder.
func (m ConfigMapBuilder) WithLabel(key, value string) ConfigMapBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the ConfigMap builder.
func (m ConfigMapBuilder) WithLabels(labels map[string]string) ConfigMapBuilder {
	m.labels = labels
	return m
}

// WithName sets the name for the ConfigMap builder.
func (m ConfigMapBuilder) WithName(name string) ConfigMapBuilder {
	m.name = name
	return m
}

// WithNamespace sets the namespace for the ConfigMap builder.
func (m ConfigMapBuilder) WithNamespace(namespace string) ConfigMapBuilder {
	m.namespace = namespace
	return m
}
