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

// Secret creates a new Secret builder.
func Secret() SecretBuilder {
	return SecretBuilder{}
}

// SecretBuilder is used to build out a Secret object.
type SecretBuilder struct {
	generateName string
	name         string
	namespace    string
	labels       map[string]string
	data         map[string][]byte
}

// Build builds a new Secret based on the configuration provided.
func (m SecretBuilder) Build() *corev1.Secret {
	Secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: m.generateName,
			Name:         m.name,
			Namespace:    m.namespace,
			Labels:       m.labels,
		},
		Data: m.data,
	}

	return Secret
}

// WithData sets the data for the Secret builder.
func (m SecretBuilder) WithData(data map[string][]byte) SecretBuilder {
	m.data = data
	return m
}

// WithGenerateName sets the generateName for the Secret builder.
func (m SecretBuilder) WithGenerateName(generateName string) SecretBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the Secret builder.
func (m SecretBuilder) WithLabel(key, value string) SecretBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the Secret builder.
func (m SecretBuilder) WithLabels(labels map[string]string) SecretBuilder {
	m.labels = labels
	return m
}

// WithName sets the name for the Secret builder.
func (m SecretBuilder) WithName(name string) SecretBuilder {
	m.name = name
	return m
}

// WithNamespace sets the namespace for the Secret builder.
func (m SecretBuilder) WithNamespace(namespace string) SecretBuilder {
	m.namespace = namespace
	return m
}
