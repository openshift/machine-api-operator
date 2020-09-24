package operator

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic"

	"k8s.io/klog"
)

const genericCABundleInjectorLabel = "service.beta.openshift.io/inject-cabundle"

// applyMutatingWebhookConfiguration ensures the form of the specified
// mutatingwebhookconfiguration is present in the API. If it does not exist,
// it will be created. If it does exist, the metadata of the required
// mutatingwebhookconfiguration will be merged with the existing mutatingwebhookconfiguration
// and an update performed if the mutatingwebhookconfiguration spec and metadata differ from
// the previously required spec and metadata. For further detail, check the top-level comment.
func applyMutatingWebhookConfiguration(client dynamic.Interface, recorder events.Recorder,
	requiredOriginal *admissionregistrationv1.MutatingWebhookConfiguration, expectedGeneration int64) (*admissionregistrationv1.MutatingWebhookConfiguration, bool, error) {

	gvr := admissionregistrationv1.SchemeGroupVersion.WithResource("mutatingwebhookconfigurations")
	resourcedClient := client.Resource(gvr)

	required := requiredOriginal.DeepCopy()
	// Providing upgrade compatibility with service-ca-bundle operator
	// and ignore clientConfig.caBundle changes on "inject-cabundle" label
	if required != nil && required.GetAnnotations() != nil && required.GetAnnotations()[genericCABundleInjectorLabel] != "" {
		if err := copyMutatingWebhookCABundle(resourcedClient, required); err != nil {
			return nil, false, err
		}
	}

	// Explictily specify type for required webhook configuration to get object meta accessor
	requiredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(required)
	if err != nil {
		return nil, false, err
	}
	requiredUnstr := &unstructured.Unstructured{Object: requiredObj}

	actualUnstr, modified, err := applyUnstructured(resourcedClient, "webhooks", recorder, requiredUnstr, expectedGeneration)
	if err != nil {
		return nil, modified, err
	}

	actual := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(actualUnstr.Object, actual); err != nil {
		return nil, modified, err
	}
	return actual, modified, nil
}

func copyMutatingWebhookCABundle(resourceClient dynamic.ResourceInterface, required *admissionregistrationv1.MutatingWebhookConfiguration) error {
	existingUnstr, err := resourceClient.Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	existing := &admissionregistrationv1.MutatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existingUnstr.Object, existing); err != nil {
		return err
	}

	existingMutatingWebhooks := make(map[string]admissionregistrationv1.MutatingWebhook)
	for _, mutatingWebhook := range existing.Webhooks {
		existingMutatingWebhooks[mutatingWebhook.Name] = mutatingWebhook
	}

	webhooks := make([]admissionregistrationv1.MutatingWebhook, len(required.Webhooks))
	for i, mutatingWebhook := range required.Webhooks {
		if webhook, ok := existingMutatingWebhooks[mutatingWebhook.Name]; ok {
			mutatingWebhook.ClientConfig.CABundle = webhook.ClientConfig.CABundle
		}
		webhooks[i] = mutatingWebhook
	}
	required.Webhooks = webhooks
	return nil
}

// applyValidatingWebhookConfiguration ensures the form of the specified
// validatingwebhookconfiguration is present in the API. If it does not exist,
// it will be created. If it does exist, the metadata of the required
// validatingwebhookconfiguration will be merged with the existing validatingwebhookconfiguration
// and an update performed if the validatingwebhookconfiguration spec and metadata differ from
// the previously required spec and metadata. For further detail, check the top-level comment.
func applyValidatingWebhookConfiguration(client dynamic.Interface, recorder events.Recorder,
	requiredOriginal *admissionregistrationv1.ValidatingWebhookConfiguration, expectedGeneration int64) (*admissionregistrationv1.ValidatingWebhookConfiguration, bool, error) {

	gvr := admissionregistrationv1.SchemeGroupVersion.WithResource("validatingwebhookconfigurations")
	resourcedClient := client.Resource(gvr)

	required := requiredOriginal.DeepCopy()
	// Providing upgrade compatibility with service-ca-bundle operator
	// and ignore clientConfig.caBundle changes on "inject-cabundle" label
	if required != nil && required.GetAnnotations() != nil && required.GetAnnotations()[genericCABundleInjectorLabel] != "" {
		if err := copyValidatingWebhookCABundle(resourcedClient, required); err != nil {
			return nil, false, err
		}
	}

	// Explictily specify type for required webhook configuration to get object meta accessor
	requiredObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(required)
	if err != nil {
		return nil, false, err
	}

	requiredUnstr := &unstructured.Unstructured{Object: requiredObj}

	actualUnstr, modified, err := applyUnstructured(resourcedClient, "webhooks", recorder, requiredUnstr, expectedGeneration)
	if err != nil {
		return nil, modified, err
	}

	actual := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(actualUnstr.Object, actual); err != nil {
		return nil, modified, err
	}
	return actual, modified, nil
}

func copyValidatingWebhookCABundle(resourceClient dynamic.ResourceInterface, required *admissionregistrationv1.ValidatingWebhookConfiguration) error {
	existingUnstr, err := resourceClient.Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	existing := &admissionregistrationv1.ValidatingWebhookConfiguration{}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(existingUnstr.Object, existing); err != nil {
		return err
	}

	existingValidatingWebhooks := make(map[string]admissionregistrationv1.ValidatingWebhook)
	for _, validatingWebhook := range existing.Webhooks {
		existingValidatingWebhooks[validatingWebhook.Name] = validatingWebhook
	}

	webhooks := make([]admissionregistrationv1.ValidatingWebhook, len(required.Webhooks))
	for i, validatingWebhook := range required.Webhooks {
		if webhook, ok := existingValidatingWebhooks[validatingWebhook.Name]; ok {
			validatingWebhook.ClientConfig.CABundle = webhook.ClientConfig.CABundle
		}
		webhooks[i] = validatingWebhook
	}
	required.Webhooks = webhooks
	return nil
}

// applyUnstructured ensures the form of the specified usntructured is present in the API.
// If it does not exist, it will be created. If it does exist, the metadata of the required
// usntructured will be merged with the existing usntructured and an update performed if the
// usntructured spec and metadata differ from the previously required spec and metadata.
// For further detail, check the top-level comment.
func applyUnstructured(resourceClient dynamic.ResourceInterface, path string, recorder events.Recorder,
	requiredOriginal *unstructured.Unstructured, expectedGeneration int64) (*unstructured.Unstructured, bool, error) {
	if requiredOriginal == nil {
		return nil, false, fmt.Errorf("Unexpected nil instead of an object")
	}
	required := requiredOriginal.DeepCopy()

	requiredSpec, exists, err := unstructured.NestedFieldNoCopy(required.Object, path)
	if err != nil {
		return nil, false, err
	}
	if !exists {
		return nil, false, fmt.Errorf("Object does not contain the specified path: %s", path)
	}

	existing, err := resourceClient.Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			actual, err := resourceClient.Create(context.TODO(), required, metav1.CreateOptions{})
			reportCreateEvent(recorder, required, err)
			if err != nil {
				return nil, false, err
			}
			return actual, true, nil
		}
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()
	modified := ensureObjectMeta(existingCopy, required)
	if !modified && existingCopy.GetGeneration() == expectedGeneration {
		return existingCopy, false, nil
	}
	if err := unstructured.SetNestedField(existingCopy.Object, requiredSpec, path); err != nil {
		return nil, false, err
	}

	// at this point we know that we're going to perform a write.  We're just trying to get the object correct
	toWrite := existingCopy // shallow copy so the code reads easier

	klog.V(4).Infof("%s %q changes: %v", required.GetKind(), required.GetNamespace()+"/"+required.GetName(), jsonPatchNoError(existing, toWrite))

	actual, err := resourceClient.Update(context.TODO(), toWrite, metav1.UpdateOptions{})
	if err != nil {
		return nil, false, err
	}
	if actual.GetGeneration() > expectedGeneration || modified {
		reportUpdateEvent(recorder, required, err)
		return actual, true, nil
	}
	return actual, false, nil
}
