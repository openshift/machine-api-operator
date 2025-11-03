package nutanix

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

const (
	machineReadyTimeout = time.Minute * 6

	// Multi-subnet test constants
	ovnHostCIDRsAnnotation     = "k8s.ovn.org/host-cidrs"
	internalIPType             = "InternalIP"
	externalIPType             = "ExternalIP"
	runningPhase               = "Running"
	maxSubnetsPerFailureDomain = 32
	minSubnetsPerFailureDomain = 1
	minUUIDLength              = 36
)

func isIpInCidrRange(ip string, cidr string) (bool, error) {
	parsed := net.ParseIP(ip)
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return false, fmt.Errorf("unable to parse address: %v", err)
	}

	return ipnet.Contains(parsed), nil
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

func getMachinesInFailureDomain(machines *machinev1beta1.MachineList, failureDomainName string) ([]machinev1beta1.Machine, error) {
	failureDomainMachines := []machinev1beta1.Machine{}
	for _, machine := range machines.Items {
		spec, err := ProviderSpecFromRawExtension(machine.Spec.ProviderSpec.Value)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling providerSpec: %v", err)
		}
		if spec.FailureDomain.Name == failureDomainName {
			failureDomainMachines = append(failureDomainMachines, machine)
		}
	}
	return failureDomainMachines, nil
}

// getSubnetIdentifier extracts the identifier (UUID or Name) from a subnet resource identifier
func getSubnetIdentifier(subnet configv1.NutanixResourceIdentifier) (string, error) {
	if subnet.UUID != nil && *subnet.UUID != "" {
		return *subnet.UUID, nil
	}
	if subnet.Name != nil && *subnet.Name != "" {
		return *subnet.Name, nil
	}
	return "", fmt.Errorf("subnet must have either UUID or Name set")
}

// getMachineSubnetIdentifier extracts the identifier (UUID or Name) from a machine subnet resource identifier
func getMachineSubnetIdentifier(subnet machinev1.NutanixResourceIdentifier) (string, error) {
	if subnet.UUID != nil && *subnet.UUID != "" {
		return *subnet.UUID, nil
	}
	if subnet.Name != nil && *subnet.Name != "" {
		return *subnet.Name, nil
	}
	return "", fmt.Errorf("machine subnet must have either UUID or Name set")
}

// countInternalIPs counts the number of InternalIP addresses for a machine
func countInternalIPs(machine machinev1beta1.Machine) int {
	count := 0
	for _, address := range machine.Status.Addresses {
		if address.Type == internalIPType {
			count++
		}
	}
	return count
}

// isRunningMachine checks if a machine is in Running phase
func isRunningMachine(machine machinev1beta1.Machine) bool {
	return machine.Status.Phase != nil && *machine.Status.Phase == runningPhase
}

// analyzeSubnetTypes analyzes what types of subnet identifiers are in use
func analyzeSubnetTypes(machineNetworks []configv1.NutanixFailureDomain) (hasUUID, hasName bool) {
	for _, domain := range machineNetworks {
		for _, subnet := range domain.Subnets {
			if subnet.UUID != nil && *subnet.UUID != "" {
				hasUUID = true
			}
			if subnet.Name != nil && *subnet.Name != "" {
				hasName = true
			}
		}
	}
	return
}

// hasMultiSubnetConfiguration checks if any failure domain has multiple subnets
func hasMultiSubnetConfiguration(machineNetworks []configv1.NutanixFailureDomain) bool {
	for _, domain := range machineNetworks {
		if len(domain.Subnets) > 1 {
			return true
		}
	}
	return false
}
