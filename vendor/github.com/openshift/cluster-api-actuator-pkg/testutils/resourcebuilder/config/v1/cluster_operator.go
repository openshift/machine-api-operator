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
	configv1 "github.com/openshift/api/config/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ClusterOperator creates a new cluster operator builder.
func ClusterOperator() ClusterOperatorBuilder {
	return ClusterOperatorBuilder{}
}

// ClusterOperatorBuilder is used to build out a cluster operator object.
type ClusterOperatorBuilder struct {
	name string
}

// Build builds a new cluster operator based on the configuration provided.
func (n ClusterOperatorBuilder) Build() *configv1.ClusterOperator {
	return &configv1.ClusterOperator{
		ObjectMeta: metav1.ObjectMeta{
			Name: n.name,
		},
	}
}

// WithName sets the name for the cluster operator builder.
func (n ClusterOperatorBuilder) WithName(name string) ClusterOperatorBuilder {
	n.name = name
	return n
}

// ClusterOperatorStatus creates a new cluster operator status builder.
func ClusterOperatorStatus() ClusterOperatorStatusBuilder {
	return ClusterOperatorStatusBuilder{}
}

// ClusterOperatorStatusBuilder is used to build out a cluster operator status object.
type ClusterOperatorStatusBuilder struct {
	conditions []configv1.ClusterOperatorStatusCondition
}

// Build builds a new cluster operator status based on the configuration provided.
func (n ClusterOperatorStatusBuilder) Build() configv1.ClusterOperatorStatus {
	return configv1.ClusterOperatorStatus{
		Conditions: n.conditions,
	}
}
