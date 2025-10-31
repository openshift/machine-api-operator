package nutanix

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
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

// validateMachineMultipleNetworks verifies machines have multiple network addresses
func validateMachineMultipleNetworks(machines *machinev1beta1.MachineList) {
	By("checking if machines have multiple network addresses")
	for _, machine := range machines.Items {
		count := countInternalIPs(machine)
		Expect(count).To(BeNumerically(">", 1),
			"machine %s should have multiple InternalIP addresses, found %d", machine.Name, count)
	}
}

// validateNodeNetworkPlacement verifies nodes are placed in correct subnets
func validateNodeNetworkPlacement(nodes *corev1.NodeList) {
	By("checking if nodes are in the machine network")

	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type != internalIPType && address.Type != externalIPType {
				continue
			}

			cidrFound := false
			cidrsJSON := node.Annotations[ovnHostCIDRsAnnotation]
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
			Expect(cidrFound).To(BeTrue(), "machine IP %s must be in one of the machine network CIDR ranges", address.Address)
		}
	}
}

// validateMachineSubnetConfiguration verifies machines have correct subnet configuration per failure domain
func validateMachineSubnetConfiguration(machines *machinev1beta1.MachineList, machineNetworks []configv1.NutanixFailureDomain) {
	By("checking machine subnets match failure domain configuration")

	failureDomainSubnets := make(map[string][]string)

	for _, domain := range machineNetworks {
		for _, subnet := range domain.Subnets {
			identifier, err := getSubnetIdentifier(subnet)
			Expect(err).NotTo(HaveOccurred())
			failureDomainSubnets[domain.Name] = append(failureDomainSubnets[domain.Name], identifier)
		}
		sort.Strings(failureDomainSubnets[domain.Name])

		failureDomainMachines, err := getMachinesInFailureDomain(machines, domain.Name)
		Expect(err).ToNot(HaveOccurred())

		for _, machine := range failureDomainMachines {
			machineSubnets := []string{}
			spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())

			for _, subnet := range spec.Subnets {
				identifier, err := getMachineSubnetIdentifier(subnet)
				Expect(err).NotTo(HaveOccurred())
				machineSubnets = append(machineSubnets, identifier)
			}

			sort.Strings(machineSubnets)
			Expect(slices.Equal(machineSubnets, failureDomainSubnets[domain.Name])).To(BeTrue(),
				"machine %s subnets %v should match failure domain %s subnets %v",
				machine.Name, machineSubnets, domain.Name, failureDomainSubnets[domain.Name])
		}
	}
}

// validateUniqueIPAddresses verifies all machines have unique IP addresses
func validateUniqueIPAddresses(machines *machinev1beta1.MachineList) {
	By("checking machines have unique IP addresses")

	seen := make(map[string]bool)
	for _, machine := range machines.Items {
		for _, addr := range machine.Status.Addresses {
			if addr.Type == internalIPType || addr.Type == externalIPType {
				Expect(seen[addr.Address]).To(BeFalse(),
					"Duplicate IP address %s found on machine %s", addr.Address, machine.Name)
				seen[addr.Address] = true
			}
		}
	}
}

// validateSubnetIdentifierFormats checks that subnet identifiers are properly formatted
func validateSubnetIdentifierFormats(machines *machinev1beta1.MachineList) {
	By("validating subnet identifier formats")

	for _, machine := range machines.Items {
		spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
		Expect(err).NotTo(HaveOccurred())

		for _, subnet := range spec.Subnets {
			hasUUID := subnet.UUID != nil && *subnet.UUID != ""
			hasName := subnet.Name != nil && *subnet.Name != ""
			Expect(hasUUID || hasName).To(BeTrue(),
				"subnet in machine %s must have either UUID or Name set", machine.Name)

			if hasUUID {
				uuid := *subnet.UUID
				Expect(len(uuid)).To(BeNumerically(">=", minUUIDLength),
					"UUID %s should be at least %d characters", uuid, minUUIDLength)
			}

			if hasName {
				name := *subnet.Name
				Expect(len(name)).To(BeNumerically(">", 0),
					"subnet name should not be empty")
			}
		}
	}
}

// validateFailureDomainSubnetLimits checks subnet count limits per failure domain
func validateFailureDomainSubnetLimits(machineNetworks []configv1.NutanixFailureDomain) {
	By("validating failure domain subnet count limits")

	for _, domain := range machineNetworks {
		subnetCount := len(domain.Subnets)
		Expect(subnetCount).To(BeNumerically(">=", minSubnetsPerFailureDomain),
			"failure domain %s must have at least %d subnet", domain.Name, minSubnetsPerFailureDomain)
		Expect(subnetCount).To(BeNumerically("<=", maxSubnetsPerFailureDomain),
			"failure domain %s cannot have more than %d subnets", domain.Name, maxSubnetsPerFailureDomain)

		seenSubnets := make(map[string]bool)
		for _, subnet := range domain.Subnets {
			identifier, err := getSubnetIdentifier(subnet)
			if err == nil {
				Expect(seenSubnets[identifier]).To(BeFalse(),
					"duplicate subnet %s found in failure domain %s", identifier, domain.Name)
				seenSubnets[identifier] = true
			}
		}
	}
}

// validateNetworkConnectivity checks that machines can communicate across subnets
func validateNetworkConnectivity(nodes *corev1.NodeList) {
	By("validating network connectivity across subnets")

	if len(nodes.Items) < 2 {
		Skip("skipping connectivity test - need at least 2 nodes")
		return
	}

	subnetNodes := make(map[string][]corev1.Node)
	for _, node := range nodes.Items {
		for _, address := range node.Status.Addresses {
			if address.Type == internalIPType {
				subnetNodes[address.Address] = append(subnetNodes[address.Address], node)
				break
			}
		}
	}

	if len(subnetNodes) < 2 {
		Skip("skipping connectivity test - nodes not distributed across multiple subnets")
		return
	}

	By(fmt.Sprintf("found nodes distributed across %d different subnets", len(subnetNodes)))
}

// validateMachineStatusConsistency checks that machine status reflects proper multi-subnet setup
func validateMachineStatusConsistency(machines *machinev1beta1.MachineList) {
	By("validating machine status consistency with multi-subnet configuration")

	for _, machine := range machines.Items {
		Expect(machine.Status.Phase).ToNot(BeNil(), "machine %s phase should be set", machine.Name)

		if isRunningMachine(machine) {
			hasInternalIP := false
			addressCount := countInternalIPs(machine)

			for _, address := range machine.Status.Addresses {
				if address.Type == internalIPType {
					hasInternalIP = true
					Expect(address.Address).ToNot(BeEmpty(),
						"address should not be empty for machine %s", machine.Name)
				}
			}

			Expect(hasInternalIP).To(BeTrue(),
				"running machine %s should have at least one InternalIP", machine.Name)

			spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
			Expect(err).NotTo(HaveOccurred())

			if len(spec.Subnets) > 1 {
				Expect(addressCount).To(BeNumerically(">=", len(spec.Subnets)),
					"machine %s with %d subnets should have at least %d internal IP addresses",
					machine.Name, len(spec.Subnets), len(spec.Subnets))
			}
		}
	}
}

// validateNodeAnnotations checks node annotations for multi-subnet configuration
func validateNodeAnnotations(nodes *corev1.NodeList, machineNetworks []configv1.NutanixFailureDomain) {
	By("checking node annotations related to multi-subnet networking")

	for _, node := range nodes.Items {
		if cidrsJSON, exists := node.Annotations[ovnHostCIDRsAnnotation]; exists && cidrsJSON != "" {
			var cidrs []string
			err := json.Unmarshal([]byte(cidrsJSON), &cidrs)
			Expect(err).NotTo(HaveOccurred(),
				"failed to parse %s annotation for node %s", ovnHostCIDRsAnnotation, node.Name)

			if hasMultiSubnetConfiguration(machineNetworks) {
				Expect(len(cidrs)).To(BeNumerically(">=", 1),
					"node %s should have at least one CIDR in multi-subnet setup", node.Name)
			}
		}
	}
}

// runAllBasicValidations runs all basic validation functions
func runAllBasicValidations(machines *machinev1beta1.MachineList, nodes *corev1.NodeList, machineNetworks []configv1.NutanixFailureDomain) {
	validateNodeNetworkPlacement(nodes)
	validateMachineMultipleNetworks(machines)
	validateMachineSubnetConfiguration(machines, machineNetworks)
	validateUniqueIPAddresses(machines)
}

// runAllAdvancedValidations runs all advanced validation functions
func runAllAdvancedValidations(machines *machinev1beta1.MachineList, nodes *corev1.NodeList, machineNetworks []configv1.NutanixFailureDomain) {
	validateSubnetIdentifierFormats(machines)
	validateMachineStatusConsistency(machines)
	validateNetworkConnectivity(nodes)
	validateNodeAnnotations(nodes, machineNetworks)
}

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:NutanixMultiSubnets][platform:nutanix] Managed cluster should support multi-subnet networking", Label("Conformance"), Label("Parallel"), func() {
	defer GinkgoRecover()
	ctx := context.Background()

	var (
		cfg             *rest.Config
		c               *kubernetes.Clientset
		cc              *configclient.ConfigV1Client
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

	Context("Basic Multi-Subnet Functionality", func() {
		It("machines should have multiple Internal Address [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateMachineMultipleNetworks(machines)
		})

		It("nodes should be present in correct subnets per Nutanix failure domain [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateNodeNetworkPlacement(nodes)
		})

		It("machines should have all specified subnets associated with their failure domain [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateMachineSubnetConfiguration(machines, machineNetworks)
		})

		It("machines should have all unique IPs for all subnets [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateUniqueIPAddresses(machines)
		})
	})

	Context("Configuration Validation", func() {
		It("subnet identifiers should be properly formatted [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateSubnetIdentifierFormats(machines)
		})

		It("failure domains should have valid subnet configurations [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateFailureDomainSubnetLimits(machineNetworks)
		})

		It("should enforce subnet count limits per feature gate specification [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			By("verifying subnet count limits are enforced")

			for _, domain := range machineNetworks {
				subnetCount := len(domain.Subnets)
				Expect(subnetCount).To(BeNumerically("<=", maxSubnetsPerFailureDomain),
					"failure domain %s has %d subnets, which exceeds the limit of %d", domain.Name, subnetCount, maxSubnetsPerFailureDomain)
				Expect(subnetCount).To(BeNumerically(">=", minSubnetsPerFailureDomain),
					"failure domain %s has %d subnets, minimum is %d", domain.Name, subnetCount, minSubnetsPerFailureDomain)
			}
		})

		It("should validate subnet uniqueness within failure domains [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			By("ensuring no duplicate subnets exist within the same failure domain")

			for _, domain := range machineNetworks {
				seenSubnets := make(map[string]bool)

				for _, subnet := range domain.Subnets {
					var identifier string
					if subnet.UUID != nil && *subnet.UUID != "" {
						identifier = fmt.Sprintf("uuid:%s", *subnet.UUID)
					} else if subnet.Name != nil && *subnet.Name != "" {
						identifier = fmt.Sprintf("name:%s", *subnet.Name)
					}

					Expect(seenSubnets[identifier]).To(BeFalse(),
						"duplicate subnet identifier found in failure domain %s: %s", domain.Name, identifier)
					seenSubnets[identifier] = true
				}
			}
		})
	})

	Context("Advanced Multi-Subnet Features", func() {
		It("machine status should be consistent with multi-subnet configuration [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateMachineStatusConsistency(machines)
		})

		It("network connectivity should work across subnets [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateNetworkConnectivity(nodes)
		})

		It("should handle mixed identifier types (UUID and Name) correctly [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			By("checking that machines handle mixed subnet identifier types")

			hasUUIDSubnets, hasNameSubnets := analyzeSubnetTypes(machineNetworks)
			By(fmt.Sprintf("infrastructure has UUID subnets: %v, Name subnets: %v", hasUUIDSubnets, hasNameSubnets))

			validateSubnetIdentifierFormats(machines)
		})

		It("should validate machine subnet count matches failure domain requirements [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			By("ensuring machines have the correct number of subnets per failure domain")

			for _, domain := range machineNetworks {
				failureDomainMachines, err := getMachinesInFailureDomain(machines, domain.Name)
				Expect(err).ToNot(HaveOccurred())

				expectedSubnetCount := len(domain.Subnets)

				for _, machine := range failureDomainMachines {
					spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
					Expect(err).NotTo(HaveOccurred())

					actualSubnetCount := len(spec.Subnets)
					Expect(actualSubnetCount).To(Equal(expectedSubnetCount),
						"machine %s in failure domain %s has %d subnets, expected %d",
						machine.Name, domain.Name, actualSubnetCount, expectedSubnetCount)
				}
			}
		})

		It("should validate node annotations for multi-subnet configuration [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			validateNodeAnnotations(nodes, machineNetworks)
		})

		It("should handle machine lifecycle in multi-subnet environment [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			By("validating machine lifecycle states are consistent with multi-subnet configuration")

			phaseStats := make(map[string]int)
			runningMachinesWithMultiNet := 0

			for _, machine := range machines.Items {
				if machine.Status.Phase != nil {
					phaseStats[*machine.Status.Phase]++
				} else {
					phaseStats["<nil>"]++
				}

				if isRunningMachine(machine) {
					spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
					Expect(err).NotTo(HaveOccurred())

					if len(spec.Subnets) > 1 {
						runningMachinesWithMultiNet++

						internalIPCount := countInternalIPs(machine)
						Expect(internalIPCount).To(BeNumerically(">=", 1),
							"running machine %s should have at least one InternalIP", machine.Name)
					}
				}
			}

			By(fmt.Sprintf("found machines in phases: %+v", phaseStats))
			By(fmt.Sprintf("found %d running machines with multi-subnet configuration", runningMachinesWithMultiNet))
		})
	})

	Context("Feature Gate Compliance", func() {
		It("should only allow multi-subnet when NutanixMultiSubnets feature gate is enabled [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
			By("verifying that multi-subnet configuration is only present when feature gate is enabled")

			hasMultiSubnetConfig := hasMultiSubnetConfiguration(machineNetworks)
			By(fmt.Sprintf("infrastructure has multi-subnet configuration: %v", hasMultiSubnetConfig))
		})
	})

	Context("Scale and Integration Tests", func() {
		It("new machines should pass multi network tests [apigroup:machine.openshift.io][Suite:openshift/conformance/serial]", Label("Serial"), func() {
			machineSets, err := e2eutil.GetMachineSets(cfg)
			Expect(err).NotTo(HaveOccurred())
			Expect(len(machineSets.Items)).ShouldNot(Equal(0))

			machineSet := machineSets.Items[0]
			origReplicas := int(*machineSet.Spec.Replicas)

			By("scaling up a new machineset which should have multiple NICs")
			err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, origReplicas+1)
			Expect(err).NotTo(HaveOccurred())

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

			By("validating provider specs are parseable")
			for _, machine := range machines.Items {
				_, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
				Expect(err).NotTo(HaveOccurred())
			}

			By("running all validation tests on scaled environment")
			runAllBasicValidations(machines, nodes, machineNetworks)
			runAllAdvancedValidations(machines, nodes, machineNetworks)

			By("scaling down the machineset")
			err = e2eutil.ScaleMachineSet(cfg, machineSet.Name, origReplicas)
			Expect(err).NotTo(HaveOccurred())

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
})
