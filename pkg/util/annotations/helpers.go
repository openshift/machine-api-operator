// Package annotations implements annotation helper functions.
package annotations

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// PausedAnnotation is an annotation that can be applied to MachineHealthCheck objects to prevent the MHC controller
	// from processing it.
	// TODO: move this annotation to the openshift/api package
	PausedAnnotation = "cluster.x-k8s.io/paused"
)

// IsPaused returns true if the Cluster is paused or the object has the `paused` annotation.
func IsPaused(o metav1.Object) bool {
	return HasPausedAnnotation(o)
}

// HasPausedAnnotation returns true if the object has the `paused` annotation.
func HasPausedAnnotation(o metav1.Object) bool {
	return hasAnnotation(o, PausedAnnotation)
}

// hasAnnotation returns true if the object has the specified annotation.
func hasAnnotation(o metav1.Object, annotation string) bool {
	annotations := o.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, ok := annotations[annotation]
	return ok
}
