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

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("[sig-cluster-lifecycle][OCPFeatureGate:VSphereHostVMGroupZonal][platform:vsphere] Managed cluster should", func() {
	defer GinkgoRecover()
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

	It("machine should be in the correct vm-host group [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachineIsNotInCorrectVMGroup(ctx, nodes, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
	})

	It("vm-host group zone machine should have the correct topology labels [apigroup:machine.openshift.io][Suite:openshift/conformance/parallel]", func() {
		failIfMachineIsNotInCorrectRegionZone(ctx, nodes, infra.Spec.PlatformSpec.VSphere, vsphereCreds)
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

func failIfMachineIsNotInCorrectRegionZone(ctx context.Context,
	nodes *corev1.NodeList,
	platform *configv1.VSpherePlatformSpec,
	vsphereCreds *corev1.Secret) {

	// vm-host zonal will only ever have one vcenter
	Expect(len(platform.VCenters) == 1)

	vcenter := platform.VCenters[0]
	By("get vsphere credentials")
	user, pass, err := getCredentialsForVCenter(ctx, vsphereCreds, vcenter)
	Expect(err).NotTo(HaveOccurred(), "expected to get vsphere credentials")
	vim25Client, restClient, logout, err := CreateVSphereClients(ctx, vcenter.Server, user, pass)
	defer logout()
	Expect(err).NotTo(HaveOccurred(), "expected to get vsphere clients")

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
	Expect(len(platform.VCenters) == 1, "expected only one vCenter to be configured")

	vcenter := platform.VCenters[0]
	By("get vsphere credentials")
	user, pass, err := getCredentialsForVCenter(ctx, vsphereCreds, vcenter)
	Expect(err).NotTo(HaveOccurred(), "expected vCenter credentials to be correct and the secret to be available")

	vim25Client, _, logout, err := CreateVSphereClients(ctx, vcenter.Server, user, pass)
	defer logout()

	for _, fd := range platform.FailureDomains {
		clusterVmGroups, err := getClusterVmGroups(ctx, vim25Client, fd.Topology.ComputeCluster)
		Expect(err).NotTo(HaveOccurred(), "expected cluster vm groups to be available")

		fdNodes, err := getNodesInFailureDomain(platform, fd, nodes)
		Expect(err).NotTo(HaveOccurred(), "expected to be able to get nodes in failure domain")

		var moRefs []types.ManagedObjectReference
		searchIndex := object.NewSearchIndex(vim25Client)

		By("get moref via node uuid")
		for _, n := range fdNodes {
			parts := strings.Split(n.Spec.ProviderID, "vsphere://")
			Expect(len(parts)).Should(BeIdenticalTo(2), "expected to find 2 parts in provider id")

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

func isVmHostZonal(platform *configv1.VSpherePlatformSpec) bool {
	By("check to make sure installed cluster is vm-host zonal")
	for _, fd := range platform.FailureDomains {
		if fd.ZoneAffinity.Type == "HostGroup" {
			if fd.ZoneAffinity.HostGroup.VMGroup != "" {
				return true
			}
		}
	}
	return false
}
