package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MachineHealthy indicates if the machine is healthy or unhealthy
type MachineHealthy string

const (
	// MachineHealthyTrue indicates when the machine is healthy
	MachineHealthyTrue MachineHealthy = "True"
	// MachineHealthyFalse indicates when the machine is unhealthy
	MachineHealthyFalse MachineHealthy = "False"
)

// ConfigMapNodeUnhealthyConditions contains the name of the unhealthy conditions config map
const ConfigMapNodeUnhealthyConditions = "node-unhealthy-conditions"

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// MachineHealthCheck is the Schema for the machinehealthchecks API
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

// MachineHealthCheckSpec defines the desired state of MachineHealthCheck
type MachineHealthCheckSpec struct {
	// Label selector to match machines whose health will be exercised
	Selector metav1.LabelSelector `json:"selector"`
}

// MachineHealthCheckStatus defines the observed state of MachineHealthCheck
type MachineHealthCheckStatus struct {
	TargetedMachines     []TargetedMachine   `json:"targetedMachines"`
	TargetedConditions   []TargetedCondition `json:"targetedConditions"`
	TotalHealthyMachines int32               `json:"totalHealthyMachines"`
}

// TargetedMachine defines machines observed by the machine health check object
type TargetedMachine struct {
	Name                string                     `json:"name"`
	Healthy             MachineHealthy             `json:"healthy"`
	UnhealthyConditions []corev1.NodeConditionType `json:"unhealthyConditions"`
}

// TargetedCondition defines conditions observed by the machine health check object
type TargetedCondition struct {
	Name   corev1.NodeConditionType `json:"name"`
	Status corev1.ConditionStatus   `json:"status"`
}
