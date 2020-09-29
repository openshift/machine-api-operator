package operator

import (
	"context"
	"fmt"
	"net"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamic "k8s.io/client-go/dynamic"
)

var provisioningGVR = schema.GroupVersionResource{Group: "metal3.io", Resource: "provisionings", Version: "v1alpha1"}
var provisioningGR = schema.GroupResource{Group: "metal3.io", Resource: "provisionings"}

const (
	baremetalProvisioningCR        = "provisioning-configuration"
	baremetalHttpPort              = "6180"
	baremetalIronicPort            = "6385"
	baremetalIronicInspectorPort   = "5050"
	baremetalKernelUrlSubPath      = "images/ironic-python-agent.kernel"
	baremetalRamdiskUrlSubPath     = "images/ironic-python-agent.initramfs"
	baremetalIronicEndpointSubpath = "v1/"
	provisioningNetworkManaged     = "Managed"
	provisioningNetworkUnmanaged   = "Unmanaged"
	provisioningNetworkDisabled    = "Disabled"
)

// Provisioning Config needed to deploy Metal3 pod
type BaremetalProvisioningConfig struct {
	ProvisioningInterface     string
	ProvisioningIp            string
	ProvisioningNetworkCIDR   string
	ProvisioningDHCPRange     string
	ProvisioningOSDownloadURL string
	ProvisioningNetwork       string
}

func reportError(found bool, err error, configItem string, configName string) error {
	if err != nil {
		return fmt.Errorf("Error while reading %s from Baremetal provisioning CR %s: %w", configItem, configName, err)
	}
	if !found {
		return fmt.Errorf("%s not found in Baremetal provisioning CR %s", configItem, configName)
	}
	return fmt.Errorf("Unknown Error while reading %s from Baremetal provisioning CR %s", configItem, configName)
}

func getBaremetalProvisioningConfig(dc dynamic.Interface, configName string) (*BaremetalProvisioningConfig, error) {
	provisioningClient := dc.Resource(provisioningGVR)
	provisioningConfig, err := provisioningClient.Get(context.Background(), configName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		// The provisioning CR is not present and that is not considered an error.
		return nil, nil
	}
	if err != nil {
		return nil, reportError(true, err, "provisioning configuration", configName)
	}
	provisioningSpec, found, err := unstructured.NestedMap(provisioningConfig.UnstructuredContent(), "spec")
	if !found || err != nil {
		return nil, reportError(found, err, "Spec field", configName)
	}
	provisioningInterface, found, err := unstructured.NestedString(provisioningSpec, "provisioningInterface")
	if !found || err != nil {
		return nil, reportError(found, err, "provisioningInterface", configName)
	}
	provisioningIP, found, err := unstructured.NestedString(provisioningSpec, "provisioningIP")
	if !found || err != nil {
		return nil, reportError(found, err, "provisioningIP", configName)
	}
	provisioningDHCPRange, found, err := unstructured.NestedString(provisioningSpec, "provisioningDHCPRange")
	if !found || err != nil {
		return nil, reportError(found, err, "provisioningDHCPRange", configName)
	}
	provisioningOSDownloadURL, found, err := unstructured.NestedString(provisioningSpec, "provisioningOSDownloadURL")
	if !found || err != nil {
		return nil, reportError(found, err, "provisioningOSDownloadURL", configName)
	}
	// If provisioningNetwork is not provided, set its value based on provisioningDHCPExternal
	provisioningNetwork, foundNetworkState, err := unstructured.NestedString(provisioningSpec, "provisioningNetwork")
	if err != nil {
		return nil, reportError(true, err, "provisioningNetwork", configName)
	}
	if !foundNetworkState {
		// Check if provisioningDHCPExternal is present in the config
		provisioningDHCPExternal, foundDHCP, err := unstructured.NestedBool(provisioningSpec, "provisioningDHCPExternal")
		if !foundDHCP || err != nil {
			// Both the new provisioningNetwork and the old provisioningDHCPExternal configs are not found.
			return nil, reportError(foundDHCP, err, "provisioningNetwork and provisioningDHCPExternal", configName)
		}
		if !provisioningDHCPExternal {
			provisioningNetwork = provisioningNetworkManaged
		} else {
			provisioningNetwork = provisioningNetworkUnmanaged
		}
	}
	// provisioningNetworkCIDR needs to be present for all provisioningNetwork states (even Disabled).
	// The CIDR of the network needs to be extracted to form the provisioningIPCIDR
	provisioningNetworkCIDR, found, err := unstructured.NestedString(provisioningSpec, "provisioningNetworkCIDR")
	if !found || err != nil {
		return nil, reportError(found, err, "provisioningNetworkCIDR", configName)
	}
	// Check if the other config values make sense for the provisioningNetwork configured.
	if provisioningInterface == "" && provisioningNetwork == provisioningNetworkManaged {
		return nil, fmt.Errorf("provisioningInterface cannot be empty when provisioningNetwork is Managed.")
	}
	if provisioningDHCPRange == "" && provisioningNetwork == provisioningNetworkManaged {
		return nil, fmt.Errorf("provisioningDHCPRange cannot be empty when provisioningNetwork is Managed or when the DHCP server needs to run with the metal3 cluster.")
	}
	baremetalConfig := BaremetalProvisioningConfig{
		ProvisioningInterface:     provisioningInterface,
		ProvisioningIp:            provisioningIP,
		ProvisioningNetworkCIDR:   provisioningNetworkCIDR,
		ProvisioningDHCPRange:     provisioningDHCPRange,
		ProvisioningOSDownloadURL: provisioningOSDownloadURL,
		ProvisioningNetwork:       provisioningNetwork,
	}
	return &baremetalConfig, nil
}

func getProvisioningIPCIDR(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningNetworkCIDR != "" && baremetalConfig.ProvisioningIp != "" {
		_, net, err := net.ParseCIDR(baremetalConfig.ProvisioningNetworkCIDR)
		if err == nil {
			cidr, _ := net.Mask.Size()
			ipCIDR := fmt.Sprintf("%s/%d", baremetalConfig.ProvisioningIp, cidr)
			return &ipCIDR
		}
	}
	return nil
}

func getDeployKernelUrl(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		deployKernelUrl := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalHttpPort), baremetalKernelUrlSubPath)
		return &deployKernelUrl
	}
	return nil
}

func getDeployRamdiskUrl(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		deployRamdiskUrl := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalHttpPort), baremetalRamdiskUrlSubPath)
		return &deployRamdiskUrl
	}
	return nil
}

func getIronicEndpoint(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		ironicEndpoint := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalIronicPort), baremetalIronicEndpointSubpath)
		return &ironicEndpoint
	}
	return nil
}

func getIronicInspectorEndpoint(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		inspectorEndpoint := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalIronicInspectorPort), baremetalIronicEndpointSubpath)
		return &inspectorEndpoint
	}
	return nil
}

func getProvisioningOSDownloadURL(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningOSDownloadURL != "" {
		return &(baremetalConfig.ProvisioningOSDownloadURL)
	}
	return nil
}

func getMetal3DeploymentConfig(name string, baremetalConfig BaremetalProvisioningConfig) *string {
	configValue := ""
	switch name {
	case "PROVISIONING_IP":
		return &baremetalConfig.ProvisioningIp
	case "PROVISIONING_CIDR":
		return getProvisioningIPCIDR(baremetalConfig)
	case "PROVISIONING_INTERFACE":
		return &baremetalConfig.ProvisioningInterface
	case "DEPLOY_KERNEL_URL":
		return getDeployKernelUrl(baremetalConfig)
	case "DEPLOY_RAMDISK_URL":
		return getDeployRamdiskUrl(baremetalConfig)
	case "IRONIC_ENDPOINT":
		return getIronicEndpoint(baremetalConfig)
	case "IRONIC_INSPECTOR_ENDPOINT":
		return getIronicInspectorEndpoint(baremetalConfig)
	case "HTTP_PORT":
		configValue = baremetalHttpPort
		return &configValue
	case "DHCP_RANGE":
		return &baremetalConfig.ProvisioningDHCPRange
	case "RHCOS_IMAGE_URL":
		return getProvisioningOSDownloadURL(baremetalConfig)
	}
	return nil
}
