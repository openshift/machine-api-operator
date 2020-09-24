package operator

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/kubernetes/typed/admissionregistration/v1"

	"k8s.io/klog"
)

const genericCABundleInjectorLabel = "service.beta.openshift.io/inject-cabundle"

// applyMutatingWebhookConfiguration ensures the form of the specified
// mutatingwebhookconfiguration is present in the API. If it does not exist,
// it will be created. If it does exist, the metadata of the required
// mutatingwebhookconfiguration will be merged with the existing mutatingwebhookconfiguration
// and an update performed if the mutatingwebhookconfiguration spec and metadata differ from
// the previously required spec and metadata based on generation change.
func applyMutatingWebhookConfiguration(client v1.MutatingWebhookConfigurationInterface, recorder events.Recorder,
	requiredOriginal *admissionregistrationv1.MutatingWebhookConfiguration, expectedGeneration int64) (*admissionregistrationv1.MutatingWebhookConfiguration, bool, error) {
	if requiredOriginal == nil {
		return nil, false, fmt.Errorf("Unexpected nil instead of an object")
	}
	required := requiredOriginal.DeepCopy()

	existing, err := client.Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			actual, err := client.Create(context.TODO(), required, metav1.CreateOptions{})
			reportCreateEvent(recorder, required, err)
			if err != nil {
				return nil, false, err
			}
			return actual, true, nil
		}
		return nil, false, err
	}

	// Providing upgrade compatibility with service-ca-bundle operator
	// and ignore clientConfig.caBundle changes on "inject-cabundle" label
	if required.GetAnnotations() != nil && required.GetAnnotations()[genericCABundleInjectorLabel] != "" {
		copyMutatingWebhookCABundle(existing, required)
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	if !*modified && existingCopy.GetGeneration() == expectedGeneration {
		return existingCopy, false, nil
	}
	// at this point we know that we're going to perform a write.  We're just trying to get the object correct
	toWrite := existingCopy // shallow copy so the code reads easier
	toWrite.Webhooks = required.Webhooks

	klog.V(4).Infof("MutatingWebhookConfiguration %q changes: %v", required.GetNamespace()+"/"+required.GetName(), jsonPatchNoError(existing, toWrite))

	actual, err := client.Update(context.TODO(), toWrite, metav1.UpdateOptions{})
	if err != nil {
		return nil, false, err
	}
	if *modified || actual.GetGeneration() > expectedGeneration {
		reportUpdateEvent(recorder, required, err)
		return actual, true, nil
	}
	return actual, false, nil
}

func copyMutatingWebhookCABundle(existing, required *admissionregistrationv1.MutatingWebhookConfiguration) {
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
}

// applyValidatingWebhookConfiguration ensures the form of the specified
// validatingwebhookconfiguration is present in the API. If it does not exist,
// it will be created. If it does exist, the metadata of the required
// validatingwebhookconfiguration will be merged with the existing validatingwebhookconfiguration
// and an update performed if the validatingwebhookconfiguration spec and metadata differ from
// the previously required spec and metadata based on generation change.
func applyValidatingWebhookConfiguration(client v1.ValidatingWebhookConfigurationInterface, recorder events.Recorder,
	requiredOriginal *admissionregistrationv1.ValidatingWebhookConfiguration, expectedGeneration int64) (*admissionregistrationv1.ValidatingWebhookConfiguration, bool, error) {
	if requiredOriginal == nil {
		return nil, false, fmt.Errorf("Unexpected nil instead of an object")
	}
	required := requiredOriginal.DeepCopy()

	existing, err := client.Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			actual, err := client.Create(context.TODO(), required, metav1.CreateOptions{})
			reportCreateEvent(recorder, required, err)
			if err != nil {
				return nil, false, err
			}
			return actual, true, nil
		}
		return nil, false, err
	}

	// Providing upgrade compatibility with service-ca-bundle operator
	// and ignore clientConfig.caBundle changes on "inject-cabundle" label
	if required.GetAnnotations() != nil && required.GetAnnotations()[genericCABundleInjectorLabel] != "" {
		copyValidatingWebhookCABundle(existing, required)
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()

	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	if !*modified && existingCopy.GetGeneration() == expectedGeneration {
		return existingCopy, false, nil
	}
	// at this point we know that we're going to perform a write.  We're just trying to get the object correct
	toWrite := existingCopy // shallow copy so the code reads easier
	toWrite.Webhooks = required.Webhooks

	klog.V(4).Infof("ValidatingWebhookConfiguration %q changes: %v", required.GetNamespace()+"/"+required.GetName(), jsonPatchNoError(existing, toWrite))

	actual, err := client.Update(context.TODO(), toWrite, metav1.UpdateOptions{})
	if err != nil {
		return nil, false, err
	}
	if *modified || actual.GetGeneration() > expectedGeneration {
		reportUpdateEvent(recorder, required, err)
		return actual, true, nil
	}
	return actual, false, nil
}

func copyValidatingWebhookCABundle(existing, required *admissionregistrationv1.ValidatingWebhookConfiguration) {
	existingMutatingWebhooks := make(map[string]admissionregistrationv1.ValidatingWebhook)
	for _, validatingWebhook := range existing.Webhooks {
		existingMutatingWebhooks[validatingWebhook.Name] = validatingWebhook
	}

	webhooks := make([]admissionregistrationv1.ValidatingWebhook, len(required.Webhooks))
	for i, validatingWebhook := range required.Webhooks {
		if webhook, ok := existingMutatingWebhooks[validatingWebhook.Name]; ok {
			validatingWebhook.ClientConfig.CABundle = webhook.ClientConfig.CABundle
		}
		webhooks[i] = validatingWebhook
	}
	required.Webhooks = webhooks
}
