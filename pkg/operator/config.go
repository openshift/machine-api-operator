package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"bytes"
	"text/template"

	configv1 "github.com/openshift/api/config/v1"
)

const (
	// TODO(alberto): move to "quay.io/openshift/origin-kubemark-machine-controllers:v4.0.0" once available
	clusterAPIControllerKubemark = "docker.io/gofed/kubemark-machine-controllers:v1.0"
	// AWSPlatformType is used to install on AWS
	AWSProvider = Provider("aws")
	// LibvirtPlatformType is used to install of libvirt
	LibvirtProvider = Provider("libvirt")
	// OpenStackPlatformType is used to install on OpenStack
	OpenStackProvider = Provider("openstack")
	// KubemarkPlatformType is used to install on Kubemark
	KubemarkProvider = Provider("kubemark")
	// BareMetalPlatformType is used for install using managed Bare Metal
	BareMetalProvider = Provider("baremetal")
	AzureProvider     = Provider("azure")
	NoneProvider      = Provider("none")
)

type Provider string

// OperatorConfig contains configuration for MAO
type OperatorConfig struct {
	TargetNamespace string `json:"targetNamespace"`
	Controllers     Controllers
}

type Controllers struct {
	Provider           string
	NodeLink           string
	MachineHealthCheck string
}

// Images allows build systems to inject images for MAO components
type Images struct {
	MachineAPIOperator            string `json:"machineAPIOperator"`
	ClusterAPIControllerAWS       string `json:"clusterAPIControllerAWS"`
	ClusterAPIControllerOpenStack string `json:"clusterAPIControllerOpenStack"`
	ClusterAPIControllerLibvirt   string `json:"clusterAPIControllerLibvirt"`
	ClusterAPIControllerBareMetal string `json:"clusterAPIControllerBareMetal"`
	ClusterAPIControllerAzure     string `json:"clusterAPIControllerAzure"`
}

// InstallConfig contains the mao relevant config coming from the install config, i.e provider
type InstallConfig struct {
	InstallPlatform `json:"platform"`
}

// InstallPlatform is the configuration for the specific platform upon which to perform
// the installation. Only one of the platform configuration should be set
type InstallPlatform struct {
	// AWS is the configuration used when running on AWS
	AWS interface{} `json:"aws,omitempty"`

	// Libvirt is the configuration used when running on libvirt
	Libvirt interface{} `json:"libvirt,omitempty"`

	// OpenStack is the configuration used when running on OpenStack
	OpenStack interface{} `json:"openstack,omitempty"`

	// Kubemark is the configuration used when running with Kubemark
	Kubemark interface{} `json:"kubemark,omitempty"`

	// BareMetal is the configuration used when running on managed Bare Metal
	BareMetal interface{} `json:"baremetal,omitempty"`

	// Azure is the configuration used when running on managed Azure
	Azure interface{} `json:"azure,omitempty"`

	// None is the configuration used when running on unmanaged UPI
	None interface{} `json:"none,omitempty"`
}

func getProviderFromInfrastructure(infra *configv1.Infrastructure) (Provider, error) {
	switch infra.Status.Platform {
	case configv1.AWSPlatform:
		return AWSProvider, nil
	case configv1.LibvirtPlatform:
		return LibvirtProvider, nil
	case configv1.OpenStackPlatform:
		return OpenStackProvider, nil
	case configv1.AzurePlatform:
		return AzureProvider, nil
	case configv1.NonePlatform:
		return NoneProvider, nil
	case configv1.PlatformType("kubemark"):
		return KubemarkProvider, nil
	case configv1.PlatformType("baremetal"):
		return BareMetalProvider, nil
	}
	return "", fmt.Errorf("no platform provider found on install config")
}

func getImagesFromJSONFile(filePath string) (*Images, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var i Images
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func getProviderControllerFromImages(provider Provider, images Images) (string, error) {
	switch provider {
	case AWSProvider:
		return images.ClusterAPIControllerAWS, nil
	case LibvirtProvider:
		return images.ClusterAPIControllerLibvirt, nil
	case OpenStackProvider:
		return images.ClusterAPIControllerOpenStack, nil
	case KubemarkProvider:
		return clusterAPIControllerKubemark, nil
	case BareMetalProvider:
		return images.ClusterAPIControllerBareMetal, nil
	case AzureProvider:
		return images.ClusterAPIControllerAzure, nil
	case NoneProvider:
		return "None", nil
	}
	return "", fmt.Errorf("not known platform provider given %s", provider)
}

func getMachineAPIOperatorFromImages(images Images) (string, error) {
	if images.MachineAPIOperator == "" {
		return "", fmt.Errorf("failed gettingMachineAPIOperator image. It is empty")
	}
	return images.MachineAPIOperator, nil
}

// PopulateTemplate receives a template file path and renders its content populated with the config
func PopulateTemplate(config *OperatorConfig, path string) ([]byte, error) {

	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed reading file, %v", err)
	}

	buf := &bytes.Buffer{}
	tmpl, err := template.New("").Option("missingkey=error").Parse(string(data))
	if err != nil {
		return nil, err
	}

	tmplData := struct {
		OperatorConfig
	}{
		OperatorConfig: *config,
	}

	if err := tmpl.Execute(buf, tmplData); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
