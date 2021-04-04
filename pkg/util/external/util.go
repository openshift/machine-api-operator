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

package external

import (
	"context"
	"fmt"

	mapiv1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apiserver/pkg/storage/names"

	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// TemplateSuffix is the object kind suffix used by infrastructure references associated
	// with MachineSet or MachineDeployments.
	TemplateSuffix = "Template"
)

// Get uses the client and reference to get an external, unstructured object.
func Get(ctx context.Context, c client.Client, ref *corev1.ObjectReference, namespace string) (*unstructured.Unstructured, error) {
	obj := new(unstructured.Unstructured)
	obj.SetAPIVersion(ref.APIVersion)
	obj.SetKind(ref.Kind)
	obj.SetName(ref.Name)
	key := client.ObjectKey{Name: obj.GetName(), Namespace: namespace}
	if err := c.Get(ctx, key, obj); err != nil {
		return nil, fmt.Errorf("failed to retrieve %s external object %q/%q: %w", obj.GetKind(), key.Namespace, key.Name, err)
	}
	return obj, nil
}

// GenerateTemplate input is everything needed to generate a new template.
type GenerateTemplateInput struct {
	// Template is the TemplateRef turned into an unstructured.
	// +required
	Template *unstructured.Unstructured

	// TemplateRef is a reference to the template that needs to be cloned.
	// +required
	TemplateRef *corev1.ObjectReference

	// Namespace is the Kubernetes namespace the cloned object should be created into.
	// +required
	Namespace string

	// OwnerRef is an optional OwnerReference to attach to the cloned object.
	// +optional
	OwnerRef *metav1.OwnerReference

	// Labels is an optional map of labels to be added to the object.
	// +optional
	Labels map[string]string
}

func GenerateTemplate(in *GenerateTemplateInput) (*unstructured.Unstructured, error) {
	template, found, err := unstructured.NestedMap(in.Template.Object, "spec", "template")
	if !found {
		return nil, fmt.Errorf("missing Spec.Template on %v %q", in.Template.GroupVersionKind(), in.Template.GetName())
	} else if err != nil {
		return nil, fmt.Errorf("failed to retrieve Spec.Template map on %v %q", in.Template.GroupVersionKind(), in.Template.GetName())
	}

	// Create the unstructured object from the template.
	to := &unstructured.Unstructured{Object: template}
	to.SetResourceVersion("")
	to.SetFinalizers(nil)
	to.SetUID("")
	to.SetSelfLink("")
	to.SetName(names.SimpleNameGenerator.GenerateName(in.Template.GetName() + "-"))
	to.SetNamespace(in.Namespace)

	if to.GetAnnotations() == nil {
		to.SetAnnotations(map[string]string{})
	}
	annotations := to.GetAnnotations()
	annotations[mapiv1.TemplateClonedFromNameAnnotation] = in.TemplateRef.Name
	annotations[mapiv1.TemplateClonedFromGroupKindAnnotation] = in.TemplateRef.GroupVersionKind().GroupKind().String()
	to.SetAnnotations(annotations)

	// Set the owner reference.
	if in.OwnerRef != nil {
		to.SetOwnerReferences([]metav1.OwnerReference{*in.OwnerRef})
	}

	// Set the object APIVersion.
	if to.GetAPIVersion() == "" {
		to.SetAPIVersion(in.Template.GetAPIVersion())
	}

	// Set the object Kind and strip the word "Template" if it's a suffix.
	if to.GetKind() == "" {
		to.SetKind(strings.TrimSuffix(in.Template.GetKind(), TemplateSuffix))
	}
	return to, nil
}
