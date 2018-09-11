package resourcemerge

import (
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/api/equality"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
)

// EnsureCustomResourceDefinition ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func EnsureCustomResourceDefinition(modified *bool, existing *apiextv1beta1.CustomResourceDefinition, required apiextv1beta1.CustomResourceDefinition) {
	EnsureObjectMeta(modified, &existing.ObjectMeta, required.ObjectMeta)

	// we stomp everything
	if !equality.Semantic.DeepEqual(existing.Spec, required.Spec) {
		*modified = true
		existing.Spec = required.Spec
	}
}

// EnsureAPIServiceDefinition ensures that the existing matches the required.
// modified is set to true when existing had to be updated with required.
func EnsureAPIServiceDefinition(modified *bool, existing *apiregistrationv1beta1.APIService, required apiregistrationv1beta1.APIService) {
	EnsureObjectMeta(modified, &existing.ObjectMeta, required.ObjectMeta)

	// we stomp everything
	if !equality.Semantic.DeepEqual(existing.Spec, required.Spec) {
		*modified = true
		existing.Spec = required.Spec
	}
}
