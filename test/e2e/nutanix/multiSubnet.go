package nutanix

import (
	"context"
	_ "embed"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	configclient "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	machinesetclient "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"

	// e2eutil "github.com/openshift/machine-api-operator/test/e2e"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func failIfNodeNotInMachineNetwork(nodes corev1.NodeList, machineNetworks []configv1.NutanixFailureDomain) {
	By("checking if nodes are in the machine network")
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type != "InternalIP" && address.Type != "ExternalIP" {
				continue
			}
			cidrFound := false
			for _, subnet := range machineNetworks {
				for _, machineNetwork := range subnet.Subnets {
					inRange, err := isIpInCidrRange(address.Address, *machineNetwork.UUID)
					Expect(err).NotTo(HaveOccurred())
					if inRange {
						cidrFound = true
						break
					}
				}
			}
			Expect(cidrFound).To(BeTrue(), "machine IP must be in one of the machine network CIDR ranges")
		}
	}
}

func failIfNodesNotInCorrectSubnets(infra *configv1.Infrastructure, nodeList *corev1.NodeList) {
	By("checking node subnets against Nutanix failure domains")
	for _, fd := range infra.Spec.PlatformSpec.Nutanix.FailureDomains {
		// Get nodes that should be in this failure domain
		nodes, err := getNodesInFailureDomainNutanix(fd, nodeList)
		Expect(err).NotTo(HaveOccurred())
		// Get subnet UUIDs for this FD
		subnetUUIDs := []string{}
		for _, sn := range fd.Subnets {
			subnetUUIDs = append(subnetUUIDs, *sn.UUID)
		}
		// For each node, verify its subnet matches one of fd.Subnets
		for _, node := range nodes {
			nodeSubnet := getNodeNutanixSubnet(node) // implement as needed
			Expect(slices.Contains(subnetUUIDs, nodeSubnet)).To(BeTrue(), "Node's subnet must match one of the FD's subnets")
		}
	}
}

func failIfMachinesDoNotHaveAllSubnets(platformSpec configv1.PlatformSpec, machines *machinev1beta1.MachineList) {
	By("checking to see if machines have all subnets associated with their failure domain")
	for _, fd := range platformSpec.Nutanix.FailureDomains {
		machinesInFD, err := getMachinesInFailureDomainNutanix(fd, machines)
		Expect(err).NotTo(HaveOccurred())
		expectedSubnets := []string{}
		for _, sn := range fd.Subnets {
			expectedSubnets = append(expectedSubnets, *sn.UUID)
		}
		slices.Sort(expectedSubnets)
		for _, machine := range machinesInFD.Items {
			spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())
			machineSubnets := []string{}
			for i := range spec.Subnets {
				machineSubnets = append(machineSubnets, *spec.Subnets[i].UUID)
			}
			slices.Sort(machineSubnets)
			Expect(slices.Equal(expectedSubnets, machineSubnets)).To(BeTrue(), "machine subnets must match the failure domain subnets")
		}
	}
}

var _ = Describe("[sig-cluster-nutanix][OCPFeatureGate:NutanixMultiSubnets][platform:nutanix] Managed cluster should", Label("Conformance"), func() {
	defer GinkgoRecover()
	ctx := context.Background()

	var (
		cfg *rest.Config
		c   *kubernetes.Clientset
		cc  *configclient.ConfigV1Client

		mc              *machinesetclient.MachineV1beta1Client
		err             error
		machineNetworks []configv1.NutanixFailureDomain
		infra           *configv1.Infrastructure
		nodes           *corev1.NodeList
		machines        *machinev1beta1.MachineList
	)

	BeforeEach(func() {
		cfg, err = e2e.LoadConfig()
		Expect(err).NotTo(HaveOccurred())
		c, err = e2e.LoadClientset()
		Expect(err).NotTo(HaveOccurred())
		mc, err = machinesetclient.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		cc, err = configclient.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred())
		infra, err = cc.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(len(infra.Spec.PlatformSpec.Nutanix.FailureDomains)).ShouldNot(Equal(0))

		machineNetworks = append(machineNetworks, infra.Spec.PlatformSpec.Nutanix.FailureDomains...)
		Expect(len(machineNetworks)).ShouldNot(Equal(0))

		nodes, err = c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		machines, err = mc.Machines("openshift-machine-api").List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		if len(machines.Items) == 0 {
			Skip("skipping due to lack of machines / IPI cluster")
		}
	})

	It("node addresses should be correlated with the machine network [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfNodeNotInMachineNetwork(*nodes, machineNetworks)
	})

	It("nodes should be present in correct subnets per Nutanix failure domain", func() {
		failIfNodesNotInCorrectSubnets(infra, nodes)
	})

	It("machines should have all specified subnets associated with their failure domain", func() {
		failIfMachinesDoNotHaveAllSubnets(infra.Spec.PlatformSpec, machines)
	})

	// It("new machines should pass multi network tests [apigroup:machine.openshift.io][Suite:openshift/conformance/serial]", Label("Serial"), func() {
	// 	machineSets, err := e2eutil.GetMachineSets(cfg)
	// 	Expect(err).NotTo(HaveOccurred())

	// 	Expect(len(machineSets.Items)).ShouldNot(Equal(0))

	// 	machineSet := machineSets.Items[0]
	// 	origReplicas := int(*machineSet.Spec.Replicas)
	// 	// scale up new machine and wait for scale up to complete
	// 	By("scaling up a new machineset which should have multiple NICs")
	// 	err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, origReplicas+1)
	// 	Expect(err).NotTo(HaveOccurred())

	// 	// Verify / wait for machine is ready
	// 	By("verifying machine became ready")
	// 	Eventually(func() (int32, error) {
	// 		ms, err := mc.MachineSets(e2eutil.MachineAPINamespace).Get(ctx, machineSet.Name, metav1.GetOptions{})
	// 		if err != nil {
	// 			return -1, err
	// 		}
	// 		return ms.Status.ReadyReplicas, nil
	// 	}, machineReadyTimeout).Should(BeEquivalentTo(origReplicas + 1))

	// 	nodes, err = c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	// 	Expect(err).NotTo(HaveOccurred())

	// 	machines, err = mc.Machines("openshift-machine-api").List(ctx, metav1.ListOptions{})
	// 	Expect(err).NotTo(HaveOccurred())

	// 	By("determining common port groups among machines")
	// 	for _, machine := range machines.Items {
	// 		_, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
	// 		Expect(err).NotTo(HaveOccurred())
	// 	}

	// 	failIfNodeNotInMachineNetwork(*nodes, machineNetworks)
	// 	failIfNodesNotInCorrectSubnets(infra, nodes)
	// 	failIfMachinesDoNotHaveAllSubnets(infra.Spec.PlatformSpec, machines)

	// 	// Scale down machineset
	// 	By("scaling down the machineset")
	// 	err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, origReplicas)
	// 	Expect(err).NotTo(HaveOccurred())

	// 	// Verify / wait for machine is removed
	// 	By("verifying machine is destroyed")
	// 	Eventually(func() (int32, error) {
	// 		ms, err := mc.MachineSets(e2eutil.MachineAPINamespace).Get(ctx, machineSet.Name, metav1.GetOptions{})
	// 		if err != nil {
	// 			return -1, err
	// 		}
	// 		return ms.Status.ReadyReplicas, nil
	// 	}, machineReadyTimeout).Should(BeEquivalentTo(origReplicas))
	// })
})
