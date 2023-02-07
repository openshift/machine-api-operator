/*
Copyright 2019 The Kubernetes Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// IPPoolFinalizer allows IPPoolReconciler to clean up resources
	// associated with IPPool before removing it from the apiserver.
	IPPoolFinalizer = "ippool.ipam.metal3.io"
)

// MetaDataIPAddress contains the info to render th ip address. It is IP-version
// agnostic.
type Pool struct {

	// Start is the first ip address that can be rendered
	Start *IPAddressStr `json:"start,omitempty"`

	// End is the last IP address that can be rendered. It is used as a validation
	// that the rendered IP is in bound.
	End *IPAddressStr `json:"end,omitempty"`

	// Subnet is used to validate that the rendered IP is in bounds. In case the
	// Start value is not given, it is derived from the subnet ip incremented by 1
	// (`192.168.0.1` for `192.168.0.0/24`)
	Subnet *IPSubnetStr `json:"subnet,omitempty"`

	// +kubebuilder:validation:Maximum=128
	// Prefix is the mask of the network as integer (max 128)
	Prefix int `json:"prefix,omitempty"`

	// Gateway is the gateway ip address
	Gateway *IPAddressStr `json:"gateway,omitempty"`

	// DNSServers is the list of dns servers
	DNSServers []IPAddressStr `json:"dnsServers,omitempty"`
}

// IPPoolSpec defines the desired state of IPPool.
type IPPoolSpec struct {

	// ClusterName is the name of the Cluster this object belongs to.
	ClusterName *string `json:"clusterName,omitempty"`

	// Pools contains the list of IP addresses pools
	Pools []Pool `json:"pools,omitempty"`

	// PreAllocations contains the preallocated IP addresses
	PreAllocations map[string]IPAddressStr `json:"preAllocations,omitempty"`

	// +kubebuilder:validation:Maximum=128
	// Prefix is the mask of the network as integer (max 128)
	Prefix int `json:"prefix,omitempty"`

	// Gateway is the gateway ip address
	Gateway *IPAddressStr `json:"gateway,omitempty"`

	// DNSServers is the list of dns servers
	DNSServers []IPAddressStr `json:"dnsServers,omitempty"`

	// +kubebuilder:validation:MinLength=1
	// namePrefix is the prefix used to generate the IPAddress object names
	NamePrefix string `json:"namePrefix"`
}

// IPPoolStatus defines the observed state of IPPool.
type IPPoolStatus struct {
	// LastUpdated identifies when this status was last observed.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`

	// Allocations contains the map of objects and IP addresses they have
	Allocations map[string]IPAddressStr `json:"indexes,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=ippools,scope=Namespaced,categories=cluster-api,shortName=ipp;ippool;m3ipp;m3ippool;m3ippools;metal3ipp;metal3ippool;metal3ippools
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this template belongs"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of Metal3IPPool"
// IPPool is the Schema for the ippools API.
type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPPoolSpec   `json:"spec,omitempty"`
	Status IPPoolStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IPPoolList contains a list of IPPool.
type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPPool{}, &IPPoolList{})
}
