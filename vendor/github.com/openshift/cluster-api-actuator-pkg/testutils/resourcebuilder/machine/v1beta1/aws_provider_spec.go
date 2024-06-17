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
	"encoding/json"

	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

// AWSProviderSpec creates a new AWS machine config builder.
func AWSProviderSpec() AWSProviderSpecBuilder {
	return AWSProviderSpecBuilder{
		availabilityZone: "us-east-1a",
		instanceType:     "m6i.xlarge",
		securityGroups: []machinev1beta1.AWSResourceReference{
			{
				Filters: []machinev1beta1.Filter{
					{
						Name: "tag:Name",
						Values: []string{
							"aws-security-group-12345678",
						},
					},
				},
			},
		},
		subnet: machinev1beta1.AWSResourceReference{
			Filters: []machinev1beta1.Filter{
				{
					Name: "tag:Name",
					Values: []string{
						"aws-subnet-12345678",
					},
				},
			},
		},
	}
}

// AWSProviderSpecBuilder is used to build out a AWS machine config object.
type AWSProviderSpecBuilder struct {
	availabilityZone string
	instanceType     string
	securityGroups   []machinev1beta1.AWSResourceReference
	subnet           machinev1beta1.AWSResourceReference
}

// Build builds a new AWS machine config based on the configuration provided.
func (m AWSProviderSpecBuilder) Build() *machinev1beta1.AWSMachineProviderConfig {
	return &machinev1beta1.AWSMachineProviderConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "awsproviderconfig.openshift.io/v1beta1",
			Kind:       "AWSMachineProviderConfig",
		},
		AMI: machinev1beta1.AWSResourceReference{
			ID: ptr.To[string]("aws-ami-12345678"),
		},
		BlockDevices: []machinev1beta1.BlockDeviceMappingSpec{
			{
				EBS: &machinev1beta1.EBSBlockDeviceSpec{
					Encrypted:  ptr.To[bool](true),
					VolumeSize: ptr.To[int64](120),
					VolumeType: ptr.To[string]("gp3"),
				},
			},
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: "aws-cloud-credentials",
		},
		IAMInstanceProfile: &machinev1beta1.AWSResourceReference{
			ID: ptr.To[string]("aws-iam-instance-profile-12345678"),
		},
		InstanceType: m.instanceType,
		LoadBalancers: []machinev1beta1.LoadBalancerReference{
			{
				Type: "network",
				Name: "aws-nlb-int",
			},
			{
				Type: "network",
				Name: "aws-nlb-ext",
			},
		},
		Placement: machinev1beta1.Placement{
			Region:           "us-east-1",
			AvailabilityZone: m.availabilityZone,
		},
		SecurityGroups: m.securityGroups,
		Subnet:         m.subnet,
		UserDataSecret: &corev1.LocalObjectReference{
			Name: "aws-user-data-12345678",
		},
	}
}

// BuildRawExtension builds a new AWS machine config based on the configuration provided.
func (m AWSProviderSpecBuilder) BuildRawExtension() *runtime.RawExtension {
	providerConfig := m.Build()

	raw, err := json.Marshal(providerConfig)
	if err != nil {
		// As we are building the input to json.Marshal, this should never happen.
		panic(err)
	}

	return &runtime.RawExtension{
		Raw: raw,
	}
}

// WithAvailabilityZone sets the availabilityZone for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithAvailabilityZone(az string) AWSProviderSpecBuilder {
	m.availabilityZone = az
	return m
}

// WithInstanceType sets the instanceType for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithInstanceType(instanceType string) AWSProviderSpecBuilder {
	m.instanceType = instanceType
	return m
}

// WithSecurityGroups sets the securityGroups for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithSecurityGroups(sgs []machinev1beta1.AWSResourceReference) AWSProviderSpecBuilder {
	m.securityGroups = sgs
	return m
}

// WithSubnet sets the subnet for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithSubnet(subnet machinev1beta1.AWSResourceReference) AWSProviderSpecBuilder {
	m.subnet = subnet
	return m
}
