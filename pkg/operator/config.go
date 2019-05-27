package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

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

func getProviderFromInfrastructure(infra *configv1.Infrastructure) (configv1.PlatformType, error) {
	if infra.Status.Platform == "" {
		return "", fmt.Errorf("no platform provider found on install config")
	}
	return infra.Status.Platform, nil
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
	case configv1.BareMetalPlatformType:
		return images.ClusterAPIControllerBareMetal, nil
	case kubemarkPlatform:
		return clusterAPIControllerKubemark, nil
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
