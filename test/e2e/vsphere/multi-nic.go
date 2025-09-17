package vsphere

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	configclient "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	machinesetclient "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere"
	e2eutil "github.com/openshift/machine-api-operator/test/e2e"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/kubernetes/test/e2e/framework"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func failIfNodeNotInMachineNetwork(nodes corev1.NodeList, machineNetworks []string) {

	By("checking if nodes are in the machine network")

	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type != "InternalIP" && address.Type != "ExternalIP" {
				continue
			}

			cidrFound := false
			for _, machineNetwork := range machineNetworks {
				inRange, err := isIpInCidrRange(address.Address, machineNetwork)
				Expect(err).NotTo(HaveOccurred())

				if inRange {
					cidrFound = true
					break
				}
			}

			Expect(cidrFound).To(BeTrue(), "machine IP must be in one of the machine network CIDR ranges")
		}
	}
}

func failIfIncorrectPortgroupsAttachedToVMs(
	ctx context.Context,
	infra *configv1.Infrastructure,
	nodeList *corev1.NodeList,
	vsphereCreds *corev1.Secret) {

	By("checking if VMs have the correct portgroups attached")

	providerSpec := infra.Spec.PlatformSpec
	for _, failureDomain := range providerSpec.VSphere.FailureDomains {
		nodes, err := getNodesInFailureDomain(providerSpec.VSphere, failureDomain, nodeList)
		fmt.Printf("nodes: %d", len(nodes))
		Expect(err).NotTo(HaveOccurred())

		vmPortgroupMap, err := GetPortGroupsAttachedToVMsInFailureDomain(ctx, infra.Status.InfrastructureName, failureDomain, vsphereCreds, providerSpec.VSphere.VCenters)
		if err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		var nodeProviderIds []string
		for _, node := range nodes {
			providerId := node.Spec.ProviderID

			if !strings.HasPrefix(providerId, "vsphere://") {
				// Node is not a vsphere node.  This could be a BM node in a hybrid scenario or maybe even a nutanix node.
				continue
			}
			parts := strings.Split(providerId, "vsphere://")
			Expect(len(parts)).Should(BeIdenticalTo(2))

			nodeProviderIds = append(nodeProviderIds, parts[1])
		}

		for _, nodeProviderId := range nodeProviderIds {
			attachedPortgroups, exists := vmPortgroupMap[nodeProviderId]
			Expect(exists).To(BeTrue())

			slices.Sort(attachedPortgroups)
			slices.Sort(failureDomain.Topology.Networks)
			if slices.Compare(attachedPortgroups, failureDomain.Topology.Networks) != 0 {
				Expect(fmt.Errorf("portgroups for VM %s does not align with failure domain %s", nodeProviderId, failureDomain.Name)).NotTo(HaveOccurred())
			}
		}
	}
}

func failIfNodeNetworkingInconsistentWithMachineNetwork(infra configv1.PlatformSpec, machineNetworks []string) {
	// This can happen in scenarios where multinetwork is not enabled.
	if len(infra.VSphere.NodeNetworking.External.NetworkSubnetCIDR) == 0 ||
		len(infra.VSphere.NodeNetworking.Internal.NetworkSubnetCIDR) == 0 {
		Skip("skipping test due to incomplete config")
	}

	internalNodeNetworking := infra.VSphere.NodeNetworking.Internal
	externalNodeNetworking := infra.VSphere.NodeNetworking.External

	// machineNetworks contain the VIPs now so we'll need to check each network to see if we find one that matches internal/external.
	By("comparing nodeNetworking slices to the machine network")
	for _, nodeNetworkingSpec := range []configv1.VSpherePlatformNodeNetworkingSpec{internalNodeNetworking, externalNodeNetworking} {
		for _, network := range nodeNetworkingSpec.NetworkSubnetCIDR {
			Expect(slices.Contains(machineNetworks, network)).To(BeTrue())
		}
	}
}

func failIfMachinesDoNotHaveAllPortgroups(platformSpec configv1.PlatformSpec, machines *machinev1beta1.MachineList) {

	By("checking to see if machines have all portgroups")

	for _, failureDomain := range platformSpec.VSphere.FailureDomains {
		machinesInFailureDomain, err := getMachinesInFailureDomain(platformSpec.VSphere, failureDomain, machines)
		Expect(err).NotTo(HaveOccurred())

		for _, machine := range machinesInFailureDomain {
			failIfMachineDoesNotHaveAllPortgroups(machine, failureDomain)
		}
	}
}

func failIfMachineDoesNotHaveAllPortgroups(machine machinev1beta1.Machine, failureDomain configv1.VSpherePlatformFailureDomainSpec) {
	framework.Logf("checking to see if machine %s has all portgroups", machine.Name)

	spec, err := vsphere.ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
	Expect(err).NotTo(HaveOccurred())

	expectedPortgroups := failureDomain.Topology.Networks
	var portgroups []string

	for _, device := range spec.Network.Devices {
		portgroups = append(portgroups, device.NetworkName)
	}

	slices.Sort(expectedPortgroups)
	slices.Sort(portgroups)

	Expect(slices.Equal(expectedPortgroups, portgroups)).To(BeTrue())
}

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:VSphereMultiNetworks][platform:vsphere] Managed cluster should", Label("Conformance"), func() {
	defer GinkgoRecover()
	ctx := context.Background()

	var (
		cfg *rest.Config
		c   *kubernetes.Clientset
		cc  *configclient.ConfigV1Client

		mc                *machinesetclient.MachineV1beta1Client
		err               error
		machineNetworks   []string
		infra             *configv1.Infrastructure
		nodes             *corev1.NodeList
		machinePortgroups []string
		vsphereCreds      *corev1.Secret
		machines          *machinev1beta1.MachineList
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

		vsphereCreds, err = c.CoreV1().Secrets("kube-system").Get(ctx, "vsphere-creds", v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())

		Expect(len(infra.Spec.PlatformSpec.VSphere.FailureDomains)).ShouldNot(Equal(0))

		for _, machineNetwork := range infra.Spec.PlatformSpec.VSphere.MachineNetworks {
			machineNetworks = append(machineNetworks, string(machineNetwork))
		}

		Expect(len(machineNetworks)).ShouldNot(Equal(0))
		slices.Sort(machineNetworks)

		nodes, err = c.CoreV1().Nodes().List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		machines, err = mc.Machines("openshift-machine-api").List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		// If we have no machines, this normally means UPI install.  Normally IPI would have machines for at least the control plane.
		if len(machines.Items) == 0 {
			Skip("skipping due to lack of machines / UPI cluster")
		}

		portGroups := make(map[string]any)
		for _, machine := range machines.Items {
			providerSpec, err := vsphere.ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())

			for _, network := range providerSpec.Network.Devices {
				portGroups[network.NetworkName] = network
			}
		}

		for k := range portGroups {
			machinePortgroups = append(machinePortgroups, k)
		}
	})

	It("node addresses should be correlated with the machine network [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		By("checking for correlation between node internal/external IPs and the machine network")
		failIfNodeNotInMachineNetwork(*nodes, machineNetworks)
	})

	It("machine network should be correlated with node networking [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfNodeNetworkingInconsistentWithMachineNetwork(infra.Spec.PlatformSpec, machineNetworks)
	})

	It("machines should have all specified portgroup associated with their failure domain [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachinesDoNotHaveAllPortgroups(infra.Spec.PlatformSpec, machines)
	})

	It("node VMs should have all specified portgroups attached which are associated with their failure domain [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfIncorrectPortgroupsAttachedToVMs(ctx, infra, nodes, vsphereCreds)
	})

	It("new machines should pass multi network tests [Serial][apigroup:machine.openshift.io][Suite:openshift/conformance/serial]", Label("Serial"), func() {

		By("checking initial cluster size")
		nodeList, err := c.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		initialNumberOfNodes := len(nodeList.Items)
		By(fmt.Sprintf("initial cluster size is %v", initialNumberOfNodes))

		machineSets, err := e2eutil.GetMachineSets(cfg)
		Expect(err).NotTo(HaveOccurred())

		Expect(len(machineSets.Items)).ShouldNot(Equal(0))

		machineSet := machineSets.Items[0]
		origReplicas := int(*machineSet.Spec.Replicas)

		// scale up new machine and wait for scale up to complete
		By("scaling up a new machineset which should have multiple NICs")
		err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, origReplicas+1)
		Expect(err).NotTo(HaveOccurred())

		// Verify / wait for machine is ready
		By("verifying machine became ready")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(e2eutil.MachineAPINamespace).Get(ctx, machineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(origReplicas + 1))

		nodes, err = c.CoreV1().Nodes().List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		machines, err = mc.Machines("openshift-machine-api").List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("determining common port groups among machines")
		portGroups := make(map[string]any)
		for _, machine := range machines.Items {
			providerSpec, err := vsphere.ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())

			for _, network := range providerSpec.Network.Devices {
				portGroups[network.NetworkName] = network
			}
		}

		for k := range portGroups {
			machinePortgroups = append(machinePortgroups, k)
		}

		failIfNodeNotInMachineNetwork(*nodes, machineNetworks)
		failIfNodeNetworkingInconsistentWithMachineNetwork(infra.Spec.PlatformSpec, machineNetworks)
		failIfMachinesDoNotHaveAllPortgroups(infra.Spec.PlatformSpec, machines)
		failIfIncorrectPortgroupsAttachedToVMs(ctx, infra, nodes, vsphereCreds)

		// Scale down machineset
		By("scaling down the machineset")
		err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, origReplicas)
		Expect(err).NotTo(HaveOccurred())

		// Verify / wait for machine is removed
		By("verifying machine is destroyed")
		Eventually(func() (int32, error) {
			ms, err := mc.MachineSets(e2eutil.MachineAPINamespace).Get(ctx, machineSet.Name, metav1.GetOptions{})
			if err != nil {
				return -1, err
			}
			return ms.Status.ReadyReplicas, nil
		}, machineReadyTimeout).Should(BeEquivalentTo(origReplicas))

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
})
