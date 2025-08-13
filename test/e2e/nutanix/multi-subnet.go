package nutanix

import (
	"context"
	_ "embed"
	"encoding/json"
	"slices"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	configclient "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	machinesetclient "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"

	e2eutil "github.com/openshift/machine-api-operator/test/e2e"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	e2e "k8s.io/kubernetes/test/e2e/framework"
)

func failIfMachinesNotHaveMultipleNetwork(machines *machinev1beta1.MachineList) {
	By("checking if machines have multiple network addresses")
	for _, machines := range machines.Items {
		count := 0
		for _, address := range machines.Status.Addresses {
			if address.Type == "Internal" {
				count = count + 1
			}
		}
		Expect(count).To(BeNumerically(">", 1))
	}
}

func failIfNodeNotInMachineNetwork(nodes *corev1.NodeList) {
	By("checking if nodes are in the machine network")

	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type != "InternalIP" && address.Type != "ExternalIP" {
				continue
			}
			cidrFound := false
			cidrsJSON := node.Annotations["k8s.ovn.org/host-cidrs"]
			var cidrs []string
			if err := json.Unmarshal([]byte(cidrsJSON), &cidrs); err != nil {
				Expect(err).NotTo(HaveOccurred())
			}
			for _, cidr := range cidrs {
				inRange, err := isIpInCidrRange(address.Address, cidr)
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

func failIfMachinesDoesNotContainAllSubnet(machines *machinev1beta1.MachineList, machineNetworks []configv1.NutanixFailureDomain) {
	By("checking node address against Nutanix failure domains")
	failureDomainSubnets := make(map[string][]string)
	for _, domain := range machineNetworks {
		for _, subnet := range domain.Subnets {
			failureDomainSubnets[domain.Name] = append(failureDomainSubnets[domain.Name], *subnet.UUID)
		}
		sort.Strings(failureDomainSubnets[domain.Name])
		failureDomainMachines, err := getMachinesInFailureDomain(machines, domain.Name)
		Expect(err).ToNot(HaveOccurred())
		for _, machine := range failureDomainMachines {
			machineSubnets := []string{}
			spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())
			for _, subnet := range spec.Subnets {
				machineSubnets = append(machineSubnets, *subnet.UUID)
			}
			sort.Strings(machineSubnets)
			Expect(slices.Equal(machineSubnets, failureDomainSubnets[domain.Name])).To(BeTrue())
		}
	}
}

func failIfMachinesIfDuplicateIP(machines *machinev1beta1.MachineList) {
	seen := make(map[string]bool)
	for _, machine := range machines.Items {
		for _, addr := range machine.Status.Addresses {
			if addr.Type == "InternalIP" || addr.Type == "ExternalIP" {
				Expect(seen[addr.Address]).To(BeFalse(), "Duplicate IP address found: "+addr.Address)
				seen[addr.Address] = true
			}
		}
	}
}

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:NutanixMultiSubnets][platform:nutanix] Managed cluster should support multi-subnet networking", Label("Conformance"), Label("Parallel"), func() {
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

		// err = fmt.Errorf("FD %s: PE=%s, Subnets=%v", machineNetworks[0].Name, *machineNetworks[0].Cluster.UUID, *machineNetworks[0].Subnets[0].UUID)
		//Expect(err).NotTo(HaveOccurred())

		nodes, err = c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		machines, err = mc.Machines("openshift-machine-api").List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		if len(machines.Items) == 0 {
			Skip("skipping due to lack of machines / IPI cluster")
		}
	})

	It("machines should have multiple Internal Address [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachinesNotHaveMultipleNetwork(machines)
	})

	It("nodes should be present in correct subnets per Nutanix failure domain", func() {
		failIfNodeNotInMachineNetwork(nodes)
	})

	It("machines should have all specified subnets associated with their failure domain", func() {
		failIfMachinesDoesNotContainAllSubnet(machines, machineNetworks)
	})

	It("machines should have all unique IPs for all subnets", func() {
		failIfMachinesIfDuplicateIP(machines)
	})

	It("new machines should pass multi network tests [apigroup:machine.openshift.io][Suite:openshift/conformance/serial]", Label("Serial"), func() {
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

		nodes, err = c.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		machines, err = mc.Machines("openshift-machine-api").List(ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		By("determining common port groups among machines")
		for _, machine := range machines.Items {
			_, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())
		}

		failIfNodeNotInMachineNetwork(nodes)
		failIfMachinesNotHaveMultipleNetwork(machines)
		failIfMachinesDoesNotContainAllSubnet(machines, machineNetworks)
		failIfMachinesIfDuplicateIP(machines)

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
	})
})
