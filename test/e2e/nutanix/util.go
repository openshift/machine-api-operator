package nutanix

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"

	machinev1 "github.com/openshift/api/machine/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

const (
	machineReadyTimeout = time.Minute * 6
)

func isIpInCidrRange(ip string, cidr string) (bool, error) {
	parsed := net.ParseIP(ip)
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, fmt.Errorf("unable to parse address: %v", err)
	}

	return ipnet.Contains(parsed), nil
}

func getNodesInFailureDomainNutanix(fd configv1.NutanixFailureDomain, nodeList *corev1.NodeList) ([]corev1.Node, error) {
	const labelKey = "topology.kubernetes.io/zone" // or e.g. "failure-domain.nutanix.openshift.io/name"
	var result []corev1.Node
	for _, node := range nodeList.Items {
		if zone, ok := node.Labels[labelKey]; ok && zone == fd.Name {
			result = append(result, node)
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no node in failure domain: %v", fd.Name)
	}
	return result, nil
}

func getNodeNutanixSubnet(node corev1.Node) string {
	return node.Spec.PodCIDR
}

func getMachinesInFailureDomainNutanix(fd configv1.NutanixFailureDomain, machines *machinev1beta1.MachineList) (*machinev1beta1.MachineList, error) {
	const labelKey = "topology.kubernetes.io/zone"
	mach := &machinev1beta1.MachineList{}
	for _, machine := range machines.Items {
		if zone, ok := machine.Labels[labelKey]; ok && zone == fd.Name {
			mach.Items = append(mach.Items, machine)
		}
	}
	if len(mach.Items) == 0 {
		return nil, fmt.Errorf("no machines in failure domain: %v", fd.Name)
	}
	return mach, nil
}

func ProviderSpecFromRawExtension(rawExtension *runtime.RawExtension) (*machinev1.NutanixMachineProviderConfig, error) {
	if rawExtension == nil {
		return &machinev1.NutanixMachineProviderConfig{}, nil
	}

	spec := new(machinev1.NutanixMachineProviderConfig)
	if err := json.Unmarshal(rawExtension.Raw, &spec); err != nil {
		return nil, fmt.Errorf("error unmarshalling providerSpec: %v", err)
	}

	klog.V(5).Infof("Got provider spec from raw extension: %+v", spec)
	return spec, nil
}
