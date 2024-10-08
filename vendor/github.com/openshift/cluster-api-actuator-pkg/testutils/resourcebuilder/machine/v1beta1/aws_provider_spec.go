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
	"github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

var (
	defaultAMIID                 = ptr.To[string]("aws-ami-12345678")
	defaultAvailabilityZone      = "us-east-1a"
	defaultCredentialsSecretName = "aws-cloud-credentials"
	defaultDeviceIndex           = int64(0)
	defaultIAMInstanceProfile    = &machinev1beta1.AWSResourceReference{
		ID: ptr.To[string]("aws-iam-instance-profile-12345678"),
	}
	defaultInstanceType  = "m6i.xlarge"
	defaultLoadBalancers = []machinev1beta1.LoadBalancerReference{
		{
			Type: "network",
			Name: "aws-nlb-int",
		},
		{
			Type: "network",
			Name: "aws-nlb-ext",
		},
	}
	defaultBlockDevices = []machinev1beta1.BlockDeviceMappingSpec{
		{
			EBS: &machinev1beta1.EBSBlockDeviceSpec{
				Encrypted:  ptr.To[bool](true),
				VolumeSize: ptr.To[int64](120),
				VolumeType: ptr.To[string]("gp3"),
			},
		},
	}
	// zero-value of the type, to play well with omitempty.
	defaultMetadataServiceOptions = machinev1beta1.MetadataServiceOptions{}
	// zero-value of the type, to play well with omitempty.
	defaultNetworkInterfaceType = machinev1beta1.AWSNetworkInterfaceType("")
	defaultPlacement            = machinev1beta1.Placement{
		Region:           defaultRegion,
		AvailabilityZone: defaultAvailabilityZone,
	}
	defaultPlacementGroupName = "" // zero-value of the type, to play well with omitempty.
	defaultRegion             = "us-east-1"
	defaultSecurityGroups     = []machinev1beta1.AWSResourceReference{
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
	}
	defaultSubnet = machinev1beta1.AWSResourceReference{
		Filters: []machinev1beta1.Filter{
			{
				Name: "tag:Name",
				Values: []string{
					"aws-subnet-12345678",
				},
			},
		},
	}
	defaultUserDataSecretName = "aws-user-data-12345678"
)

// AWSProviderSpec creates a new AWS machine config builder.
func AWSProviderSpec() AWSProviderSpecBuilder {
	return AWSProviderSpecBuilder{}
}

// AWSProviderSpecBuilder is used to build out a AWS machine config object.
// All the fields of this struct are a pointer to the corresponding original type
// in the machinev1beta1.AWSMachineProviderConfig. This is done to enable representing
// the value not being specified in the building chain (so it can be defaulted), versus
// it being specified, either for setting it to a custom value or to the zero-value of that type.
type AWSProviderSpecBuilder struct {
	ami                    *machinev1beta1.AWSResourceReference
	availabilityZone       *string
	blockDevices           *[]machinev1beta1.BlockDeviceMappingSpec
	credentialsSecret      **corev1.LocalObjectReference
	deviceIndex            *int64
	iamInstanceProfile     **machinev1beta1.AWSResourceReference
	instanceType           *string
	keyName                **string
	loadBalancers          *[]machinev1beta1.LoadBalancerReference
	metadataServiceOptions *machinev1beta1.MetadataServiceOptions
	networkInterfaceType   *machinev1beta1.AWSNetworkInterfaceType
	placement              *machinev1beta1.Placement
	placementGroupName     *string
	publicIP               **bool
	region                 *string
	securityGroups         *[]machinev1beta1.AWSResourceReference
	spotMarketOptions      **machinev1beta1.SpotMarketOptions
	subnet                 *machinev1beta1.AWSResourceReference
	tags                   *[]machinev1beta1.TagSpecification
	userDataSecret         **corev1.LocalObjectReference
}

// Build builds a new AWS machine config based on the configuration provided.
func (m AWSProviderSpecBuilder) Build() *machinev1beta1.AWSMachineProviderConfig {
	return &machinev1beta1.AWSMachineProviderConfig{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "awsproviderconfig.openshift.io/v1beta1",
			Kind:       "AWSMachineProviderConfig",
		},
		AMI:                    coalesceAWSResourceReference(m.ami, machinev1beta1.AWSResourceReference{ID: defaultAMIID}),
		BlockDevices:           coalesceBlockDevices(m.blockDevices, defaultBlockDevices),
		CredentialsSecret:      resourcebuilder.Coalesce(m.credentialsSecret, &corev1.LocalObjectReference{Name: defaultCredentialsSecretName}),
		DeviceIndex:            resourcebuilder.Coalesce(m.deviceIndex, defaultDeviceIndex),
		IAMInstanceProfile:     resourcebuilder.Coalesce(m.iamInstanceProfile, defaultIAMInstanceProfile),
		InstanceType:           resourcebuilder.Coalesce(m.instanceType, defaultInstanceType),
		KeyName:                resourcebuilder.Coalesce(m.keyName, nil),
		LoadBalancers:          coalesceLoadBalancers(m.loadBalancers, defaultLoadBalancers),
		MetadataServiceOptions: resourcebuilder.Coalesce(m.metadataServiceOptions, defaultMetadataServiceOptions),
		NetworkInterfaceType:   resourcebuilder.Coalesce(m.networkInterfaceType, defaultNetworkInterfaceType),
		Placement: resourcebuilder.Coalesce(m.placement, machinev1beta1.Placement{
			Region:           resourcebuilder.Coalesce(m.region, defaultRegion),
			AvailabilityZone: resourcebuilder.Coalesce(m.availabilityZone, defaultAvailabilityZone),
		}),
		PlacementGroupName: resourcebuilder.Coalesce(m.placementGroupName, defaultPlacementGroupName),
		PublicIP:           resourcebuilder.Coalesce(m.publicIP, nil),
		SecurityGroups:     coalesceAWSResourceReferences(m.securityGroups, defaultSecurityGroups),
		SpotMarketOptions:  resourcebuilder.Coalesce(m.spotMarketOptions, nil),
		Subnet:             coalesceAWSResourceReference(m.subnet, defaultSubnet),
		Tags:               coalesceTags(m.tags, nil),
		UserDataSecret:     resourcebuilder.Coalesce(m.userDataSecret, &corev1.LocalObjectReference{Name: defaultUserDataSecretName}),
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

// WithAMI sets the AMI for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithAMI(ami machinev1beta1.AWSResourceReference) AWSProviderSpecBuilder {
	m.ami = &ami
	return m
}

// WithAvailabilityZone sets the availabilityZone for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithAvailabilityZone(az string) AWSProviderSpecBuilder {
	m.availabilityZone = &az
	return m
}

// WithBlockDevices sets the BlockDevices for the AWS machine config builder.
// Use WithBlockDevices(nil), to set them to the zero value.
func (m AWSProviderSpecBuilder) WithBlockDevices(blockDevices []machinev1beta1.BlockDeviceMappingSpec) AWSProviderSpecBuilder {
	m.blockDevices = &blockDevices
	return m
}

// WithCredentialsSecret sets the CredentialsSecret for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithCredentialsSecret(credentialsSecret *corev1.LocalObjectReference) AWSProviderSpecBuilder {
	m.credentialsSecret = &credentialsSecret
	return m
}

// WithDeviceIndex sets the DeviceIndex for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithDeviceIndex(deviceIndex int64) AWSProviderSpecBuilder {
	m.deviceIndex = &deviceIndex
	return m
}

// WithIAMInstanceProfile sets the AMI for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithIAMInstanceProfile(iamInstanceProfile *machinev1beta1.AWSResourceReference) AWSProviderSpecBuilder {
	m.iamInstanceProfile = &iamInstanceProfile
	return m
}

// WithInstanceType sets the instanceType for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithInstanceType(instanceType string) AWSProviderSpecBuilder {
	m.instanceType = &instanceType
	return m
}

// WithKeyName sets the keyName for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithKeyName(keyName *string) AWSProviderSpecBuilder {
	m.keyName = &keyName
	return m
}

// WithLoadBalancers sets the LoadBalancers for the AWS machine config builder.
// Use WithLoadBalancers(nil), to set them to the zero value.
func (m AWSProviderSpecBuilder) WithLoadBalancers(loadBalancers []machinev1beta1.LoadBalancerReference) AWSProviderSpecBuilder {
	m.loadBalancers = &loadBalancers
	return m
}

// WithMetadataServiceOptions sets the MetadataServiceOptions for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithMetadataServiceOptions(opts machinev1beta1.MetadataServiceOptions) AWSProviderSpecBuilder {
	m.metadataServiceOptions = &opts
	return m
}

// WithNetworkInterfaceType sets the NetworkInterfaceType for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithNetworkInterfaceType(netIntType machinev1beta1.AWSNetworkInterfaceType) AWSProviderSpecBuilder {
	m.networkInterfaceType = &netIntType
	return m
}

// WithPlacement sets the Placement for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithPlacement(placement machinev1beta1.Placement) AWSProviderSpecBuilder {
	m.placement = &placement
	return m
}

// WithPlacementGroupName sets the PlacementGroupName for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithPlacementGroupName(placementGroupName string) AWSProviderSpecBuilder {
	m.placementGroupName = &placementGroupName
	return m
}

// WithPublicIP sets the PublicIP for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithPublicIP(publicIP *bool) AWSProviderSpecBuilder {
	m.publicIP = &publicIP
	return m
}

// WithRegion sets the region for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithRegion(region string) AWSProviderSpecBuilder {
	m.region = &region
	return m
}

// WithSecurityGroups sets the securityGroups for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithSecurityGroups(sgs []machinev1beta1.AWSResourceReference) AWSProviderSpecBuilder {
	m.securityGroups = &sgs
	return m
}

// WithSpotMarketOptions sets the SpotMarketOptions for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithSpotMarketOptions(opts *machinev1beta1.SpotMarketOptions) AWSProviderSpecBuilder {
	m.spotMarketOptions = &opts
	return m
}

// WithSubnet sets the subnet for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithSubnet(subnet machinev1beta1.AWSResourceReference) AWSProviderSpecBuilder {
	m.subnet = &subnet
	return m
}

// WithTags sets the tags for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithTags(tags []machinev1beta1.TagSpecification) AWSProviderSpecBuilder {
	m.tags = &tags
	return m
}

// WithUserDataSecret sets the UserDataSecret for the AWS machine config builder.
func (m AWSProviderSpecBuilder) WithUserDataSecret(userDataSecret *corev1.LocalObjectReference) AWSProviderSpecBuilder {
	m.userDataSecret = &userDataSecret
	return m
}
