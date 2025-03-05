package vsphere

import (
	. "github.com/onsi/gomega"
	"github.com/openshift/api/machine/v1beta1"
	util "github.com/openshift/machine-api-operator/test/e2e"
	"k8s.io/client-go/rest"

	"github.com/openshift/machine-api-operator/pkg/controller/vsphere"
)

func getProviderFromMachineSet(cfg *rest.Config) *v1beta1.VSphereMachineProviderSpec {
	workerMachineSets, err := util.GetMachineSets(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(len(workerMachineSets.Items)).NotTo(Equal(0), "cluster should have at least 1 worker machine set created by installer")

	provider, err := vsphere.ProviderSpecFromRawExtension(workerMachineSets.Items[0].Spec.Template.Spec.ProviderSpec.Value)
	Expect(err).NotTo(HaveOccurred())

	return provider
}
