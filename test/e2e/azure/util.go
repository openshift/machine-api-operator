package azure

import (
	"encoding/json"
	"fmt"

	. "github.com/onsi/gomega"
	"github.com/openshift/api/machine/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"

	util "github.com/openshift/machine-api-operator/test/e2e"
)

// RawExtensionFromProviderSpec marshals the Azure machine provider spec.
func RawExtensionFromProviderSpec(spec *v1beta1.AzureMachineProviderSpec) (*runtime.RawExtension, error) {
	if spec == nil {
		return &runtime.RawExtension{}, nil
	}

	var rawBytes []byte
	var err error
	if rawBytes, err = json.Marshal(spec); err != nil {
		return nil, fmt.Errorf("error marshalling providerSpec: %v", err)
	}

	return &runtime.RawExtension{
		Raw: rawBytes,
	}, nil
}

// ProviderSpecFromRawExtension unmarshals the JSON-encoded spec
func ProviderSpecFromRawExtension(rawExtension *runtime.RawExtension) (*v1beta1.AzureMachineProviderSpec, error) {
	if rawExtension == nil {
		return &v1beta1.AzureMachineProviderSpec{}, nil
	}

	spec := new(v1beta1.AzureMachineProviderSpec)
	if err := json.Unmarshal(rawExtension.Raw, &spec); err != nil {
		return nil, fmt.Errorf("error unmarshalling providerSpec: %v", err)
	}

	return spec, nil
}

// getProviderFromMachineSet retrieves the Azure provider spec from an existing worker machineset.
// This is used as a template for creating test machines and machinesets.
func getProviderFromMachineSet(cfg *rest.Config) *v1beta1.AzureMachineProviderSpec {
	workerMachineSets, err := util.GetMachineSets(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(len(workerMachineSets.Items)).NotTo(Equal(0), "cluster should have at least 1 worker machine set created by installer")

	provider, err := ProviderSpecFromRawExtension(workerMachineSets.Items[0].Spec.Template.Spec.ProviderSpec.Value)
	Expect(err).NotTo(HaveOccurred())

	return provider
}
