package azure

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

	util "github.com/openshift/machine-api-operator/test/e2e"
)

const (
	machineRole         = "azure-datadisk-test"
	machineReadyTimeout = time.Minute * 6
)

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:AzureMultiDisk][platform:azure][Disruptive] Managed cluster should", Label("Conformance"), Label("Serial"), func() {
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

	It("create machine with multiple data disks [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", func() {
		machineName := "machine-multi-disk-test"
		dataDisks := []v1beta1.DataDisk{
			{
				NameSuffix: "disk1",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadOnly,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
			{
				NameSuffix: "disk2",
				DiskSizeGB: 256,
				Lun:        1,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountStandardLRS,
				},
				CachingType:    v1beta1.CachingTypeNone,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
			{
				NameSuffix: "disk3",
				DiskSizeGB: 512,
				Lun:        2,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadWrite,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}

		By("checking for the openshift machine api operator")
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		By("creating new machine with multiple data disks configured")
		provider.DataDisks = []v1beta1.DataDisk{}
		provider.DataDisks = append(provider.DataDisks, dataDisks...)

		provRawData, err := RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())
		machine, err := util.CreateMachine(ctx, cfg, mc, machineName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

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

		By("delete the machine")
		err = mc.Machines(util.MachineAPINamespace).Delete(ctx, machine.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

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

	DescribeTable("create machinesets with different storage account types", func(msName string, dataDisks []v1beta1.DataDisk) {
		By("checking for the openshift machine api operator")
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		By("creating new machineset with data disk configured")
		provider.DataDisks = []v1beta1.DataDisk{}
		provider.DataDisks = append(provider.DataDisks, dataDisks...)

		provRawData, err := RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())

		ddMachineSet, err := util.CreateMachineSet(ctx, cfg, mc, msName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

		By("scaling up machineset to create machine")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 1)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine became ready")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(1), "machine ReadyReplicas should be 1 when all machines are ready")

		By("scaling down the machineset")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 0)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine is destroyed")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(0), "machine ReadyReplicas should be zero when all machines are destroyed")

		By("deleting the machineset")
		err = mc.MachineSets(util.MachineAPINamespace).Delete(ctx, ddMachineSet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

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
		Entry("with Standard_LRS storage account type [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-standard-lrs-test", []v1beta1.DataDisk{
			{
				NameSuffix: "standarddisk",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountStandardLRS,
				},
				CachingType:    v1beta1.CachingTypeNone,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
		Entry("with Premium_LRS storage account type [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-premium-lrs-test", []v1beta1.DataDisk{
			{
				NameSuffix: "premiumdisk",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadOnly,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
	)

	DescribeTable("create machinesets with different caching types", func(msName string, dataDisks []v1beta1.DataDisk) {
		By("checking for the openshift machine api operator")
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		By("creating new machineset with data disk configured")
		provider.DataDisks = []v1beta1.DataDisk{}
		provider.DataDisks = append(provider.DataDisks, dataDisks...)

		provRawData, err := RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())

		ddMachineSet, err := util.CreateMachineSet(ctx, cfg, mc, msName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

		By("scaling up machineset to create machine")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 1)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine became ready")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(1), "machine ReadyReplicas should be 1 when all machines are ready")

		By("scaling down the machineset")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 0)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine is destroyed")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(0), "machine ReadyReplicas should be zero when all machines are destroyed")

		By("deleting the machineset")
		err = mc.MachineSets(util.MachineAPINamespace).Delete(ctx, ddMachineSet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

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
		Entry("with None caching type [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-cache-none-test", []v1beta1.DataDisk{
			{
				NameSuffix: "cachenone",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeNone,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
		Entry("with ReadOnly caching type [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-cache-readonly-test", []v1beta1.DataDisk{
			{
				NameSuffix: "cachereadonly",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadOnly,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
		Entry("with ReadWrite caching type [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-cache-readwrite-test", []v1beta1.DataDisk{
			{
				NameSuffix: "cachereadwrite",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadWrite,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
	)

	DescribeTable("create machinesets with various disk sizes", func(msName string, dataDisks []v1beta1.DataDisk) {
		By("checking for the openshift machine api operator")
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		By("creating new machineset with data disk configured")
		provider.DataDisks = []v1beta1.DataDisk{}
		provider.DataDisks = append(provider.DataDisks, dataDisks...)

		provRawData, err := RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())

		ddMachineSet, err := util.CreateMachineSet(ctx, cfg, mc, msName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

		By("scaling up machineset to create machine")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 1)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine became ready")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(1), "machine ReadyReplicas should be 1 when all machines are ready")

		By("scaling down the machineset")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 0)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine is destroyed")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(0), "machine ReadyReplicas should be zero when all machines are destroyed")

		By("deleting the machineset")
		err = mc.MachineSets(util.MachineAPINamespace).Delete(ctx, ddMachineSet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

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
		Entry("with small disk size (64GB) [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-size-small-test", []v1beta1.DataDisk{
			{
				NameSuffix: "smalldisk",
				DiskSizeGB: 64,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeNone,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
		Entry("with medium disk size (256GB) [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-size-medium-test", []v1beta1.DataDisk{
			{
				NameSuffix: "mediumdisk",
				DiskSizeGB: 256,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadOnly,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
		Entry("with large disk size (1024GB) [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-size-large-test", []v1beta1.DataDisk{
			{
				NameSuffix: "largedisk",
				DiskSizeGB: 1024,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadWrite,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
		Entry("with multiple disks of different sizes [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", "ms-size-multi-test", []v1beta1.DataDisk{
			{
				NameSuffix: "disk64gb",
				DiskSizeGB: 64,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeNone,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
			{
				NameSuffix: "disk256gb",
				DiskSizeGB: 256,
				Lun:        1,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadOnly,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
			{
				NameSuffix: "disk512gb",
				DiskSizeGB: 512,
				Lun:        2,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
				},
				CachingType:    v1beta1.CachingTypeReadWrite,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}),
	)

	It("create machineset with disk encryption set [apigroup:machine.openshift.io][Serial][Suite:openshift/conformance/serial]", func() {
		msName := "ms-disk-encryption-test"

		By("checking for the openshift machine api operator")
		util.SkipUnlessMachineAPIOperator(dc, c.CoreV1().Namespaces())

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

		By("generating provider for tests")
		provider := getProviderFromMachineSet(cfg)

		// Get the disk encryption set ID from the OS disk if available
		// This ensures we're using a valid encryption set from the cluster
		var diskEncryptionSet *v1beta1.DiskEncryptionSetParameters
		if provider.OSDisk.ManagedDisk.DiskEncryptionSet != nil && provider.OSDisk.ManagedDisk.DiskEncryptionSet.ID != "" {
			diskEncryptionSet = &v1beta1.DiskEncryptionSetParameters{
				ID: provider.OSDisk.ManagedDisk.DiskEncryptionSet.ID,
			}
			By(fmt.Sprintf("using disk encryption set from OS disk: %s", diskEncryptionSet.ID))
		} else {
			By("no disk encryption set found on OS disk, skipping test")
			Skip("cluster does not have disk encryption set configured")
		}

		By("creating new machineset with encrypted data disk configured")
		provider.DataDisks = []v1beta1.DataDisk{
			{
				NameSuffix: "encrypteddisk",
				DiskSizeGB: 128,
				Lun:        0,
				ManagedDisk: v1beta1.DataDiskManagedDiskParameters{
					StorageAccountType: v1beta1.StorageAccountPremiumLRS,
					DiskEncryptionSet:  diskEncryptionSet,
				},
				CachingType:    v1beta1.CachingTypeReadOnly,
				DeletionPolicy: v1beta1.DiskDeletionPolicyTypeDelete,
			},
		}

		provRawData, err := RawExtensionFromProviderSpec(provider)
		Expect(err).NotTo(HaveOccurred())

		ddMachineSet, err := util.CreateMachineSet(ctx, cfg, mc, msName, machineRole, provRawData)
		Expect(err).NotTo(HaveOccurred())

		By("scaling up machineset to create machine")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 1)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine became ready")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(1), "machine ReadyReplicas should be 1 when all machines are ready")

		By("scaling down the machineset")
		err = util.ScaleMachineSet(cfg, ddMachineSet.Name, 0)
		Expect(err).NotTo(HaveOccurred())

		By("verifying machine is destroyed")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(util.MachineAPINamespace).Get(ctx, ddMachineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(0), "machine ReadyReplicas should be zero when all machines are destroyed")

		By("deleting the machineset")
		err = mc.MachineSets(util.MachineAPINamespace).Delete(ctx, ddMachineSet.Name, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

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
})
