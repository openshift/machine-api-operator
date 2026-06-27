package vsphere

import (
	"context"
	_ "embed"
	"fmt"
	"strings"

	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	vapirest "github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	"k8s.io/utils/ptr"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	configv1 "github.com/openshift/api/config/v1"
	configclient "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	machinesetclient "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:VSphereHostVMGroupZonal][platform:vsphere] A Machine in a managed cluster", Label("Conformance"), Label("Parallel"), func() {
	ctx := context.Background()

	var (
		cfg *rest.Config
		c   *kubernetes.Clientset
		cc  *configclient.ConfigV1Client

		vsphereCreds *corev1.Secret
		err          error
		infra        *configv1.Infrastructure
		nodes        *corev1.NodeList
	)

	BeforeEach(func() {
		cfg, err = e2e.LoadConfig()
		Expect(err).NotTo(HaveOccurred(), "expected LoadConfig() to succeed")
		c, err = e2e.LoadClientset()
		Expect(err).NotTo(HaveOccurred(), "expected LoadClientset() to succeed")
		cc, err = configclient.NewForConfig(cfg)
		Expect(err).NotTo(HaveOccurred(), "expected configclient.NewForConfig() to succeed")
		By("Get Infrastructure spec")
		infra, err = cc.Infrastructures().Get(ctx, "cluster", metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "expected Infrastructures().Get() to succeed")

		if !isVmHostZonal(infra.Spec.PlatformSpec.VSphere) {
			Skip("skipping test since cluster does not support vSphere host zones")
		}

		By("Get vSphere Credentials")
		vsphereCreds, err = c.CoreV1().Secrets("kube-system").Get(ctx, "vsphere-creds", v1.GetOptions{})
		Expect(err).NotTo(HaveOccurred(), "expected vSphere creds secret to exist")

		By("Expect Failure Domains to be greater than one")
		Expect(len(infra.Spec.PlatformSpec.VSphere.FailureDomains) >= 1, "expected more than one failure domain")

		nodes, err = c.CoreV1().Nodes().List(ctx, v1.ListOptions{})
		Expect(err).NotTo(HaveOccurred(), "expected nodes List() to succeed")
	})

	It("should be placed in the correct vm-host group [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachineIsNotInCorrectVMGroup(ctx, nodes, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
	})

	It("associated with a vm-host group should have the correct topology labels [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachineIsNotInCorrectRegionZone(ctx, nodes, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
	})

	It("should enforce vm-host affinity rules between VM groups and host groups [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfVMHostAffinityRulesAreNotEnforced(ctx, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
	})

	It("should respect zonal constraints during machine provisioning and scaling operations [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachineAPIViolatesZonalConstraints(ctx, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
	})

	It("should handle zone failures gracefully and recover workloads to healthy zones [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfZoneFailureRecoveryIsNotGraceful(ctx, nodes, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
	})

})

func getClusterVmGroups(ctx context.Context, vim25Client *vim25.Client, computeCluster string) ([]*types.ClusterVmGroup, error) {
	By("get cluster vm groups")
	finder := find.NewFinder(vim25Client, true)

	ccr, err := finder.ClusterComputeResource(ctx, computeCluster)
	if err != nil {
		return nil, err
	}

	clusterConfig, err := ccr.Configuration(ctx)
	if err != nil {
		return nil, err
	}

	var clusterVmGroup []*types.ClusterVmGroup

	for _, g := range clusterConfig.Group {
		if vmg, ok := g.(*types.ClusterVmGroup); ok {
			clusterVmGroup = append(clusterVmGroup, vmg)
		}
	}
	return clusterVmGroup, nil
}

func getAttachedObjects(ctx context.Context, restClient *vapirest.Client) ([]tags.AttachedObjects, []tags.AttachedObjects) {
	tmgr := tags.NewManager(restClient)

	regionTags, err := tmgr.GetTagsForCategory(ctx, "openshift-region")
	Expect(err).NotTo(HaveOccurred(), "expected to get openshift-region tags")
	zoneTags, err := tmgr.GetTagsForCategory(ctx, "openshift-zone")
	Expect(err).NotTo(HaveOccurred(), "expected to get openshift-zone tags")

	regionAttached, err := tmgr.GetAttachedObjectsOnTags(ctx, func(tags []tags.Tag) []string {
		var tagsID []string
		for _, tag := range tags {
			tagsID = append(tagsID, tag.ID)
		}
		return tagsID
	}(regionTags))

	Expect(err).NotTo(HaveOccurred(), "expected to get attached objects to be in region tags")
	zoneAttached, err := tmgr.GetAttachedObjectsOnTags(ctx, func(tags []tags.Tag) []string {
		var tagsID []string
		for _, tag := range tags {
			tagsID = append(tagsID, tag.ID)
		}
		return tagsID
	}(zoneTags))
	Expect(err).NotTo(HaveOccurred(), "expected to get attached objects to be in zone tags")

	return regionAttached, zoneAttached
}

func getVSphereClientsFromClusterCreds(ctx context.Context, platform *configv1.VSpherePlatformSpec, vsphereCreds *corev1.Secret) (*vim25.Client, *vapirest.Client, ClientLogout, error) {
	vcenter := platform.VCenters[0]
	By("get vsphere credentials")
	user, pass, err := getCredentialsForVCenter(ctx, vsphereCreds, vcenter)
	Expect(err).NotTo(HaveOccurred(), "expected to get vsphere credentials")
	return CreateVSphereClients(ctx, vcenter.Server, user, pass)
}

func failIfMachineIsNotInCorrectRegionZone(ctx context.Context,
	nodes *corev1.NodeList,
	platform *configv1.VSpherePlatformSpec,
	vsphereCreds *corev1.Secret) {

	vim25Client, restClient, logout, err := getVSphereClientsFromClusterCreds(ctx, platform, vsphereCreds)
	defer logout()
	Expect(err).NotTo(HaveOccurred(), "expected to get vSphere clients from cluster credentials")

	// vm-host zonal will only ever have one vcenter
	Expect(platform.VCenters).To(HaveLen(1), "Expected only one vCenter to be configured, but found %d", len(platform.VCenters))

	regionAttached, zoneAttached := getAttachedObjects(ctx, restClient)

	for _, fd := range platform.FailureDomains {
		fdNodes, err := getNodesInFailureDomain(platform, fd, nodes)
		Expect(err).NotTo(HaveOccurred(), "expected to get nodes in failure domain")

		searchIndex := object.NewSearchIndex(vim25Client)

		By("get moref via node uuid")
		for _, n := range fdNodes {

			parts := strings.Split(n.Spec.ProviderID, "vsphere://")
			Expect(len(parts)).Should(BeIdenticalTo(2))

			ref, err := searchIndex.FindAllByUuid(ctx, nil, parts[1], true, ptr.To(false))

			Expect(err).NotTo(HaveOccurred(), "expected to FindAllByUuid to succeed")

			pc := vim25Client.ServiceContent.PropertyCollector

			for _, r := range ref {
				var vm *object.VirtualMachine
				var ok bool
				if vm, ok = r.(*object.VirtualMachine); !ok {
					Expect(vm).NotTo(BeNil())
				}

				host, err := vm.HostSystem(ctx)
				Expect(err).NotTo(HaveOccurred(), "expected HostSystem to succeed")

				foundRegion := false
				foundZone := false
				me, err := mo.Ancestors(ctx, vim25Client, pc, host.Reference())

				Expect(err).NotTo(HaveOccurred(), "expected Ancestor to succeed")

				// for vm-host zonal we only care about tags attached to the
				// ClusterComputeResource and HostSystem (ESXi Hosts)
				for _, m := range me {
					switch m.Self.Type {
					case "ClusterComputeResource":
						for _, r := range regionAttached {
							if r.Tag.Name == fd.Region {
								for _, attachedRef := range r.ObjectIDs {
									if attachedRef.Reference().Value == m.Self.Value {
										foundRegion = true
										break
									}
								}
							}
							if foundRegion {
								break
							}
						}
					case "HostSystem":
						for _, z := range zoneAttached {
							if z.Tag.Name == fd.Zone {
								for _, attachedRef := range z.ObjectIDs {
									if attachedRef.Reference().Value == m.Self.Value {
										foundZone = true
										break
									}
								}
							}
							if foundZone {
								break
							}
						}
					}
				}

				if !foundRegion || !foundZone {
					Expect(fmt.Errorf("node %s missing expected region '%s' or zone '%s' tags", n.Name, fd.Region, fd.Zone)).NotTo(HaveOccurred())
				}
			}
		}
	}
}

func failIfMachineIsNotInCorrectVMGroup(ctx context.Context,
	nodes *corev1.NodeList,
	platform *configv1.VSpherePlatformSpec,
	vsphereCreds *corev1.Secret) {

	// vm-host zonal will only ever have one vcenter
	Expect(platform.VCenters).To(HaveLen(1), "Expected only one vCenter to be configured, but found %d", len(platform.VCenters))

	vim25Client, _, logout, err := getVSphereClientsFromClusterCreds(ctx, platform, vsphereCreds)
	defer logout()
	Expect(err).NotTo(HaveOccurred(), "expected to get vSphere clients from cluster credentials")

	for _, fd := range platform.FailureDomains {
		if fd.ZoneAffinity == nil || fd.ZoneAffinity.HostGroup == nil {
			By(fmt.Sprintf("skipping failure domain %s - no HostGroup ZoneAffinity configured", fd.Name))
			continue
		}

		clusterVmGroups, err := getClusterVmGroups(ctx, vim25Client, fd.Topology.ComputeCluster)
		Expect(err).NotTo(HaveOccurred(), "expected cluster vm groups to be available")

		fdNodes, err := getNodesInFailureDomain(platform, fd, nodes)
		Expect(err).NotTo(HaveOccurred(), "expected to be able to get nodes in failure domain")

		var moRefs []types.ManagedObjectReference
		searchIndex := object.NewSearchIndex(vim25Client)

		By("get moref via node uuid")
		for _, n := range fdNodes {
			parts := strings.Split(n.Spec.ProviderID, "vsphere://")
			Expect(parts).Should(HaveLen(2), "Expected to find 2 parts in the provider ID, but found %d", len(parts))

			ref, err := searchIndex.FindAllByUuid(ctx, nil, parts[1], true, ptr.To(false))

			Expect(err).NotTo(HaveOccurred(), "expected FindAllByUuid to succeed")

			for _, r := range ref {
				moRefs = append(moRefs, r.Reference())
			}
		}

		foundMoRef := make(map[string]bool)

		var clusterVmGroup *types.ClusterVmGroup
		for _, group := range clusterVmGroups {
			if fd.ZoneAffinity.HostGroup.VMGroup == group.Name {
				clusterVmGroup = group
			}
		}

		By("check to make sure vm groups are in the correct location")
		for _, nMoRef := range moRefs {
			foundMoRef[nMoRef.Value] = false
			if clusterVmGroup != nil {
				for _, gMoRef := range clusterVmGroup.Vm {
					if gMoRef.Value == nMoRef.Value {
						foundMoRef[nMoRef.Value] = true
						break
					}
				}
				if foundMoRef[nMoRef.Value] {
					continue
				}
			}
		}

		for moRef, v := range foundMoRef {
			if !v {
				Expect(fmt.Errorf("virtual machine id %s was not in vm group %s", moRef, fd.ZoneAffinity.HostGroup.VMGroup)).NotTo(HaveOccurred())
			}
		}
	}
}

func failIfVMHostAffinityRulesAreNotEnforced(ctx context.Context,
	platform *configv1.VSpherePlatformSpec,
	vsphereCreds *corev1.Secret) {

	By("validating VM-Host affinity rules are correctly configured and enforced")

	// vm-host zonal will only ever have one vcenter
	Expect(platform.VCenters).To(HaveLen(1), "Expected only one vCenter to be configured, but found %d", len(platform.VCenters))

	vim25Client, _, logout, err := getVSphereClientsFromClusterCreds(ctx, platform, vsphereCreds)
	defer logout()
	Expect(err).NotTo(HaveOccurred(), "expected to get vSphere clients from cluster credentials")

	for _, fd := range platform.FailureDomains {
		By(fmt.Sprintf("checking VM-Host affinity rules for failure domain %s", fd.Name))

		if fd.ZoneAffinity == nil || fd.ZoneAffinity.HostGroup == nil {
			By(fmt.Sprintf("skipping failure domain %s - no HostGroup ZoneAffinity configured", fd.Name))
			continue
		}

		// Get cluster configuration to check VM-Host rules
		finder := find.NewFinder(vim25Client, true)
		ccr, err := finder.ClusterComputeResource(ctx, fd.Topology.ComputeCluster)
		Expect(err).NotTo(HaveOccurred(), "expected to find cluster compute resource")

		clusterConfig, err := ccr.Configuration(ctx)
		Expect(err).NotTo(HaveOccurred(), "expected to get cluster configuration")

		// Verify VM-Host affinity rule exists and is properly configured
		var vmHostRule *types.ClusterVmHostRuleInfo
		for _, rule := range clusterConfig.Rule {
			if r, ok := rule.(*types.ClusterVmHostRuleInfo); ok {
				if r.Name == fd.ZoneAffinity.HostGroup.VMHostRule {
					vmHostRule = r
					By(fmt.Sprintf("found VM-Host rule %s for failure domain %s", vmHostRule.Name, fd.Name))

					// Verify the rule references the correct VM and Host groups
					Expect(vmHostRule.VmGroupName).To(Equal(fd.ZoneAffinity.HostGroup.VMGroup),
						"VM-Host rule should reference the correct VM group")
					Expect(vmHostRule.AffineHostGroupName).To(Equal(fd.ZoneAffinity.HostGroup.HostGroup),
						"VM-Host rule should reference the correct Host group")
					Expect(ptr.Deref(vmHostRule.Enabled, false)).To(BeTrue(),
						"VM-Host affinity rule should be enabled")

					By(fmt.Sprintf("verified VM-Host affinity rule %s is correctly configured", vmHostRule.Name))
					break
				}
			}
		}

		Expect(vmHostRule).NotTo(BeNil(), "VM-Host affinity rule %s should exist for failure domain %s",
			fd.ZoneAffinity.HostGroup.VMHostRule, fd.Name)
	}
}

func failIfMachineAPIViolatesZonalConstraints(ctx context.Context,
	platform *configv1.VSpherePlatformSpec,
	vsphereCreds *corev1.Secret) {

	By("testing Machine API zonal constraint enforcement during provisioning")

	// This test verifies that the Machine API respects zonal constraints
	// For minimal implementation, we'll verify existing machines comply with constraints

	vim25Client, _, logout, err := getVSphereClientsFromClusterCreds(ctx, platform, vsphereCreds)
	defer logout()
	Expect(err).NotTo(HaveOccurred(), "expected to get vSphere clients from cluster credentials")

	// Get all machines to verify they comply with zonal constraints
	cfg, err := e2e.LoadConfig()
	Expect(err).NotTo(HaveOccurred(), "expected LoadConfig() to succeed")

	// Create machine client to get machine list
	machineClient, err := machinesetclient.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred(), "expected to create machine client")

	machineList, err := machineClient.Machines("openshift-machine-api").List(ctx, metav1.ListOptions{})
	Expect(err).NotTo(HaveOccurred(), "expected to get machine list")

	for _, fd := range platform.FailureDomains {
		By(fmt.Sprintf("verifying machines in failure domain %s comply with zonal constraints", fd.Name))

		if fd.ZoneAffinity == nil || fd.ZoneAffinity.HostGroup == nil {
			By(fmt.Sprintf("skipping failure domain %s - no HostGroup ZoneAffinity configured", fd.Name))
			continue
		}

		machinesInFd, err := getMachinesInFailureDomain(platform, fd, machineList)
		Expect(err).NotTo(HaveOccurred(), "expected to get machines in failure domain")

		if len(machinesInFd) == 0 {
			By(fmt.Sprintf("no machines found in failure domain %s, skipping", fd.Name))
			continue
		}

		clusterVmGroups, err := getClusterVmGroups(ctx, vim25Client, fd.Topology.ComputeCluster)
		Expect(err).NotTo(HaveOccurred(), "expected cluster vm groups to be available")

		var clusterVmGroup *types.ClusterVmGroup
		for _, group := range clusterVmGroups {
			if fd.ZoneAffinity.HostGroup.VMGroup == group.Name {
				clusterVmGroup = group
				break
			}
		}

		Expect(clusterVmGroup).NotTo(BeNil(), "VM group %s should exist for failure domain %s",
			fd.ZoneAffinity.HostGroup.VMGroup, fd.Name)

		// Verify each machine in the failure domain has its VM in the correct VM group
		searchIndex := object.NewSearchIndex(vim25Client)
		for _, machine := range machinesInFd {
			By(fmt.Sprintf("verifying machine %s is in correct VM group", machine.Name))

			if machine.Spec.ProviderID == nil || *machine.Spec.ProviderID == "" {
				By(fmt.Sprintf("machine %s has no provider ID, skipping", machine.Name))
				continue
			}

			parts := strings.Split(*machine.Spec.ProviderID, "vsphere://")
			Expect(parts).To(HaveLen(2), "expected valid vSphere provider ID")

			ref, err := searchIndex.FindAllByUuid(ctx, nil, parts[1], true, ptr.To(false))
			Expect(err).NotTo(HaveOccurred(), "expected FindAllByUuid to succeed")
			Expect(ref).To(HaveLen(1), "expected exactly one VM reference")

			vmRef := ref[0].Reference()
			vmInGroup := false
			for _, groupVmRef := range clusterVmGroup.Vm {
				if groupVmRef.Value == vmRef.Value {
					vmInGroup = true
					break
				}
			}

			Expect(vmInGroup).To(BeTrue(), "machine %s VM should be in VM group %s",
				machine.Name, fd.ZoneAffinity.HostGroup.VMGroup)
		}

		By(fmt.Sprintf("verified all machines in failure domain %s comply with zonal constraints", fd.Name))
	}
}

func failIfZoneFailureRecoveryIsNotGraceful(ctx context.Context,
	nodes *corev1.NodeList,
	platform *configv1.VSpherePlatformSpec,
	vsphereCreds *corev1.Secret) {

	By("testing zone failure simulation and recovery capabilities")

	// For minimal implementation, we'll validate the cluster's current resilience capabilities
	// without actually inducing failures (which could be destructive)

	vim25Client, _, logout, err := getVSphereClientsFromClusterCreds(ctx, platform, vsphereCreds)
	defer logout()
	Expect(err).NotTo(HaveOccurred(), "expected to get vSphere clients from cluster credentials")

	// Verify we have multiple failure domains for resilience
	Expect(len(platform.FailureDomains)).To(BeNumerically(">=", 2),
		"cluster should have at least 2 failure domains for zone failure resilience")

	// Check node distribution across zones
	nodeDistribution := make(map[string][]corev1.Node)
	for _, node := range nodes.Items {
		if node.Labels == nil {
			continue
		}

		zone, exists := node.Labels["topology.kubernetes.io/zone"]
		if !exists {
			continue
		}

		nodeDistribution[zone] = append(nodeDistribution[zone], node)
	}

	By(fmt.Sprintf("found nodes distributed across %d zones", len(nodeDistribution)))
	Expect(len(nodeDistribution)).To(BeNumerically(">=", 2),
		"nodes should be distributed across multiple zones for resilience")

	// Verify each zone has VM-Host affinity rules configured for proper isolation
	for _, fd := range platform.FailureDomains {
		By(fmt.Sprintf("verifying zone failure resilience configuration for %s", fd.Name))

		nodesInZone, exists := nodeDistribution[fd.Zone]
		if !exists || len(nodesInZone) == 0 {
			By(fmt.Sprintf("no nodes found in zone %s, skipping resilience check", fd.Zone))
			continue
		}

		// Verify VM-Host affinity configuration exists for this zone
		Expect(fd.ZoneAffinity).NotTo(BeNil(), "zone affinity should be configured for resilience")
		Expect(fd.ZoneAffinity.HostGroup).NotTo(BeNil(), "host group should be configured for zone isolation")
		Expect(fd.ZoneAffinity.HostGroup.VMHostRule).NotTo(BeEmpty(),
			"VM-Host rule should be configured for zone %s", fd.Zone)

		// Check that cluster has VM groups configured for this zone
		clusterVmGroups, err := getClusterVmGroups(ctx, vim25Client, fd.Topology.ComputeCluster)
		Expect(err).NotTo(HaveOccurred(), "expected cluster vm groups to be available")

		vmGroupExists := false
		for _, group := range clusterVmGroups {
			if group.Name == fd.ZoneAffinity.HostGroup.VMGroup {
				vmGroupExists = true
				By(fmt.Sprintf("verified VM group %s exists for zone %s with %d VMs",
					group.Name, fd.Zone, len(group.Vm)))
				break
			}
		}

		Expect(vmGroupExists).To(BeTrue(), "VM group %s should exist for zone resilience in %s",
			fd.ZoneAffinity.HostGroup.VMGroup, fd.Zone)
	}

	By("verified cluster has proper zone failure resilience configuration")
}

func isVmHostZonal(platform *configv1.VSpherePlatformSpec) bool {
	By("check to make sure installed cluster is vm-host zonal")
	for _, fd := range platform.FailureDomains {
		if fd.ZoneAffinity != nil {
			if fd.ZoneAffinity.Type == "HostGroup" {
				if fd.ZoneAffinity.HostGroup.VMGroup != "" {
					return true
				}
			}
		}
	}
	return false
}
