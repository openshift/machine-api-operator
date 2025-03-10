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

type MachineTest struct {
	testCase    string
	machineName string
	dataDisks   []v1beta1.VSphereDisk
	ctx         context.Context
	cfg         *rest.Config
	c           *kubernetes.Clientset
	dc          *dynamic.DynamicClient
	mc          *machinesetclient.MachineV1beta1Client
}

type MachineSetTest struct {
	testCase  string
	msName    string
	dataDisks []v1beta1.VSphereDisk
	ctx       context.Context
	cfg       *rest.Config
	c         *kubernetes.Clientset
	dc        *dynamic.DynamicClient
	mc        *machinesetclient.MachineV1beta1Client
}

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
		test := MachineTest{
			testCase:    "create machine with a data disk for each provisioning mode",
			machineName: "machine-multi-test",
			dataDisks: []v1beta1.VSphereDisk{
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
			},
			ctx: ctx,
			cfg: cfg,
			c:   c,
			dc:  dc,
			mc:  mc,
		}

		performMachineTest(test)
	})

	It("create machinesets with thick data disk [apigroup:machine.openshift.io]", func() {
		test := MachineSetTest{
			testCase: "create machinesets with thick data disk",
			msName:   "ms-thick-test",
			dataDisks: []v1beta1.VSphereDisk{
				{
					Name:             "thickDataDisk",
					SizeGiB:          1,
					ProvisioningMode: v1beta1.ProvisioningModeThick,
				},
			},
			ctx: ctx,
			cfg: cfg,
			c:   c,
			dc:  dc,
			mc:  mc,
		}

		performMachineSetTest(test)
	})

	It("create machineset with thin data disk [apigroup:machine.openshift.io]", func() {
		test := MachineSetTest{
			testCase: "create machinesets with thick data disk",
			msName:   "ms-thin-test",
			dataDisks: []v1beta1.VSphereDisk{
				{
					Name:             "thinDataDisk",
					SizeGiB:          1,
					ProvisioningMode: v1beta1.ProvisioningModeThin,
				},
			},
			ctx: ctx,
			cfg: cfg,
			c:   c,
			dc:  dc,
			mc:  mc,
		}

		performMachineSetTest(test)
	})

	It("create machineset with eagerly zeroed data disk [apigroup:machine.openshift.io]", func() {
		test := MachineSetTest{
			testCase: "create machinesets with thick data disk",
			msName:   "ms-zeroed-test",
			dataDisks: []v1beta1.VSphereDisk{
				{
					Name:             "zeroedDataDisk",
					SizeGiB:          1,
					ProvisioningMode: v1beta1.ProvisioningModeEagerlyZeroed,
				},
			},
			ctx: ctx,
			cfg: cfg,
			c:   c,
			dc:  dc,
			mc:  mc,
		}

		performMachineSetTest(test)
	})

	It("create machineset with data disk with each provisioning mode [apigroup:machine.openshift.io]", func() {
		test := MachineSetTest{
			testCase: "create machinesets with thick data disk",
			msName:   "ms-multi-test",
			dataDisks: []v1beta1.VSphereDisk{
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
			},
			ctx: ctx,
			cfg: cfg,
			c:   c,
			dc:  dc,
			mc:  mc,
		}

		performMachineSetTest(test)
	})
})

func performMachineTest(test MachineTest) {
	By("checking for the openshift machine api operator")

	// skip if operator is not running
	util.SkipUnlessMachineAPIOperator(test.dc, test.c.CoreV1().Namespaces())

	// get provider for simple definition vs generating one from scratch
	By("generating provider for tests")
	provider := getProviderFromMachineSet(test.cfg)

	// Create new machine to test
	By("creating new machine with data disk configured")
	provRawData, err := vsphere.RawExtensionFromProviderSpec(provider)
	Expect(err).NotTo(HaveOccurred())
	machine, err := util.CreateMachine(test.ctx, test.cfg, test.mc, test.machineName, machineRole, provRawData)
	Expect(err).NotTo(HaveOccurred())

	// Wait for machine to get ready
	By("verifying machine became ready")
	Eventually(func() (string, error) {
		ms, err := test.mc.Machines(util.MachineAPINamespace).Get(test.ctx, machine.Name, metav1.GetOptions{})
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
	err = test.mc.Machines(util.MachineAPINamespace).Delete(test.ctx, machine.Name, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred())
}

// performMachineSetTest will run the supplied MachineSetTest.  This involves creating the machine set, scaling it up,
// verifying it results in a machine that becomes ready, and then scales it down followed by deleting the machine set.
func performMachineSetTest(test MachineSetTest) {
	By("checking for the openshift machine api operator")

	// skip if operator is not running
	util.SkipUnlessMachineAPIOperator(test.dc, test.c.CoreV1().Namespaces())

	// get provider for simple definition vs generating one from scratch
	By("generating provider for tests")
	provider := getProviderFromMachineSet(test.cfg)

	// Create new machineset to test
	By("creating new machineset with data disk configured")
	provider.DataDisks = []v1beta1.VSphereDisk{}
	provider.DataDisks = append(provider.DataDisks, test.dataDisks...)

	provRawData, err := vsphere.RawExtensionFromProviderSpec(provider)
	Expect(err).NotTo(HaveOccurred())

	ddMachineSet, err := util.CreateMachineSet(test.ctx, test.cfg, test.mc, test.msName, machineRole, provRawData)
	Expect(err).NotTo(HaveOccurred())

	// Scale up one machine
	By("scaling up machineset to create machine")
	err = util.ScaleMachineSet(test.cfg, ddMachineSet.Name, 1)
	Expect(err).NotTo(HaveOccurred())

	// Verify / wait for machine is ready
	By("verifying machine became ready")
	Eventually(func() (int32, error) {
		ms, err := test.mc.MachineSets(util.MachineAPINamespace).Get(test.ctx, ddMachineSet.Name, metav1.GetOptions{})
		if err != nil {
			return -1, err
		}
		return ms.Status.ReadyReplicas, nil
	}, machineReadyTimeout).Should(BeEquivalentTo(1))

	// Scale down machineset
	By("scaling down the machineset")
	err = util.ScaleMachineSet(test.cfg, ddMachineSet.Name, 0)
	Expect(err).NotTo(HaveOccurred())

	// Delete machineset
	By("deleting the machineset")
	err = test.mc.MachineSets(util.MachineAPINamespace).Delete(test.ctx, ddMachineSet.Name, metav1.DeleteOptions{})
	Expect(err).NotTo(HaveOccurred())
}
