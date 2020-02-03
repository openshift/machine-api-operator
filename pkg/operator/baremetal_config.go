package operator

import (
	"fmt"
	"net"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamic "k8s.io/client-go/dynamic"
)

var provisioningGVR = schema.GroupVersionResource{Group: "metal3.io", Resource: "provisionings", Version: "v1alpha1"}

const (
	baremetalProvisioningCR        = "provisioning-configuration"
	baremetalHttpPort              = "6180"
	baremetalIronicPort            = "6385"
	baremetalIronicInspectorPort   = "5050"
	baremetalKernelUrlSubPath      = "images/ironic-python-agent.kernel"
	baremetalRamdiskUrlSubPath     = "images/ironic-python-agent.initramfs"
	baremetalIronicEndpointSubpath = "v1/"
)

// Provisioning Config needed to deploy Metal3 pod
type BaremetalProvisioningConfig struct {
	ProvisioningInterface     string
	ProvisioningIp            string
	ProvisioningNetworkCIDR   string
	ProvisioningDHCPExternal  bool
	ProvisioningDHCPRange     string
	ProvisioningOSDownloadURL string
}

func getBaremetalProvisioningConfig(dc dynamic.Interface, configName string) (BaremetalProvisioningConfig, error) {
	provisioningClient := dc.Resource(provisioningGVR)
	provisioningConfig, err := provisioningClient.Get(configName, metav1.GetOptions{})
	if err != nil {
		glog.Errorf("Error getting config from Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningSpec, found, err := unstructured.NestedMap(provisioningConfig.UnstructuredContent(), "spec")
	if !found || err != nil {
		glog.Errorf("Nested Spec not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningInterface, found, err := unstructured.NestedString(provisioningSpec, "provisioningInterface")
	if !found || err != nil {
		glog.Errorf("provisioningInterface not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningIP, found, err := unstructured.NestedString(provisioningSpec, "provisioningIP")
	if !found || err != nil {
		glog.Errorf("provisioningIP not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningNetworkCIDR, found, err := unstructured.NestedString(provisioningSpec, "provisioningNetworkCIDR")
	if !found || err != nil {
		glog.Errorf("provisioningNetworkCIDR not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningDHCPExternal, found, err := unstructured.NestedBool(provisioningSpec, "provisioningDHCPExternal")
	if !found || err != nil {
		glog.Errorf("provisioningDHCPExternal not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningDHCPRange, found, err := unstructured.NestedString(provisioningSpec, "provisioningDHCPRange")
	if !found || err != nil {
		glog.Errorf("provisioningDHCPRange not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}
	provisioningOSDownloadURL, found, err := unstructured.NestedString(provisioningSpec, "provisioningOSDownloadURL")
	if !found || err != nil {
		glog.Errorf("provisioningOSDownloadURL not found in Baremetal provisioning CR %s", configName)
		return BaremetalProvisioningConfig{}, err
	}

	return BaremetalProvisioningConfig{
		ProvisioningInterface:     provisioningInterface,
		ProvisioningIp:            provisioningIP,
		ProvisioningNetworkCIDR:   provisioningNetworkCIDR,
		ProvisioningDHCPExternal:  provisioningDHCPExternal,
		ProvisioningDHCPRange:     provisioningDHCPRange,
		ProvisioningOSDownloadURL: provisioningOSDownloadURL,
	}, nil
}

func getProvisioningIPCIDR(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningNetworkCIDR != "" && baremetalConfig.ProvisioningIp != "" {
		_, net, err := net.ParseCIDR(baremetalConfig.ProvisioningNetworkCIDR)
		if err == nil {
			cidr, _ := net.Mask.Size()
			generatedConfig := fmt.Sprintf("%s/%d", baremetalConfig.ProvisioningIp, cidr)
			return &generatedConfig
		}
	}
	return nil
}

func getDeployKernelUrl(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		generatedConfig := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalHttpPort), baremetalKernelUrlSubPath)
		return &generatedConfig
	}
	return nil
}

func getDeployRamdiskUrl(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		generatedConfig := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalHttpPort), baremetalRamdiskUrlSubPath)
		return &generatedConfig
	}
	return nil
}

func getIronicEndpoint(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		generatedConfig := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalIronicPort), baremetalIronicEndpointSubpath)
		return &generatedConfig
	}
	return nil
}

func getIronicInspectorEndpoint(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningIp != "" {
		generatedConfig := fmt.Sprintf("http://%s/%s", net.JoinHostPort(baremetalConfig.ProvisioningIp, baremetalIronicInspectorPort), baremetalIronicEndpointSubpath)
		return &generatedConfig
	}
	return nil
}

func getProvisioningDHCPRange(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningDHCPRange != "" {
		return &(baremetalConfig.ProvisioningDHCPRange)
	}
	return nil
}

func getProvisioningInterface(baremetalConfig BaremetalProvisioningConfig) *string {
	if baremetalConfig.ProvisioningInterface != "" {
		return &(baremetalConfig.ProvisioningInterface)
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
		return getProvisioningIPCIDR(baremetalConfig)
	case "PROVISIONING_INTERFACE":
		return getProvisioningInterface(baremetalConfig)
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
		return getProvisioningDHCPRange(baremetalConfig)
	case "RHCOS_IMAGE_URL":
		return getProvisioningOSDownloadURL(baremetalConfig)
	}
	return nil
}
