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

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Namespace creates a new namespace builder.
func Namespace() NamespaceBuilder {
	return NamespaceBuilder{}
}

// NamespaceBuilder is used to build out a namespace object.
type NamespaceBuilder struct {
	generateName string
	name         string
}

// Build builds a new namespace based on the configuration provided.
func (n NamespaceBuilder) Build() *corev1.Namespace {
	return &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: n.generateName,
			Name:         n.name,
		},
	}
}

// WithGenerateName sets the generateName for the namespace builder.
func (n NamespaceBuilder) WithGenerateName(generateName string) NamespaceBuilder {
	n.generateName = generateName
	return n
}

// WithName sets the name for the namespace builder.
func (n NamespaceBuilder) WithName(name string) NamespaceBuilder {
	n.name = name
	return n
}
