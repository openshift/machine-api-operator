package util

import (
	"errors"
)

const (
	// Deprecated OpenShift introduced annotations for scaling from zero.
	CpuKeyDeprecated      = "machine.openshift.io/vCPU"
	MemoryKeyDeprecated   = "machine.openshift.io/memoryMb"
	GpuCountKeyDeprecated = "machine.openshift.io/GPU"
	MaxPodsKeyDeprecated  = "machine.openshift.io/maxPods"

	// Upstream preferred annotations for scaling from zero.
	CpuKey      = "capacity.cluster-autoscaler.kubernetes.io/cpu"
	MemoryKey   = "capacity.cluster-autoscaler.kubernetes.io/memory"
	GpuTypeKey  = "capacity.cluster-autoscaler.kubernetes.io/gpu-type"
	GpuCountKey = "capacity.cluster-autoscaler.kubernetes.io/gpu-count"
	MaxPodsKey  = "capacity.cluster-autoscaler.kubernetes.io/maxPods"

	GpuNvidiaType = "nvidia.com/gpu"
)

// This module's intended use is to perform changes and basic checks
// to scale from zero annotations on MachineSets. Example use of the module:
//
// ann := machineSet.Annotations
// ann = SetCpuAnnotation(ann, 10)
// ann = SetMemoryAnnotation(ann, 10)
// if isScaleFromZeroAnnotations := HasScaleFromZeroAnnotationsEnabled(ann); !isScaleFromZeroAnnotations { ... }

var (
	// errAnnotationKeyNotFound signals to the user that the selected key was not found in the MachineSet's annotations
	errAnnotationKeyNotFound = errors.New("could not find the selected annotation key in the MachineSet's annotations")
)

// ParseMachineSetAnnotationKey parses MachineSet's annotations and look for a key
func ParseMachineSetAnnotationKey(annotations map[string]string, key string) (string, error) {
	if val, exists := annotations[key]; exists && key != "" {
		return val, nil
	}

	return "", errAnnotationKeyNotFound
}

// HasScaleFromZeroAnnotationsEnabled checks that cpu and memory upstream annotations are set in a MachineSet.
func HasScaleFromZeroAnnotationsEnabled(annotations map[string]string) bool {
	cpu := annotations[CpuKey]
	mem := annotations[MemoryKey]

	if cpu != "" && mem != "" {
		return true
	}
	return false
}

// SetCpuAnnotation sets a value for a cpu key in the annotations of a MachineSet.
func SetCpuAnnotation(annotations map[string]string, value string) map[string]string {
	annotations[CpuKey] = value

	return annotations
}

// SetMemoryAnnotation sets a value for a mempory key in the annotations of a MachineSet.
func SetMemoryAnnotation(annotations map[string]string, value string) map[string]string {
	annotations[MemoryKey] = value

	return annotations
}

// SetGpuCountAnnotation sets a value for a gpu count key in the annotations of a MachineSet.
func SetGpuCountAnnotation(annotations map[string]string, value string) map[string]string {
	annotations[GpuCountKey] = value

	return annotations
}

// SetGpuTypeAnnotation sets a value for gpu type in the annotations of a MachineSet.
// Currently, we only support nvidia as a gpu type.
func SetGpuTypeAnnotation(annotations map[string]string, _ string) map[string]string {
	// TODO: Once we introduce proper gpu types, this needs to be changed.
	annotations[GpuTypeKey] = GpuNvidiaType

	return annotations
}

// SetMaxPodsAnnotation sets a value for a maxPods key in the annotations of a MachineSet.
func SetMaxPodsAnnotation(annotations map[string]string, value string) map[string]string {
	annotations[MaxPodsKey] = value

	return annotations
}
