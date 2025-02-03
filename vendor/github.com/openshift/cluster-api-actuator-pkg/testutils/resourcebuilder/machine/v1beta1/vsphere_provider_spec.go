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
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	configv1resourcebuilder "github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder/config/v1"
)

// VSphereProviderSpec creates a new VSphere machine config builder.
func VSphereProviderSpec() VSphereProviderSpecBuilder {
	return VSphereProviderSpecBuilder{
		template: "/datacenter/vm/test-ln-xw89i22-c1627-rvtrn-rhcos",
	}
}

// VSphereProviderSpecBuilder is used to build out a VSphere machine config object.
type VSphereProviderSpecBuilder struct {
	template          string
	cpmsProviderSpec  bool
	failureDomainName string
	infrastructure    *configv1.Infrastructure
	ippool            bool
	tags              []string
}

// Build builds a new VSphere machine config based on the configuration provided.
func (v VSphereProviderSpecBuilder) Build() *machinev1beta1.VSphereMachineProviderSpec {
	var networkDevices []machinev1beta1.NetworkDeviceSpec

	if v.infrastructure == nil {
		v.infrastructure = configv1resourcebuilder.Infrastructure().AsVSphereWithFailureDomains("vsphere-test", nil).Build()
	}

	failureDomains := v.infrastructure.Spec.PlatformSpec.VSphere.FailureDomains

	workspace := &machinev1beta1.Workspace{
		Server:       "test-vcenter",
		Datacenter:   "test-datacenter",
		Datastore:    "test-datastore",
		ResourcePool: "/test-datacenter/hosts/test-cluster/resources",
	}
	networkDevices = []machinev1beta1.NetworkDeviceSpec{
		{
			NetworkName: "test-network",
		},
	}

	if v.ippool {
		networkDevices[0].AddressesFromPools = []machinev1beta1.AddressesFromPool{
			{
				Group:    "test",
				Resource: "IPpool",
				Name:     "test",
			},
		}
	}

	template := v.template

	switch {
	case v.cpmsProviderSpec && len(failureDomains) != 0:
		workspace = &machinev1beta1.Workspace{}
		networkDevices = nil
		template = ""
	case !v.cpmsProviderSpec && len(failureDomains) != 0:
		for _, vSphereFailureDomain := range failureDomains {
			if vSphereFailureDomain.Name == v.failureDomainName {
				workspace = &machinev1beta1.Workspace{
					Server:     vSphereFailureDomain.Server,
					Datacenter: vSphereFailureDomain.Topology.Datacenter,
					Datastore:  vSphereFailureDomain.Topology.Datastore,
					ResourcePool: fmt.Sprintf("%s/Resources",
						vSphereFailureDomain.Topology.ComputeCluster),
				}
				networkDevices[0].NetworkName = vSphereFailureDomain.Topology.Networks[0]
				template = v.template

				if vSphereFailureDomain.ZoneAffinity != nil && vSphereFailureDomain.ZoneAffinity.HostGroup != nil {
					workspace.VMGroup = vSphereFailureDomain.ZoneAffinity.HostGroup.VMGroup
				}

				break
			}
		}
	}

	return &machinev1beta1.VSphereMachineProviderSpec{
		TypeMeta: metav1.TypeMeta{
			Kind:       "VSphereMachineProviderSpec",
			APIVersion: "machine.openshift.io/v1beta1",
		},
		NumCoresPerSocket: 4,
		DiskGiB:           120,
		UserDataSecret: &v1.LocalObjectReference{
			Name: "master-user-data",
		},
		MemoryMiB: 16384,
		CredentialsSecret: &v1.LocalObjectReference{
			Name: "vsphere-cloud-credentials",
		},
		Network: machinev1beta1.NetworkSpec{
			Devices: networkDevices,
		},
		TagIDs:    v.tags,
		Workspace: workspace,
		NumCPUs:   4,
		Template:  template,
	}
}

// BuildRawExtension builds a new VSphere machine config based on the configuration provided.
func (v VSphereProviderSpecBuilder) BuildRawExtension() *runtime.RawExtension {
	providerConfig := v.Build()

	raw, err := json.Marshal(providerConfig)
	if err != nil {
		// As we are building the input to json.Marshal, this should never happen.
		panic(err)
	}

	return &runtime.RawExtension{
		Raw: raw,
	}
}

// AsControlPlaneMachineSetProviderSpec the control plane machine set providerConfig is derived from the
// infrastructure spec. when failure domains are used to populate the provider spec of descendant machines,
// the cpms provider spec workspace, template, and network are left uninitialized to prevent ambiguity as
// the provider spec is not used to populate the workspace, template, and network.
func (v VSphereProviderSpecBuilder) AsControlPlaneMachineSetProviderSpec() VSphereProviderSpecBuilder {
	v.cpmsProviderSpec = true
	return v
}

// WithInfrastructure sets the template for the VSphere machine config builder.
func (v VSphereProviderSpecBuilder) WithInfrastructure(infrastructure configv1.Infrastructure) VSphereProviderSpecBuilder {
	v.infrastructure = &infrastructure
	return v
}

// WithTemplate sets the template for the VSphere machine config builder.
func (v VSphereProviderSpecBuilder) WithTemplate(template string) VSphereProviderSpecBuilder {
	v.template = template
	return v
}

// WithTags sets the tags for the VSphere machine config builder.
func (v VSphereProviderSpecBuilder) WithTags(tags []string) VSphereProviderSpecBuilder {
	v.tags = tags
	return v
}

// WithZone sets the zone for the VSphere machine config builder.
func (v VSphereProviderSpecBuilder) WithZone(zone string) VSphereProviderSpecBuilder {
	v.failureDomainName = zone
	return v
}

// WithIPPool sets the ippool for the VSphere machine config builder.
func (v VSphereProviderSpecBuilder) WithIPPool() VSphereProviderSpecBuilder {
	v.ippool = true
	return v
}
