package operator

import (
	"fmt"
	"strings"

	patch "github.com/evanphx/json-patch"
	operatorsv1 "github.com/openshift/api/operator/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func reportCreateEvent(recorder events.Recorder, obj runtime.Object, originalErr error) {
	gvk := resourcehelper.GuessObjectGroupVersionKind(obj)
	if originalErr == nil {
		recorder.Eventf(fmt.Sprintf("%sCreated", gvk.Kind), "Created %s because it was missing", resourcehelper.FormatResourceForCLIWithNamespace(obj))
		return
	}
	recorder.Warningf(fmt.Sprintf("%sCreateFailed", gvk.Kind), "Failed to create %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(obj), originalErr)
}

func reportUpdateEvent(recorder events.Recorder, obj runtime.Object, originalErr error, details ...string) {
	gvk := resourcehelper.GuessObjectGroupVersionKind(obj)
	switch {
	case originalErr != nil:
		recorder.Warningf(fmt.Sprintf("%sUpdateFailed", gvk.Kind), "Failed to update %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(obj), originalErr)
	case len(details) == 0:
		recorder.Eventf(fmt.Sprintf("%sUpdated", gvk.Kind), "Updated %s because it changed", resourcehelper.FormatResourceForCLIWithNamespace(obj))
	default:
		recorder.Eventf(fmt.Sprintf("%sUpdated", gvk.Kind), "Updated %s:\n%s", resourcehelper.FormatResourceForCLIWithNamespace(obj), strings.Join(details, "\n"))
	}
}

// expectedMutatingWebhooksConfiguration returns last applied generation for MutatingWebhookConfiguration resource registered in operator
func expectedMutatingWebhooksConfiguration(name string, previousGenerations []operatorsv1.GenerationStatus) int64 {
	generation := resourcemerge.GenerationFor(previousGenerations, schema.GroupResource{Group: admissionregistrationv1.SchemeGroupVersion.Group, Resource: "mutatingwebhookconfigurations"}, "", name)
	if generation != nil {
		return generation.LastGeneration
	}
	return -1
}

// setMutatingWebhooksConfigurationGeneration updates operator generation status list with last applied generation for provided MutatingWebhookConfiguration resource
func setMutatingWebhooksConfigurationGeneration(generations *[]operatorsv1.GenerationStatus, actual *admissionregistrationv1.MutatingWebhookConfiguration) {
	if actual == nil {
		return
	}
	resourcemerge.SetGeneration(generations, operatorsv1.GenerationStatus{
		Group:          admissionregistrationv1.SchemeGroupVersion.Group,
		Resource:       "mutatingwebhookconfigurations",
		Name:           actual.Name,
		LastGeneration: actual.ObjectMeta.Generation,
	})
}

// expectedValidatingWebhooksConfiguration returns last applied generation for ValidatingWebhookConfiguration resource registered in operator
func expectedValidatingWebhooksConfiguration(name string, previousGenerations []operatorsv1.GenerationStatus) int64 {
	generation := resourcemerge.GenerationFor(previousGenerations, schema.GroupResource{Group: admissionregistrationv1.SchemeGroupVersion.Group, Resource: "validatingwebhookconfigurations"}, "", name)
	if generation != nil {
		return generation.LastGeneration
	}
	return -1
}

// setValidatingWebhooksConfigurationGeneration updates operator generation status list with last applied generation for provided ValidatingWebhookConfiguration resource
func setValidatingWebhooksConfigurationGeneration(generations *[]operatorsv1.GenerationStatus, actual *admissionregistrationv1.ValidatingWebhookConfiguration) {
	if actual == nil {
		return
	}
	resourcemerge.SetGeneration(generations, operatorsv1.GenerationStatus{
		Group:          admissionregistrationv1.SchemeGroupVersion.Group,
		Resource:       "validatingwebhookconfigurations",
		Name:           actual.Name,
		LastGeneration: actual.ObjectMeta.Generation,
	})
}

// jsonPatchNoError generates a JSON patch between original and modified objects and return the JSON as a string.
// Note:
//
// In case of error, the returned string will contain the error messages.
func jsonPatchNoError(original, modified runtime.Object) string {
	if original == nil {
		return "original object is nil"
	}
	if modified == nil {
		return "modified object is nil"
	}
	originalJSON, err := runtime.Encode(unstructured.UnstructuredJSONScheme, original)
	if err != nil {
		return fmt.Sprintf("unable to decode original to JSON: %v", err)
	}
	modifiedJSON, err := runtime.Encode(unstructured.UnstructuredJSONScheme, modified)
	if err != nil {
		return fmt.Sprintf("unable to decode modified to JSON: %v", err)
	}
	patchBytes, err := patch.CreateMergePatch(originalJSON, modifiedJSON)
	if err != nil {
		return fmt.Sprintf("unable to create JSON patch: %v", err)
	}
	return string(patchBytes)
}
