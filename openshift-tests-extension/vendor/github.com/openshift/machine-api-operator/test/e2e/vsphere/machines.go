package vsphere

import (
	"context"
	"fmt"
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
	machineRole         = "feature-gate-test"
	machineReadyTimeout = time.Minute * 6
)

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:VSphereMultiDisk][platform:vsphere][Disruptive] Managed cluster should", Label("Conformance"), Label("Serial"), func() {
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

	It("create machines with data disks [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", func() {
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

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

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

		// By this point, the node object should be deleted, but seems it may linger momentarily causing issue with other tests that grab current
		// nodes to perform tests against.
		By(fmt.Sprintf("waiting for cluster to get back to original size. Final size should be %d worker nodes", initialNumberOfNodes))
		Eventually(func() bool {
			nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			By(fmt.Sprintf("got %v nodes, expecting %v", len(nodeList.Items), initialNumberOfNodes))
			if len(nodeList.Items) != initialNumberOfNodes {
				return false
			}

			return true
		}, 10*time.Minute, 5*time.Second).Should(BeTrue(), "number of nodes should be the same as it was before test started")
	})

	DescribeTable("create machinesets", func(msName string, dataDisks []v1beta1.VSphereDisk) {
		By("checking for the openshift machine api operator")

		// skip if operator is not running
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

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
		}, machineReadyTimeout).Should(BeEquivalentTo(1), "machine ReadyReplicas should be 1 when all machines are ready")

		// Scale down machineset
		By("scaling down the machineset")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 0)
		Expect(err).NotTo(HaveOccurred())

		// Verify / wait for machine is removed
		By("verifying machine is destroyed")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(0), "machine ReadyReplicas should be zero when all machines are destroyed")

		// Delete machineset
		By("deleting the machineset")
		err = mc.MachineSets(util.MachineAPINamespace).Delete(ctx, ddMachineSet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		// By this point, the node object should be deleted, but seems it may linger momentarily causing issue with other tests that grab current
		// nodes to perform tests against.
		By(fmt.Sprintf("waiting for cluster to get back to original size. Final size should be %d worker nodes", initialNumberOfNodes))
		Eventually(func() bool {
			nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
			Expect(err).NotTo(HaveOccurred())
			By(fmt.Sprintf("got %v nodes, expecting %v", len(nodeList.Items), initialNumberOfNodes))
			if len(nodeList.Items) != initialNumberOfNodes {
				return false
			}

			return true
		}, 10*time.Minute, 5*time.Second).Should(BeTrue(), "number of nodes should be the same as it was before test started")

	},
		Entry("with thin data disk [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-thin-test", []v1beta1.VSphereDisk{
			{
				Name:             "thickDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeThick,
			},
		}),
		Entry("with thick data disk [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-thick-test", []v1beta1.VSphereDisk{
			{
				Name:             "thickDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeThick,
			},
		}),
		Entry("with eagerly zeroed data disk [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-zeroed-test", []v1beta1.VSphereDisk{
			{
				Name:             "zeroedDataDisk",
				SizeGiB:          1,
				ProvisioningMode: v1beta1.ProvisioningModeEagerlyZeroed,
			},
		}),
		Entry("with a data disk using each provisioning mode [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-multi-test", []v1beta1.VSphereDisk{
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
