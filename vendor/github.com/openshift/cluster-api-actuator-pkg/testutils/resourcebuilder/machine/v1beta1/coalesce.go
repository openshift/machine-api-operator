/*
Copyright 2024 Red Hat, Inc.

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
	"k8s.io/apimachinery/pkg/runtime"
)

func coalesceAWSResourceReference(v1 *machinev1beta1.AWSResourceReference, v2 machinev1beta1.AWSResourceReference) machinev1beta1.AWSResourceReference {
	if v1 == nil {
		return v2
	}

	return *v1
}

func coalesceAWSResourceReferences(v1 *[]machinev1beta1.AWSResourceReference, v2 []machinev1beta1.AWSResourceReference) []machinev1beta1.AWSResourceReference {
	if v1 == nil {
		return v2
	}

	return *v1
}

func coalesceLoadBalancers(v1 *[]machinev1beta1.LoadBalancerReference, v2 []machinev1beta1.LoadBalancerReference) []machinev1beta1.LoadBalancerReference {
	if v1 == nil {
		return v2
	}

	return *v1
}

func coalesceBlockDevices(v1 *[]machinev1beta1.BlockDeviceMappingSpec, v2 []machinev1beta1.BlockDeviceMappingSpec) []machinev1beta1.BlockDeviceMappingSpec {
	if v1 == nil {
		return v2
	}

	return *v1
}

func coalesceTags(v1 *[]machinev1beta1.TagSpecification, v2 []machinev1beta1.TagSpecification) []machinev1beta1.TagSpecification {
	if v1 == nil {
		return v2
	}

	return *v1
}

func coalesceMachineSpec(v1 *machinev1beta1.MachineSpec, v2 machinev1beta1.MachineSpec) machinev1beta1.MachineSpec {
	if v1 == nil {
		return v2
	}

	return *v1
}

func coalesceProviderSpecValue(v1 *resourcebuilder.RawExtensionBuilder) *runtime.RawExtension {
	if v1 == nil {
		return nil
	}

	return (*v1).BuildRawExtension()
}

func coalesceMachineSetSpecSelector(v1 *metav1.LabelSelector, v2 metav1.LabelSelector) metav1.LabelSelector {
	if v1 == nil {
		return v2
	}

	return *v1
}
