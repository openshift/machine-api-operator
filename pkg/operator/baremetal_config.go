package operator

import (
	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamic "k8s.io/client-go/dynamic"
)

var provisioningGVR = schema.GroupVersionResource{Group: "metal3.io", Resource: "provisionings", Version: "v1alpha1"}
var baremetalProvisioningCR = "cluster"

// Provisioning Config needed to deploy Metal3 pod
type BaremetalProvisioningConfig struct {
	ProvisioningInterface    string
	ProvisioningIp           string
	ProvisioningNetworkCIDR  string
	ProvisioningDHCPExternal bool
	ProvisioningDHCPRange    string
}

func getBaremetalProvisioningConfig(dc dynamic.Interface, configName string) (BaremetalProvisioningConfig, error) {
	provisioningClient := dc.Resource(provisioningGVR)
	provisioningConfig, err := provisioningClient.Get(configName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Error getting config from Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningSpec, found, err := unstructured.NestedMap(provisioningConfig.UnstructuredContent(), "spec")
	if !found {
		glog.Errorf("Nested Spec not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningInterface, found, err := unstructured.NestedString(provisioningSpec, "provisioningInterface")
	if !found {
		glog.Errorf("provisioningInterface not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningIP, found, err := unstructured.NestedString(provisioningSpec, "provisioningIP")
	if !found {
		glog.Errorf("provisioningIP not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningNetworkCIDR, found, err := unstructured.NestedString(provisioningSpec, "provisioningNetworkCIDR")
	if !found {
		glog.Errorf("provisioningNetworkCIDR not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningDHCPExternal, found, err := unstructured.NestedBool(provisioningSpec, "provisioningDHCPExternal")
	if !found {
		glog.Errorf("provisioningDHCPExternal not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningDHCPRange, found, err := unstructured.NestedString(provisioningSpec, "provisioningDHCPRange")
	if !found {
		glog.Errorf("provisioningDHCPRange not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	return BaremetalProvisioningConfig{
		ProvisioningInterface:    provisioningInterface,
		ProvisioningIp:           provisioningIP,
		ProvisioningNetworkCIDR:  provisioningNetworkCIDR,
		ProvisioningDHCPExternal: provisioningDHCPExternal,
		ProvisioningDHCPRange:    provisioningDHCPRange,
	}, nil
}
