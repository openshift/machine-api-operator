package vsphere

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"path"
	"slices"
	"strconv"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/machine/v1beta1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/pkg/errors"
	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	vapirest "github.com/vmware/govmomi/vapi/rest"
	"github.com/vmware/govmomi/vapi/tags"
	"github.com/vmware/govmomi/vim25"
	"github.com/vmware/govmomi/vim25/mo"
	"github.com/vmware/govmomi/vim25/soap"
	"github.com/vmware/govmomi/vim25/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"

	"github.com/openshift/machine-api-operator/pkg/controller/vsphere"
	util "github.com/openshift/machine-api-operator/test/e2e"
)

func isIpInCidrRange(ip string, cidr string) (bool, error) {
	parsed := net.ParseIP(ip)
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, fmt.Errorf("unable to parse address: %v", err)
	}

	return ipnet.Contains(parsed), nil
}

func getNodesInFailureDomain(vsphereInfraSpec *configv1.VSpherePlatformSpec,
	fd configv1.VSpherePlatformFailureDomainSpec,
	nodes *corev1.NodeList) ([]corev1.Node, error) {
	if len(vsphereInfraSpec.FailureDomains) < 2 {
		return nodes.Items, nil
	}

	By("getting nodes in failure domain")

	var nodesInFd []corev1.Node
	region := fd.Region
	zone := fd.Zone

	for _, node := range nodes.Items {
		var nodeZone string
		var nodeRegion string
		var exists bool
		if node.ObjectMeta.Labels == nil {
			continue
		}

		if nodeZone, exists = node.ObjectMeta.Labels["topology.kubernetes.io/zone"]; !exists {
			continue
		}
		if nodeRegion, exists = node.ObjectMeta.Labels["topology.kubernetes.io/region"]; !exists {
			continue
		}

		if region == nodeRegion && zone == nodeZone {
			nodesInFd = append(nodesInFd, node)
		}
	}

	return nodesInFd, nil
}

func getMachinesInFailureDomain(vsphereInfraSpec *configv1.VSpherePlatformSpec,
	fd configv1.VSpherePlatformFailureDomainSpec,
	machines *machinev1beta1.MachineList) ([]machinev1beta1.Machine, error) {

	By("getting machines in failure domain")

	if len(vsphereInfraSpec.FailureDomains) < 2 {
		return machines.Items, nil
	}
	var machinesInFd []machinev1beta1.Machine
	region := fd.Region
	zone := fd.Zone

	for _, machine := range machines.Items {
		var machineZone string
		var machineRegion string
		var exists bool
		if machine.ObjectMeta.Labels == nil {
			continue
		}

		if machineZone, exists = machine.ObjectMeta.Labels["machine.openshift.io/zone"]; !exists {
			continue
		}
		if machineRegion, exists = machine.ObjectMeta.Labels["machine.openshift.io/region"]; !exists {
			continue
		}

		if region == machineRegion && zone == machineZone {
			machinesInFd = append(machinesInFd, machine)
		}
	}

	return machinesInFd, nil
}

func getProviderFromMachineSet(cfg *rest.Config) *v1beta1.VSphereMachineProviderSpec {

	By("getting provider from machineset")

	workerMachineSets, err := util.GetMachineSets(cfg)
	Expect(err).NotTo(HaveOccurred())
	Expect(len(workerMachineSets.Items)).NotTo(Equal(0), "cluster should have at least 1 worker machine set created by installer")

	provider, err := vsphere.ProviderSpecFromRawExtension(workerMachineSets.Items[0].Spec.Template.Spec.ProviderSpec.Value)
	Expect(err).NotTo(HaveOccurred())

	return provider
}

// NewFinder creates a new client that conforms with the Finder interface and returns a
// vmware govmomi finder object that can be used to search for resources in vsphere.
func NewFinder(client *vim25.Client, all ...bool) *find.Finder {
	return find.NewFinder(client, all...)
}

// ClientLogout is empty function that logs out of vSphere clients
type ClientLogout func()

// CreateVSphereClients creates the SOAP and REST client to access
// different portions of the vSphere API
// e.g. tags are only available in REST
func CreateVSphereClients(ctx context.Context, vcenter, username, password string) (*vim25.Client, *vapirest.Client, ClientLogout, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	u, err := soap.ParseURL(vcenter)
	if err != nil {
		return nil, nil, nil, err
	}
	u.User = url.UserPassword(username, password)
	c, err := govmomi.NewClient(ctx, u, true)

	if err != nil {
		return nil, nil, nil, err
	}

	restClient := vapirest.NewClient(c.Client)
	err = restClient.Login(ctx, u.User)
	if err != nil {
		logoutErr := c.Logout(context.TODO())
		if logoutErr != nil {
			err = logoutErr
		}
		return nil, nil, nil, err
	}

	return c.Client, restClient, func() {
		c.Logout(ctx)
		restClient.Logout(ctx)
	}, nil
}

// getNetworks returns a slice of Managed Object references for networks in the given vSphere Cluster.
func getNetworks(ctx context.Context, ccr *object.ClusterComputeResource) ([]types.ManagedObjectReference, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var ccrMo mo.ClusterComputeResource

	err := ccr.Properties(ctx, ccr.Reference(), []string{"network"}, &ccrMo)
	if err != nil {
		return nil, errors.Wrap(err, "could not get properties of cluster")
	}
	return ccrMo.Network, nil
}

// GetClusterNetworks returns a slice of Managed Object references for vSphere networks in the given Datacenter
// and Cluster.
func GetClusterNetworks(ctx context.Context, finder *find.Finder, datacenter, cluster string) ([]types.ManagedObjectReference, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ccr, err := finder.ClusterComputeResource(context.TODO(), cluster)
	if err != nil {
		return nil, errors.Wrapf(err, "could not find vSphere cluster at %s", cluster)
	}

	// Get list of Networks inside vSphere Cluster
	networks, err := getNetworks(ctx, ccr)
	if err != nil {
		return nil, err
	}

	return networks, nil
}

// GetNetworkName returns the name of a vSphere network given its Managed Object reference.
func GetNetworkName(ctx context.Context, client *vim25.Client, ref types.ManagedObjectReference) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	netObj := object.NewNetwork(client, ref)
	name, err := netObj.ObjectName(ctx)
	if err != nil {
		return "", errors.Wrapf(err, "could not get network name for %s", ref.String())
	}
	return name, nil
}

// GetNetworkMo returns the unique Managed Object for given network name inside of the given Datacenter
// and Cluster.
func GetNetworkMo(ctx context.Context, client *vim25.Client, finder *find.Finder, datacenter, cluster, network string) (*types.ManagedObjectReference, error) {
	networks, err := GetClusterNetworks(ctx, finder, datacenter, cluster)
	if err != nil {
		return nil, err
	}
	for _, net := range networks {
		name, err := GetNetworkName(ctx, client, net)
		if err != nil {
			return nil, err
		}
		if name == network {
			return &net, nil
		}
	}

	return nil, errors.Errorf("unable to find network provided")
}

func getCredentialsForVCenter(ctx context.Context, vsphereCreds *corev1.Secret, vCenter configv1.VSpherePlatformVCenterSpec) (username, password string, err error) {
	vcenterUrl := vCenter.Server

	if val, exists := vsphereCreds.Data[fmt.Sprintf("%s.username", vcenterUrl)]; !exists {
		return "", "", errors.New("unable to find username in secret")
	} else {
		username = string(val)
	}
	if val, exists := vsphereCreds.Data[fmt.Sprintf("%s.password", vcenterUrl)]; !exists {
		return "", "", errors.New("unable to find password in secret")
	} else {
		password = string(val)
	}

	return username, password, nil
}

func GetPortGroupsAttachedToVMsInFailureDomain(
	ctx context.Context,
	clusterID string,
	failureDomain configv1.VSpherePlatformFailureDomainSpec,
	vsphereCreds *corev1.Secret,
	vCenters []configv1.VSpherePlatformVCenterSpec) (map[string][]string, error) {

	var vcenterUrl string
	var vcenter configv1.VSpherePlatformVCenterSpec

	for _, vcenter = range vCenters {
		if failureDomain.Server == vcenter.Server {
			if vcenter.Port == 0 {
				vcenter.Port = 443
			}
			vcenterUrl = fmt.Sprintf("https://%s", net.JoinHostPort(vcenter.Server, strconv.Itoa(int(vcenter.Port))))
			break
		}
	}

	if len(vcenterUrl) == 0 {
		return nil, fmt.Errorf("unable to find vCenter %s", failureDomain.Server)
	}

	user, pass, err := getCredentialsForVCenter(ctx, vsphereCreds, vcenter)
	Expect(err).NotTo(HaveOccurred())

	portGroupMap := make(map[string][]string)

	client, rClient, logout, err := CreateVSphereClients(ctx, vcenterUrl, user, pass)
	defer logout()

	if err != nil {
		return nil, fmt.Errorf("unable to create vSphere clients. %v", err)
	}

	finder := NewFinder(client)

	// Get tagged objects and then grab VMs
	tm := tags.NewManager(rClient)
	refs, err := tm.ListAttachedObjects(ctx, clusterID)
	var vmList []types.ManagedObjectReference
	for _, ref := range refs {
		if ref.Reference().Type == "VirtualMachine" {
			vmList = append(vmList, ref.Reference())
		}
	}

	vms, err := finder.VirtualMachineList(ctx, fmt.Sprintf("/%s/vm/...", failureDomain.Topology.Datacenter))
	if err != nil {
		return nil, fmt.Errorf("unable to get VMs in %s. %v", failureDomain.Topology.ComputeCluster, err)
	}

	for _, vm := range vms {
		// Make sure vm has the cluster id tag.  Note: template will be included.
		if !slices.Contains(vmList, vm.Reference()) {
			continue
		}

		vm, err := finder.VirtualMachine(ctx, vm.InventoryPath)
		if err != nil {
			return nil, fmt.Errorf("unable to get VMs in %s. %v", failureDomain.Topology.ComputeCluster, err)
		}
		virtualDevices, err := vm.Device(ctx)

		var portGroups []string
		for _, virtualDevice := range virtualDevices {
			if nic, ok := virtualDevice.(types.BaseVirtualEthernetCard); ok {
				backing := nic.GetVirtualEthernetCard().Backing

				switch b := backing.(type) {
				case *types.VirtualEthernetCardNetworkBackingInfo:
					fmt.Printf("NIC is connected to standard port group: %s\n", b.DeviceName)
				case *types.VirtualEthernetCardDistributedVirtualPortBackingInfo:
					fmt.Printf("NIC is connected to distributed port group: %s\n", b.Port.PortgroupKey)
					portgroup, err := finder.Network(ctx, b.Port.PortgroupKey)
					if err != nil {
						fmt.Printf("Failed to resolve port group key: %s\n", b.Port.PortgroupKey)
					} else {
						fmt.Printf("NIC connected to distributed port group: %s\n", portgroup.GetInventoryPath())
						portGroups = append(portGroups, path.Base(portgroup.GetInventoryPath()))
					}
				}
			}
		}
		portGroupMap[vm.UUID(ctx)] = portGroups
	}

	return portGroupMap, nil
}
