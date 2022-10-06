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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// DataFinalizer allows IPAddressReconciler to clean up resources
	// associated with IPAddress before removing it from the apiserver.
	IPAddressFinalizer = "ipaddress.ipam.metal3.io"
)

// IPAddressSpec defines the desired state of IPAddress.
type IPAddressSpec struct {

	// Claim points to the object the IPClaim was created for.
	Claim corev1.ObjectReference `json:"claim"`

	// Pool is the IPPool this was generated from.
	Pool corev1.ObjectReference `json:"pool"`

	// +kubebuilder:validation:Maximum=128
	// Prefix is the mask of the network as integer (max 128)
	Prefix int `json:"prefix,omitempty"`

	// Gateway is the gateway ip address
	Gateway *IPAddressStr `json:"gateway,omitempty"`

	// Address contains the IP address
	Address IPAddressStr `json:"address"`

	// DNSServers is the list of dns servers
	DNSServers []IPAddressStr `json:"dnsServers,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=ipaddresses,scope=Namespaced,categories=metal3,shortName=ipa;ipaddress;m3ipa;m3ipaddress;m3ipaddresses;metal3ipa;metal3ipaddress;metal3ipaddresses
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of Metal3IPAddress"
// +kubebuilder:storageversion
// +kubebuilder:object:root=true
// IPAddress is the Schema for the ipaddresses API.
type IPAddress struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec IPAddressSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// IPAddressList contains a list of IPAddress.
type IPAddressList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPAddress `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPAddress{}, &IPAddressList{})
}
