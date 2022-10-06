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
	// IPClaimFinalizer allows IPClaimReconciler to clean up resources
	// associated with IPClaim before removing it from the apiserver.
	IPClaimFinalizer = "ipclaim.ipam.metal3.io"
)

// IPClaimSpec defines the desired state of IPClaim.
type IPClaimSpec struct {

	// Pool is the IPPool this was generated from.
	Pool corev1.ObjectReference `json:"pool"`
}

// IPClaimStatus defines the observed state of IPClaim.
type IPClaimStatus struct {

	// Address is the IPAddress that was generated for this claim.
	Address *corev1.ObjectReference `json:"address,omitempty"`

	// ErrorMessage contains the error message
	ErrorMessage *string `json:"errorMessage,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:resource:path=ipclaims,scope=Namespaced,categories=cluster-api,shortName=ipc;ipclaim;m3ipc;m3ipclaim;m3ipclaims;metal3ipc;metal3ipclaim;metal3ipclaims
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of Metal3IPClaim"
// IPClaim is the Schema for the ipclaims API.
type IPClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPClaimSpec   `json:"spec,omitempty"`
	Status IPClaimStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// IPClaimList contains a list of IPClaim.
type IPClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPClaim{}, &IPClaimList{})
}
