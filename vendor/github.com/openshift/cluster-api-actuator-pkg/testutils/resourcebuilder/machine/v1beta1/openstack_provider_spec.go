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

	machinev1alpha1 "github.com/openshift/api/machine/v1alpha1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// OpenStackProviderSpec creates a new OpenStack machine config builder.
func OpenStackProviderSpec() OpenStackProviderSpecBuilder {
	return OpenStackProviderSpecBuilder{
		flavor:                 "m1.large",
		availabilityZone:       "",
		rootVolume:             nil,
		serverGroupName:        "master",
		additionalBlockDevices: nil,
	}
}

// OpenStackProviderSpecBuilder is used to build a OpenStack machine config object.
type OpenStackProviderSpecBuilder struct {
	flavor                 string
	availabilityZone       string
	rootVolume             *machinev1alpha1.RootVolume
	serverGroupName        string
	additionalBlockDevices []machinev1alpha1.AdditionalBlockDevice
}

// Build builds a new OpenStack machine config based on the configuration provided.
func (m OpenStackProviderSpecBuilder) Build() *machinev1alpha1.OpenstackProviderSpec {
	return &machinev1alpha1.OpenstackProviderSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1alpha1",
			Kind:       "OpenstackProviderSpec",
		},
		AvailabilityZone: m.availabilityZone,
		CloudsSecret: &v1.SecretReference{
			Name:      "openstack-cloud-credentials",
			Namespace: "openshift-machine-api",
		},
		CloudName: "openstack",
		Flavor:    m.flavor,
		Image:     "rhcos",
		Networks: []machinev1alpha1.NetworkParam{
			{
				Subnets: []machinev1alpha1.SubnetParam{
					{
						Filter: machinev1alpha1.SubnetFilter{
							ID: "810c3d97-98c2-4cf3-b0f6-8977b6e0b4b2",
						},
					},
				},
				UUID: "d06af90b-1677-4b35-a7fb-3ae023dc8f62",
			},
		},
		PrimarySubnet: "810c3d97-98c2-4cf3-b0f6-8977b6e0b4b2",
		SecurityGroups: []machinev1alpha1.SecurityGroupParam{
			{
				Filter: machinev1alpha1.SecurityGroupFilter{
					Name: "test-cluster-worker",
				},
			},
		},
		ServerGroupName: m.serverGroupName,
		ServerMetadata: map[string]string{
			"Name":               "test-cluster-worker",
			"openshiftClusterID": "test-cluster",
		},
		Tags: []string{
			"openshiftClusterID=test-cluster",
		},
		Trunk: true,
		UserDataSecret: &v1.SecretReference{
			Name: "worker-user-data",
		},
		RootVolume:             m.rootVolume,
		AdditionalBlockDevices: m.additionalBlockDevices,
	}
}

// BuildRawExtension builds a new OpenStack machine config based on the configuration provided.
func (m OpenStackProviderSpecBuilder) BuildRawExtension() *runtime.RawExtension {
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

// WithZone sets the availabilityZone for the OpenStack machine config builder.
func (m OpenStackProviderSpecBuilder) WithZone(az string) OpenStackProviderSpecBuilder {
	m.availabilityZone = az
	return m
}

// WithRootVolume sets the rootVolume for the OpenStack machine config builder.
func (m OpenStackProviderSpecBuilder) WithRootVolume(rootVolume *machinev1alpha1.RootVolume) OpenStackProviderSpecBuilder {
	m.rootVolume = rootVolume
	return m
}

// WithFlavor sets the flavor for the OpenStack machine config builder.
func (m OpenStackProviderSpecBuilder) WithFlavor(flavor string) OpenStackProviderSpecBuilder {
	m.flavor = flavor
	return m
}

// WithServerGroupName sets the server group name for the OpenStack machine config builder.
func (m OpenStackProviderSpecBuilder) WithServerGroupName(name string) OpenStackProviderSpecBuilder {
	m.serverGroupName = name
	return m
}

// WithAdditionalBlockDevices sets the additional block devices for the OpenStack machine config builder.
func (m OpenStackProviderSpecBuilder) WithAdditionalBlockDevices(additionalBlockDevices []machinev1alpha1.AdditionalBlockDevice) OpenStackProviderSpecBuilder {
	m.additionalBlockDevices = additionalBlockDevices
	return m
}
