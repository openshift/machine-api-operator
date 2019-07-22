package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConfigMapNodeUnhealthyConditions contains the name of the unhealthy conditions config map
const ConfigMapNodeUnhealthyConditions = "node-unhealthy-conditions"

// RemediationStrategyType contains remediation strategy type
type RemediationStrategyType string

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineHealthCheck is the Schema for the machinehealthchecks API
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=mhc;mhcs
// +k8s:openapi-gen=true
type MachineHealthCheck struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Specification of machine health check policy
	Spec MachineHealthCheckSpec `json:"spec,omitempty"`

	// Most recently observed status of MachineHealthCheck resource
	Status MachineHealthCheckStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineHealthCheckList contains a list of MachineHealthCheck
type MachineHealthCheckList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []MachineHealthCheck `json:"items"`
}

// UnhealthyNodeCondition defines a condition under which a node is considered unhealthy
type UnhealthyNodeCondition struct {
	Name    corev1.NodeConditionType `json:"name"`
	Status  corev1.ConditionStatus   `json:"status"`
	Timeout metav1.Duration          `json:"timeout"`
}

// MachineHealthCheckSpec defines the desired state of MachineHealthCheck
type MachineHealthCheckSpec struct {
	// RemediationStrategy to use in case of problem detection
	// default is machine deletion
	// +optional
	RemediationStrategy *RemediationStrategyType `json:"remediationStrategy,omitempty"`

	// Label selector to match machines whose health will be exercised
	Selector metav1.LabelSelector `json:"selector"`

	// List of conditions under which a machine is considered unhealthy
	UnhealthyNodeConditions []UnhealthyNodeCondition `json:"unhealthyNodeConditions"`
}

// MachineHealthCheckStatus defines the observed state of MachineHealthCheck
type MachineHealthCheckStatus struct {
	// TODO(alberto)
}
