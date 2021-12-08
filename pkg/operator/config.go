package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	// TODO(alberto): move to "quay.io/openshift/origin-kubemark-machine-controllers:v4.0.0" once available
	clusterAPIControllerKubemark = "docker.io/gofed/kubemark-machine-controllers:v1.0"
	clusterAPIControllerNoOp     = "no-op"
	kubemarkPlatform             = configv1.PlatformType("kubemark")
	MAPOFeature                  = "MachineAPIProviderOpenStack"
)

type Provider string

// OperatorConfig contains configuration for MAO
type OperatorConfig struct {
	TargetNamespace string `json:"targetNamespace"`
	Controllers     Controllers
	Proxy           *configv1.Proxy
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
	MachineAPIControllerOpenStack string `json:"clusterAPIControllerMAPO"`
	ClusterAPIControllerLibvirt   string `json:"clusterAPIControllerLibvirt"`
	ClusterAPIControllerBareMetal string `json:"clusterAPIControllerBareMetal"`
	ClusterAPIControllerAzure     string `json:"clusterAPIControllerAzure"`
	ClusterAPIControllerGCP       string `json:"clusterAPIControllerGCP"`
	ClusterAPIControllerOvirt     string `json:"clusterAPIControllerOvirt"`
	ClusterAPIControllerVSphere   string `json:"clusterAPIControllerVSphere"`
	ClusterAPIControllerIBMCloud  string `json:"clusterAPIControllerIBMCloud"`
	ClusterAPIControllerPowerVS   string `json:"clusterAPIControllerPowerVS"`
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
	data, err := ioutil.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}

	var i Images
	if err := json.Unmarshal(data, &i); err != nil {
		return nil, err
	}
	return &i, nil
}

func isMAPOFeatureGateEnabled(featureGate *configv1.FeatureGate) (bool, error) {
	if featureGate == nil {
		// If no featureGate is present, then the user hasn't opted in to the external cloud controllers
		return false, nil
	}
	featureSet, ok := configv1.FeatureSets[featureGate.Spec.FeatureSet]
	if !ok {
		return false, fmt.Errorf(".spec.featureSet %q not found", featureGate.Spec.FeatureSet)
	}

	enabledFeatureGates := sets.NewString(featureSet.Enabled...)
	disabledFeatureGates := sets.NewString(featureSet.Disabled...)
	// CustomNoUpgrade will override the default enabled feature gates.
	if featureGate.Spec.FeatureSet == configv1.CustomNoUpgrade && featureGate.Spec.CustomNoUpgrade != nil {
		enabledFeatureGates = sets.NewString(featureGate.Spec.CustomNoUpgrade.Enabled...)
		disabledFeatureGates = sets.NewString(featureGate.Spec.CustomNoUpgrade.Disabled...)
	}

	return !disabledFeatureGates.Has(MAPOFeature) && enabledFeatureGates.Has(MAPOFeature), nil
}

func getProviderControllerFromImages(platform configv1.PlatformType, featureGate *configv1.FeatureGate, images Images) (string, error) {
	switch platform {
	case configv1.AWSPlatformType:
		return images.ClusterAPIControllerAWS, nil
	case configv1.LibvirtPlatformType:
		return images.ClusterAPIControllerLibvirt, nil
	case configv1.OpenStackPlatformType:
		enabled, err := isMAPOFeatureGateEnabled(featureGate)
		if err != nil {
			return "", err
		}
		if enabled {
			return images.MachineAPIControllerOpenStack, nil
		} else {
			return images.ClusterAPIControllerOpenStack, nil
		}
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
	case kubemarkPlatform:
		return clusterAPIControllerKubemark, nil
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
