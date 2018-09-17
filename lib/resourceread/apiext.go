package resourceread

import (
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
)

var (
	apiExtensionsScheme   = runtime.NewScheme()
	apiExtensionsCodecs   = serializer.NewCodecFactory(apiExtensionsScheme)
	apiRegistrationScheme = runtime.NewScheme()
	apiRegistrationCodecs = serializer.NewCodecFactory(apiRegistrationScheme)
)

func init() {
	if err := apiextv1beta1.AddToScheme(apiExtensionsScheme); err != nil {
		panic(err)
	}
	if err := apiregistrationv1beta1.AddToScheme(apiRegistrationScheme); err != nil {
		panic(err)
	}
}

// ReadCustomResourceDefinitionV1Beta1OrDie reads crd object from bytes. Panics on error.
func ReadCustomResourceDefinitionV1Beta1OrDie(objBytes []byte) *apiextv1beta1.CustomResourceDefinition {
	requiredObj, err := runtime.Decode(apiExtensionsCodecs.UniversalDecoder(apiextv1beta1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return requiredObj.(*apiextv1beta1.CustomResourceDefinition)
}

// ReadAPIServiceDefinitionV1Beta1OrDie reads crd object from bytes. Panics on error.
func ReadAPIServiceDefinitionV1Beta1OrDie(objBytes []byte) *apiregistrationv1beta1.APIService {
	requiredObj, err := runtime.Decode(apiRegistrationCodecs.UniversalDecoder(apiregistrationv1beta1.SchemeGroupVersion), objBytes)
	if err != nil {
		panic(err)
	}
	return requiredObj.(*apiregistrationv1beta1.APIService)
}
