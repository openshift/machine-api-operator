package operator

import (
	"strings"
)

const (
	baremetalHttpPort = "6180"
	baremetalIronicPort = "6385"
	baremetalIronicInspectorPort = "5050"
	baremetalKernelUrlSubPath = "/images/ironic-python-agent.kernel"
	baremetalRamdiskUrlSubPath = "/images/ironic-python-agent.initramfs"
	baremetalIronicEndpointSubpath = "/v1/"
)

func getHttpPort() string {
	return baremetalHttpPort
}

func getProvisioningIPCIDR(baremetalConfig BaremetalConfig) string {
	cidr := strings.Split(baremetalConfig.ProvisioningNetworkCIDR, "/")
	return baremetalConfig.ProvisioningIp + "/" + cidr[1]
}

func getDeployKernelUrl(baremetalConfig BaremetalConfig) string {
	return baremetalConfig.ProvisioningIp + ":" + baremetalHttpPort + baremetalKernelUrlSubPath
}

func getDeployRamdiskUrl(baremetalConfig BaremetalConfig) string {
	return baremetalConfig.ProvisioningIp + ":" + baremetalHttpPort + baremetalRamdiskUrlSubPath
}

func getIronicEndpoint(baremetalConfig BaremetalConfig) string {
	return baremetalConfig.ProvisioningIp + ":" + baremetalIronicPort + baremetalIronicEndpointSubpath
}

func getIronicInspectorEndpoint(baremetalConfig BaremetalConfig) string {
	return baremetalConfig.ProvisioningIp + ":" + baremetalIronicInspectorPort + baremetalIronicEndpointSubpath
}
