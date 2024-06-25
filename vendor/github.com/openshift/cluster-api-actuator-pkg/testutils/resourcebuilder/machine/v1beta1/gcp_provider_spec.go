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
)

// GCPProviderSpec creates a new GCP machine config builder.
func GCPProviderSpec() GCPProviderSpecBuilder {
	return GCPProviderSpecBuilder{
		machineType: "n1-standard-4",
		targetPools: []string{"target-pool-1", "target-pool-2"},
		zone:        "us-central1-a",
	}
}

// GCPProviderSpecBuilder is used to build a GCP machine config object.
type GCPProviderSpecBuilder struct {
	machineType string
	targetPools []string
	zone        string
}

// Build builds a new GCP machine config based on the configuration provided.
func (m GCPProviderSpecBuilder) Build() *machinev1beta1.GCPMachineProviderSpec {
	return &machinev1beta1.GCPMachineProviderSpec{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "GCPMachineProviderSpec",
		},
		MachineType: m.machineType,
		UserDataSecret: &corev1.LocalObjectReference{
			Name: "gcp-user-data-12345678",
		},
		TargetPools:        m.targetPools,
		DeletionProtection: false,
		NetworkInterfaces: []*machinev1beta1.GCPNetworkInterface{{
			Network:    "gcp-network-12345678",
			Subnetwork: "gcp-subnetwork-12345678",
		}},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: "gcp-cloud-credentials",
		},
		Zone:         m.zone,
		CanIPForward: false,
		ProjectID:    "openshift-cpms-unit-tests",
		Region:       "us-central1",
		Disks: []*machinev1beta1.GCPDisk{
			{
				AutoDelete: true,
				Boot:       true,
				Image:      "projects/rhcos-cloud/global/images/rhcos-411-85-202205101201-0-gcp-x86-64",
				SizeGB:     128,
				Type:       "pd-ssd",
			},
		},
		Tags: []string{
			"gcp-tag-12345678",
		},
		ServiceAccounts: []machinev1beta1.GCPServiceAccount{
			{
				Email: "service-account-12345678",
				Scopes: []string{
					"https://www.googleapis.com/auth/cloud-platform",
				},
			},
		},
	}
}

// BuildRawExtension builds a new GCP machine config based on the configuration provided.
func (m GCPProviderSpecBuilder) BuildRawExtension() *runtime.RawExtension {
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

// WithMachineType sets the machine type for the GCP machine config builder.
func (m GCPProviderSpecBuilder) WithMachineType(machineType string) GCPProviderSpecBuilder {
	m.machineType = machineType
	return m
}

// WithTargetPools sets the target pools for the GCP machine config builder.
func (m GCPProviderSpecBuilder) WithTargetPools(targetPools []string) GCPProviderSpecBuilder {
	m.targetPools = targetPools
	return m
}

// WithZone sets the zone for the GCP machine config builder.
func (m GCPProviderSpecBuilder) WithZone(zone string) GCPProviderSpecBuilder {
	m.zone = zone
	return m
}
