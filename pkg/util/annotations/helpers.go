// Package annotations implements annotation helper functions.
package annotations

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
)

// IsPaused returns true if the Cluster is paused or the object has the `paused` annotation.
func IsPaused(o metav1.Object) bool {
	return HasPausedAnnotation(o)
}

// HasPausedAnnotation returns true if the object has the `paused` annotation.
func HasPausedAnnotation(o metav1.Object) bool {
	return hasAnnotation(o, v1beta1.PausedAnnotation)
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
