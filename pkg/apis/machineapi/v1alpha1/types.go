package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorsv1 "github.com/openshift/api/operator/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineAPIOperatorConfig provides information to configure an operator to manage openshift cluster api.
type MachineAPIOperatorConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   MachineAPIOperatorConfigSpec   `json:"spec"`
	Status MachineAPIOperatorConfigStatus `json:"status"`
}

type MachineAPIOperatorConfigSpec struct {
	operatorsv1.OperatorSpec `json:",inline"`
}

type MachineAPIOperatorConfigStatus struct {
	operatorsv1.OperatorStatus `json:",inline"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineAPIOperatorConfigList is a collection of items
type MachineAPIOperatorConfigList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard object's metadata.
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items contains the items
	Items []MachineAPIOperatorConfig `json:"items"`
}
