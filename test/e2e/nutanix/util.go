package nutanix

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
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
			_ = append(failureDomainMachines, machine)
		}
	}
	return failureDomainMachines, nil
}
