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

const (
	masterNodeRoleLabel = "node-role.kubernetes.io/master"
	workerNodeRoleLabel = "node-role.kubernetes.io/worker"
)

// Node creates a new node builder.
func Node() NodeBuilder {
	return NodeBuilder{}
}

// NodeBuilder is used to build out a node object.
type NodeBuilder struct {
	generateName string
	name         string
	labels       map[string]string
	conditions   []corev1.NodeCondition
}

// Build builds a new node based on the configuration provided.
func (m NodeBuilder) Build() *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: m.generateName,
			Name:         m.name,
			Labels:       m.labels,
		},
		Status: corev1.NodeStatus{
			Conditions: m.conditions,
		},
	}

	return node
}

// AsWorker sets the worker role on the node labels for the node builder.
func (m NodeBuilder) AsWorker() NodeBuilder {
	return m.WithLabel(workerNodeRoleLabel, "")
}

// AsMaster sets the master role on the node labels for the node builder.
func (m NodeBuilder) AsMaster() NodeBuilder {
	return m.WithLabel(masterNodeRoleLabel, "")
}

// AsNotReady sets the node as ready for the node builder.
func (m NodeBuilder) AsNotReady() NodeBuilder {
	return m.WithConditions([]corev1.NodeCondition{
		{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionFalse,
		},
	})
}

// AsReady sets the node as ready for the node builder.
func (m NodeBuilder) AsReady() NodeBuilder {
	return m.WithConditions([]corev1.NodeCondition{
		{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		},
	})
}

// WithConditions sets the conditions for the node builder.
func (m NodeBuilder) WithConditions(conditions []corev1.NodeCondition) NodeBuilder {
	m.conditions = conditions
	return m
}

// WithGenerateName sets the generateName for the node builder.
func (m NodeBuilder) WithGenerateName(generateName string) NodeBuilder {
	m.generateName = generateName
	return m
}

// WithLabel sets the labels for the node builder.
func (m NodeBuilder) WithLabel(key, value string) NodeBuilder {
	if m.labels == nil {
		m.labels = make(map[string]string)
	}

	m.labels[key] = value

	return m
}

// WithLabels sets the labels for the node builder.
func (m NodeBuilder) WithLabels(labels map[string]string) NodeBuilder {
	m.labels = labels
	return m
}

// WithName sets the name for the node builder.
func (m NodeBuilder) WithName(name string) NodeBuilder {
	m.name = name
	return m
}
