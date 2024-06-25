package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	configv1 "github.com/openshift/api/config/v1"
)

const (
	// TODO(alberto): move to "quay.io/openshift/origin-kubemark-machine-controllers:v4.0.0" once available
	clusterAPIControllerKubemark = "docker.io/gofed/kubemark-machine-controllers:v1.0"
	clusterAPIControllerNoOp     = "no-op"
	kubemarkPlatform             = configv1.PlatformType("kubemark")
)

type Provider string

// OperatorConfig contains configuration for MAO
type OperatorConfig struct {
	TargetNamespace string `json:"targetNamespace"`
	Controllers     Controllers
	Proxy           *configv1.Proxy
	PlatformType    configv1.PlatformType
	Features        map[string]bool
}

type Controllers struct {
	Provider           string
	MachineSet         string
	NodeLink           string
	MachineHealthCheck string
	KubeRBACProxy      string
	TerminationHandler string
}

// Images allows build systems to inject images for MAO components
type Images struct {
	MachineAPIOperator            string `json:"machineAPIOperator"`
	ClusterAPIControllerAWS       string `json:"clusterAPIControllerAWS"`
	ClusterAPIControllerOpenStack string `json:"clusterAPIControllerOpenStack"`
	ClusterAPIControllerLibvirt   string `json:"clusterAPIControllerLibvirt"`
	ClusterAPIControllerBareMetal string `json:"clusterAPIControllerBareMetal"`
	ClusterAPIControllerAzure     string `json:"clusterAPIControllerAzure"`
	ClusterAPIControllerGCP       string `json:"clusterAPIControllerGCP"`
	ClusterAPIControllerOvirt     string `json:"clusterAPIControllerOvirt"`
	ClusterAPIControllerVSphere   string `json:"clusterAPIControllerVSphere"`
	ClusterAPIControllerIBMCloud  string `json:"clusterAPIControllerIBMCloud"`
	ClusterAPIControllerPowerVS   string `json:"clusterAPIControllerPowerVS"`
	ClusterAPIControllerNutanix   string `json:"clusterAPIControllerNutanix"`
	KubeRBACProxy                 string `json:"kubeRBACProxy"`
}

func getProviderFromInfrastructure(infra *configv1.Infrastructure) (configv1.PlatformType, error) {
	if infra.Status.PlatformStatus != nil {
		if infra.Status.PlatformStatus.Type != "" {
			return infra.Status.PlatformStatus.Type, nil
		}
	}
	return "", fmt.Errorf("no platform provider found on install config")
}

func getImagesFromJSONFile(filePath string) (*Images, error) {
	data, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}

	var i Images
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func getProviderControllerFromImages(platform configv1.PlatformType, images Images) (string, error) {
	switch platform {
	case configv1.AWSPlatformType:
		return images.ClusterAPIControllerAWS, nil
	case configv1.LibvirtPlatformType:
		return images.ClusterAPIControllerLibvirt, nil
	case configv1.OpenStackPlatformType:
		return images.ClusterAPIControllerOpenStack, nil
	case configv1.AzurePlatformType:
		return images.ClusterAPIControllerAzure, nil
	case configv1.GCPPlatformType:
		return images.ClusterAPIControllerGCP, nil
	case configv1.BareMetalPlatformType:
		return images.ClusterAPIControllerBareMetal, nil
	case configv1.OvirtPlatformType:
		return images.ClusterAPIControllerOvirt, nil
	case configv1.VSpherePlatformType:
		return images.ClusterAPIControllerVSphere, nil
	case configv1.IBMCloudPlatformType:
		return images.ClusterAPIControllerIBMCloud, nil
	case configv1.PowerVSPlatformType:
		return images.ClusterAPIControllerPowerVS, nil
	case configv1.NutanixPlatformType:
		return images.ClusterAPIControllerNutanix, nil
	case kubemarkPlatform:
		return clusterAPIControllerKubemark, nil
	case configv1.NonePlatformType, configv1.ExternalPlatformType:
		return clusterAPIControllerNoOp, nil
	default:
		return clusterAPIControllerNoOp, nil
	}
}

// getTerminationHandlerFromImages returns the image to use for the Termination Handler DaemonSet
// based on the platform provided.
// Defaults to NoOp if not supported by the platform.
func getTerminationHandlerFromImages(platform configv1.PlatformType, images Images) (string, error) {
	switch platform {
	case configv1.AWSPlatformType:
		return images.ClusterAPIControllerAWS, nil
	case configv1.GCPPlatformType:
		return images.ClusterAPIControllerGCP, nil
	case configv1.AzurePlatformType:
		return images.ClusterAPIControllerAzure, nil
	default:
		return clusterAPIControllerNoOp, nil
	}
}

func getMachineAPIOperatorFromImages(images Images) (string, error) {
	if images.MachineAPIOperator == "" {
		return "", fmt.Errorf("failed gettingMachineAPIOperator image. It is empty")
	}
	return images.MachineAPIOperator, nil
}

func getKubeRBACProxyFromImages(images Images) (string, error) {
	if images.KubeRBACProxy == "" {
		return "", fmt.Errorf("failed getting kubeRBACProxy image. It is empty")
	}
	return images.KubeRBACProxy, nil
}
