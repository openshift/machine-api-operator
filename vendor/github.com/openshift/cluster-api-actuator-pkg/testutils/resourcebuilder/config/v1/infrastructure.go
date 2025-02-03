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
	"fmt"
	"path"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	configv1 "github.com/openshift/api/config/v1"
)

// Infrastructure creates a new infrastructure builder.
func Infrastructure() InfrastructureBuilder {
	return InfrastructureBuilder{
		name: "cluster",
	}
}

// InfrastructureBuilder is used to build out an infrastructure object.
type InfrastructureBuilder struct {
	generateName string
	name         string
	namespace    string
	labels       map[string]string
	spec         *configv1.InfrastructureSpec
	status       *configv1.InfrastructureStatus
}

// Build builds a new infrastructure object based on the configuration provided.
func (i InfrastructureBuilder) Build() *configv1.Infrastructure {
	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: i.generateName,
			Name:         i.name,
			Namespace:    i.namespace,
			Labels:       i.labels,
		},
	}

	if i.spec != nil {
		infra.Spec = *i.spec
	}

	if i.status != nil {
		infra.Status = *i.status
	}

	return infra
}

// AsAWS sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsAWS(name string, region string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type: "AWS",
			AWS:  &configv1.AWSPlatformSpec{},
		},
	}
	i.status = &configv1.InfrastructureStatus{
		InfrastructureName:     name,
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		EtcdDiscoveryDomain:    "",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		PlatformStatus: &configv1.PlatformStatus{
			Type: "AWS",
			AWS: &configv1.AWSPlatformStatus{
				Region: region,
			},
		},
	}

	return i
}

// AsAzure sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsAzure(name string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type:  "Azure",
			Azure: &configv1.AzurePlatformSpec{},
		},
	}
	i.status = &configv1.InfrastructureStatus{
		InfrastructureName:     name,
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		EtcdDiscoveryDomain:    "",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		PlatformStatus: &configv1.PlatformStatus{
			Type:  "Azure",
			Azure: &configv1.AzurePlatformStatus{},
		},
	}

	return i
}

// AsGCP sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsGCP(name string, region string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type: configv1.GCPPlatformType,
			GCP:  &configv1.GCPPlatformSpec{},
		},
	}
	i.status = &configv1.InfrastructureStatus{
		InfrastructureName:     name,
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		EtcdDiscoveryDomain:    "",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		PlatformStatus: &configv1.PlatformStatus{
			Type: configv1.GCPPlatformType,
			GCP: &configv1.GCPPlatformStatus{
				Region: region,
			},
		},
	}

	return i
}

// AsOpenStack sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsOpenStack(name string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type:      configv1.OpenStackPlatformType,
			OpenStack: &configv1.OpenStackPlatformSpec{},
		},
	}
	i.status = &configv1.InfrastructureStatus{
		InfrastructureName:     name,
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		EtcdDiscoveryDomain:    "",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		PlatformStatus: &configv1.PlatformStatus{
			Type: configv1.OpenStackPlatformType,
			OpenStack: &configv1.OpenStackPlatformStatus{
				APIServerInternalIPs: []string{"10.0.0.5"},
				IngressIPs:           []string{"10.0.0.7"},
			},
		},
	}

	return i
}

// AsNutanix sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsNutanix(name string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type: configv1.NutanixPlatformType,
			Nutanix: &configv1.NutanixPlatformSpec{
				PrismCentral: configv1.NutanixPrismEndpoint{
					Address: "https://pc0_address",
					Port:    9440,
				},
				PrismElements: []configv1.NutanixPrismElementEndpoint{
					{
						Name: "pe0",
						Endpoint: configv1.NutanixPrismEndpoint{
							Address: "pe0-address",
							Port:    9440,
						},
					},
				},
			},
		},
	}

	i.status = &configv1.InfrastructureStatus{
		InfrastructureName:     name,
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		EtcdDiscoveryDomain:    "",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		PlatformStatus: &configv1.PlatformStatus{
			Type: configv1.NutanixPlatformType,
			Nutanix: &configv1.NutanixPlatformStatus{
				APIServerInternalIPs: []string{"10.0.0.5"},
				IngressIPs:           []string{"10.0.0.7"},
			},
		},
	}

	return i
}

// AsNutanixWithFailureDomains returns a Nutanix infrastructure resource with failure domains.
// if failureDomains is nil, default failure domains will be applied to the resource which are
// compatible with machinev1beta1resourcebuilder default failure domain names.
func (i InfrastructureBuilder) AsNutanixWithFailureDomains(name string, failureDomains *[]configv1.NutanixFailureDomain) InfrastructureBuilder {
	infraBuilder := i.AsNutanix(name)

	if failureDomains != nil {
		infraBuilder.spec.PlatformSpec.Nutanix.FailureDomains = *failureDomains
	} else {
		infraBuilder.spec.PlatformSpec.Nutanix.FailureDomains = []configv1.NutanixFailureDomain{
			{
				Name: "fd-pe0",
				Cluster: configv1.NutanixResourceIdentifier{
					Type: configv1.NutanixIdentifierName,
					Name: ptr.To[string]("pe0"),
				},
				Subnets: []configv1.NutanixResourceIdentifier{{
					Type: configv1.NutanixIdentifierName,
					Name: ptr.To[string]("pe0-subnet"),
				}},
			},
			{
				Name: "fd-pe1",
				Cluster: configv1.NutanixResourceIdentifier{
					Type: configv1.NutanixIdentifierUUID,
					UUID: ptr.To[string]("0005a0f3-8f43-a0f5-02b7-3cecef194315"),
				},
				Subnets: []configv1.NutanixResourceIdentifier{{
					Type: configv1.NutanixIdentifierName,
					Name: ptr.To[string]("pe1-subnet"),
				}},
			},
			{
				Name: "fd-pe2",
				Cluster: configv1.NutanixResourceIdentifier{
					Type: configv1.NutanixIdentifierName,
					Name: ptr.To[string]("pe2"),
				},
				Subnets: []configv1.NutanixResourceIdentifier{{
					Type: configv1.NutanixIdentifierUUID,
					UUID: ptr.To[string]("a8938dc6-7659-6801-a688-e26020c68241"),
				}},
			},
		}
	}

	i.spec = infraBuilder.spec
	i.status = infraBuilder.status

	return i
}

// AsVSphere sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsVSphere(name string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type:    configv1.VSpherePlatformType,
			VSphere: &configv1.VSpherePlatformSpec{},
		},
	}
	i.status = &configv1.InfrastructureStatus{
		InfrastructureName:     name,
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		EtcdDiscoveryDomain:    "",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
		PlatformStatus: &configv1.PlatformStatus{
			Type: configv1.VSpherePlatformType,
			VSphere: &configv1.VSpherePlatformStatus{
				APIServerInternalIPs: []string{"10.0.0.5"},
				IngressIPs:           []string{"10.0.0.7"},
			},
		},
	}

	return i
}

// AsVSphereWithFailureDomains returns a VSphere infrastructure resource with failure domains.
// if failureDomains = nil, default failure domains will be applied to the resource which are
// compatible with machinev1beta1resourcebuilder default failure domain names.
func (i InfrastructureBuilder) AsVSphereWithFailureDomains(name string, failureDomains *[]configv1.VSpherePlatformFailureDomainSpec) InfrastructureBuilder {
	infraBuilder := i.AsVSphere(name)
	if failureDomains != nil {
		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains = *failureDomains
	} else {
		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains = []configv1.VSpherePlatformFailureDomainSpec{
			{
				Name:   "us-central1-a",
				Region: "us-central",
				Zone:   "1-a",
				Server: "vcenter.test.com",
				Topology: configv1.VSpherePlatformTopology{
					Datacenter:     "test-dc1",
					ComputeCluster: "/test-dc1/host/test-cluster-1",
					Networks: []string{
						"test-network-1",
					},
					Datastore:    "/test-dc1/datastore/test-datastore-1",
					ResourcePool: "/test-dc1/host/test-cluster-1/Resources",
				},
			},
			{
				Name:   "us-central1-b",
				Region: "us-central",
				Zone:   "1-b",
				Server: "vcenter.test.com",
				Topology: configv1.VSpherePlatformTopology{
					Datacenter:     "test-dc2",
					ComputeCluster: "/test-dc2/host/test-cluster-2",
					Networks: []string{
						"test-network-2",
					},
					Datastore:    "/test-dc2/datastore/test-datastore-2",
					ResourcePool: "/test-dc2/host/test-cluster-2/Resources",
				},
			},
			{
				Name:   "us-central1-c",
				Region: "us-central",
				Zone:   "1-c",
				Server: "vcenter.test.com",
				Topology: configv1.VSpherePlatformTopology{
					Datacenter:     "test-dc3",
					ComputeCluster: "/test-dc3/host/test-cluster-3",
					Networks: []string{
						"test-network-3",
					},
					Datastore:    "/test-dc3/datastore/test-datastore-3",
					ResourcePool: "/test-dc3/host/test-cluster-3/Resources",
				},
			},
		}
	}

	i.spec = infraBuilder.spec
	i.status = infraBuilder.status

	return i
}

func (i InfrastructureBuilder) WithVSphereVMHostZonal() InfrastructureBuilder {
	infraBuilder := i

	for n := range infraBuilder.spec.PlatformSpec.VSphere.FailureDomains {
		datacenter := "test-dc1"
		cluster := "test-cluster-1"
		datastore := "test-datastore-1"

		fdName := infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].Name

		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].Topology.Datacenter = datacenter
		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].Topology.ComputeCluster = path.Join("/", datacenter, "host", cluster)
		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].Topology.Datastore = path.Join("/", datacenter, "datastore", datastore)
		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].Topology.ResourcePool = path.Join("/", datacenter, "host", cluster, "Resources")

		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].ZoneAffinity = &configv1.VSphereFailureDomainZoneAffinity{
			Type: configv1.HostGroupFailureDomainZone,
			HostGroup: &configv1.VSphereFailureDomainHostGroup{
				VMGroup:    fmt.Sprintf("%s-vm-group", fdName),
				HostGroup:  fmt.Sprintf("%s-host-group", fdName),
				VMHostRule: fmt.Sprintf("%s-vm-host-rule", fdName),
			},
		}

		infraBuilder.spec.PlatformSpec.VSphere.FailureDomains[n].RegionAffinity = &configv1.VSphereFailureDomainRegionAffinity{Type: configv1.ComputeClusterFailureDomainRegion}
	}

	i.spec = infraBuilder.spec

	return i
}

// AsPowerVS sets the Status for the infrastructure builder.
func (i InfrastructureBuilder) AsPowerVS(name string) InfrastructureBuilder {
	i.spec = &configv1.InfrastructureSpec{
		PlatformSpec: configv1.PlatformSpec{
			Type:    configv1.PowerVSPlatformType,
			PowerVS: &configv1.PowerVSPlatformSpec{},
		},
	}
	i.status = &configv1.InfrastructureStatus{
		InfrastructureName: name,
		PlatformStatus: &configv1.PlatformStatus{
			Type:    configv1.PowerVSPlatformType,
			PowerVS: &configv1.PowerVSPlatformStatus{},
		},
		APIServerURL:           "https://api.test-cluster.test-domain:6443",
		APIServerInternalURL:   "https://api-int.test-cluster.test-domain:6443",
		ControlPlaneTopology:   configv1.HighlyAvailableTopologyMode,
		InfrastructureTopology: configv1.HighlyAvailableTopologyMode,
	}

	return i
}

// WithGenerateName sets the generateName for the infrastructure builder.
func (i InfrastructureBuilder) WithGenerateName(generateName string) InfrastructureBuilder {
	i.generateName = generateName
	return i
}

// WithInfrastructureName sets the infrastructureName in the status for the infrastructure builder.
func (i InfrastructureBuilder) WithInfrastructureName(infraName string) InfrastructureBuilder {
	if i.status == nil {
		i.status = &configv1.InfrastructureStatus{}
	}

	i.status.InfrastructureName = infraName

	return i
}

// WithLabel sets the labels for the infrastructure builder.
func (i InfrastructureBuilder) WithLabel(key, value string) InfrastructureBuilder {
	if i.labels == nil {
		i.labels = make(map[string]string)
	}

	i.labels[key] = value

	return i
}

// WithLabels sets the labels for the infrastructure builder.
func (i InfrastructureBuilder) WithLabels(labels map[string]string) InfrastructureBuilder {
	i.labels = labels
	return i
}

// WithName sets the name for the infrastructure builder.
func (i InfrastructureBuilder) WithName(name string) InfrastructureBuilder {
	i.name = name
	return i
}

// WithNamespace sets the namespace for the infrastructure builder.
func (i InfrastructureBuilder) WithNamespace(namespace string) InfrastructureBuilder {
	i.namespace = namespace
	return i
}

// WithPlatformStatus sets the platformStatus for the infrastructure builder.
func (i InfrastructureBuilder) WithPlatformStatus(ps configv1.PlatformStatus) InfrastructureBuilder {
	if i.status == nil {
		i.status = &configv1.InfrastructureStatus{}
	}

	i.status.PlatformStatus = &ps

	return i
}

// WithStatus sets the status for the infrastructure builder.
func (i InfrastructureBuilder) WithStatus(status configv1.InfrastructureStatus) InfrastructureBuilder {
	i.status = &status
	return i
}
