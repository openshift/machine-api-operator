package vsphere

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/api/machine/v1beta1"
	machinesetclient "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	"github.com/openshift/machine-api-operator/pkg/controller/vsphere"
	util "github.com/openshift/machine-api-operator/test/e2e"
)

const (
	machineRole         = "e2e-test"
	machineReadyTimeout = time.Minute * 6
)

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:VSphereMultiDisk][platform:vsphere] Managed cluster should", func() {
	defer GinkgoRecover()
	ctx := context.Background()

	var (
		cfg *rest.Config
		c   *kubernetes.Clientset
		dc  *dynamic.DynamicClient
		mc  *machinesetclient.MachineV1beta1Client
		err error
	)

	BeforeEach(func() {
		cfg, err = e2e.LoadConfig()
		Expect(err).NotTo(HaveOccurred())
		c, err = e2e.LoadClientset()
		Expect(err).NotTo(HaveOccurred())
		dc, err = dynamic.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		mc, err = machinesetclient.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
	})

	It("create machines with data disks [apigroup:machine.openshift.io]", func() {
		machineName := "machine-multi-test"
		dataDisks := []v1beta1.VSphereDisk{
			{
				Name:             "thinDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeThin,
			},
			{
				Name:             "thickDataDisk",
				SizeGiB:          2,
				ProvisioningMode: v1beta1.ProvisioningModeThick,
			},
			{
				Name:             "zeroedDataDisk",
				SizeGiB:          3,
				ProvisioningMode: v1beta1.ProvisioningModeEagerlyZeroed,
			},
			{
				Name:    "noModeDataDisk",
				SizeGiB: 3,
			},
		}

		By("checking for the openshift machine api operator")

		// skip if operator is not running
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		// get provider for simple definition vs generating one from scratch
		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		// Create new machineset to test
		By("creating new machineset with data disk configured")
		provider.DataDisks = []v1beta1.VSphereDisk{}
		provider.DataDisks = append(provider.DataDisks, dataDisks...)

		// Create new machine to test
		By("creating new machine with data disk configured")
		provRawData, err := vsphere.RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())
		machine, err := util.CreateMachine(ctx, cfg, mc, machineName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

		// Wait for machine to get ready
		By("verifying machine became ready")
		Eventually(func() (string, error) {
			ms, err := mc.Machines(util.MachineAPINamespace).Get(ctx, machine.Name, metav1.GetOptions{})
			if err != nil {
				return "", err
			}
			if ms.Status.Phase == nil {
				return "", nil
			}
			return *(ms.Status.Phase), nil
		}, machineReadyTimeout).Should(BeEquivalentTo("Running"))

		// Remove machine
		By("delete the machine")
		err = mc.Machines(util.MachineAPINamespace).Delete(ctx, machine.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("create machinesets", func(msName string, dataDisks []v1beta1.VSphereDisk) {
		By("checking for the openshift machine api operator")

		// skip if operator is not running
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		// get provider for simple definition vs generating one from scratch
		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		// Create new machineset to test
		By("creating new machineset with data disk configured")
		provider.DataDisks = []v1beta1.VSphereDisk{}
		provider.DataDisks = append(provider.DataDisks, dataDisks...)

		provRawData, err := vsphere.RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())

		ddMachineSet, err := util.CreateMachineSet(ctx, cfg, mc, msName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

		// Scale up one machine
		By("scaling up machineset to create machine")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 1)
		Expect(err).NotTo(HaveOccurred())

		// Verify / wait for machine is ready
		By("verifying machine became ready")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(1))

		// Scale down machineset
		By("scaling down the machineset")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 0)
		Expect(err).NotTo(HaveOccurred())

		// Delete machineset
		By("deleting the machineset")
		err = mc.MachineSets(util.MachineAPINamespace).Delete(ctx, ddMachineSet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())
	},
		Entry("with thin data disk [apigroup:machine.openshift.io]", "ms-thin-test", []v1beta1.VSphereDisk{
			{
				Name:             "thickDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeThick,
			},
		}),
		Entry("with thick data disk [apigroup:machine.openshift.io]", "ms-thick-test", []v1beta1.VSphereDisk{
			{
				Name:             "thickDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeThick,
			},
		}),
		Entry("with eagerly zeroed data disk [apigroup:machine.openshift.io]", "ms-zeroed-test", []v1beta1.VSphereDisk{
			{
				Name:             "zeroedDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeEagerlyZeroed,
			},
		}),
		Entry("with a data disk using each provisioning mode [apigroup:machine.openshift.io]", "ms-multi-test", []v1beta1.VSphereDisk{
			{
				Name:             "thinDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeThin,
			},
			{
				Name:             "thickDataDisk",
				SizeGiB:          2,
				ProvisioningMode: v1beta1.ProvisioningModeThick,
			},
			{
				Name:             "zeroedDataDisk",
				SizeGiB:          3,
				ProvisioningMode: v1beta1.ProvisioningModeEagerlyZeroed,
			},
			{
				Name:    "noModeDataDisk",
				SizeGiB: 3,
			},
		}),
	)
})
