package vsphere

import (
	"context"
	_ "embed"
	"fmt"
	"slices"
	"strings"

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
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func failIfNodeNotInMachineNetwork(nodes corev1.NodeList, machineNetworks []string) {

	By("checking if nodes are in the machine network")

	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type != "InternalIP" && address.Type != "ExternalIP" {
				continue
			}
			inRange, err := isIpInCidrRange(address.Address, machineNetworks[0])
			Expect(err).NotTo(HaveOccurred())
			Expect(inRange).To(BeTrue())
		}
	}
}

func failIfIncorrectPortgroupsAttachedToVMs(
	ctx context.Context,
	infra configv1.PlatformSpec,
	nodeList *corev1.NodeList,
	vsphereCreds *corev1.Secret) {

	By("checking if VMs have the correct portgroups attached")

	for _, failureDomain := range infra.VSphere.FailureDomains {
		nodes, err := getNodesInFailureDomain(infra.VSphere, failureDomain, nodeList)
		fmt.Printf("nodes: %d", len(nodes))
		Expect(err).NotTo(HaveOccurred())

		vmPortgroupMap, err := GetPortGroupsAttachedToVMsInFailureDomain(ctx, failureDomain, vsphereCreds, infra.VSphere.VCenters)
		if err != nil {
			Expect(err).NotTo(HaveOccurred())
		}

		var nodeProviderIds []string
		for _, node := range nodes {
			providerId := node.Spec.ProviderID
			Expect(len(providerId)).ShouldNot(BeZero())

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

	By("checking to see if machine has all portgroups")

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

		Expect(len(infra.Spec.PlatformSpec.VSphere.FailureDomains) >= 1)

		for _, machineNetwork := range infra.Spec.PlatformSpec.VSphere.MachineNetworks {
			machineNetworks = append(machineNetworks, string(machineNetwork))
		}

		Expect(len(machineNetworks) >= 1)
		slices.Sort(machineNetworks)

		nodes, err = c.CoreV1().Nodes().List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		machines, err = mc.Machines("openshift-machine-api").List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		portGroups := make(map[string]any)
		for _, machine := range machines.Items {
			providerSpec, err := vsphere.ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())

			for _, network := range providerSpec.Network.Devices {
				portGroups[network.NetworkName] = network
			}
		}

		for k, _ := range portGroups {
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
		failIfIncorrectPortgroupsAttachedToVMs(ctx, infra.Spec.PlatformSpec, nodes, vsphereCreds)
	})

	It("new machines should pass multi network tests [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		machineSets, err := e2eutil.GetMachineSets(cfg)
		Expect(err).NotTo(HaveOccurred())

		Expect(len(machineSets.Items) >= 1)

		machineSet := machineSets.Items[0]

		// scale up new machine and wait for scale up to complete
		By("scaling up a new machineset which should have multiple NICs")
		err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, int(*machineSet.Spec.Replicas)+1)
		Expect(err).NotTo(HaveOccurred())

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

		for k, _ := range portGroups {
			machinePortgroups = append(machinePortgroups, k)
		}

		failIfNodeNotInMachineNetwork(*nodes, machineNetworks)
		failIfNodeNetworkingInconsistentWithMachineNetwork(infra.Spec.PlatformSpec, machineNetworks)
		failIfMachinesDoNotHaveAllPortgroups(infra.Spec.PlatformSpec, machines)
		failIfIncorrectPortgroupsAttachedToVMs(ctx, infra.Spec.PlatformSpec, nodes, vsphereCreds)
	})
})
