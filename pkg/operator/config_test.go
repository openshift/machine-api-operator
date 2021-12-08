package operator

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

var (
	imagesJSONFile                  = "fixtures/images.json"
	expectedAWSImage                = "docker.io/openshift/origin-aws-machine-controllers:v4.0.0"
	expectedLibvirtImage            = "docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0"
	expectedOpenstackImage          = "docker.io/openshift/origin-openstack-machine-controllers:v4.0.0"
	expectedMAPOImage               = "quay.io/shiftstack/machine-api-provider-openstack:latest"
	expectedMachineAPIOperatorImage = "docker.io/openshift/origin-machine-api-operator:v4.0.0"
	expectedKubeRBACProxyImage      = "docker.io/openshift/origin-kube-rbac-proxy:v4.0.0"
	expectedBareMetalImage          = "quay.io/openshift/origin-baremetal-machine-controllers:v4.0.0"
	expectedAzureImage              = "quay.io/openshift/origin-azure-machine-controllers:v4.0.0"
	expectedGCPImage                = "quay.io/openshift/origin-gcp-machine-controllers:v4.0.0"
	expectedOvirtImage              = "quay.io/openshift/origin-ovirt-machine-controllers"
	expectedVSphereImage            = "docker.io/openshift/origin-machine-api-operator:v4.0.0"
	expectedPowerVSImage            = "quay.io/openshift/origin-powervs-machine-controllers:v4.0.0"
)

func TestGetProviderFromInfrastructure(t *testing.T) {
	tests := []struct {
		infra    *configv1.Infrastructure
		expected configv1.PlatformType
	}{{
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.AWSPlatformType,
				},
			},
		},
		expected: configv1.AWSPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.LibvirtPlatformType,
				},
			},
		},
		expected: configv1.LibvirtPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.OpenStackPlatformType,
				},
			},
		},
		expected: configv1.OpenStackPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.AzurePlatformType,
				},
			},
		},
		expected: configv1.AzurePlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.GCPPlatformType,
				},
			},
		},
		expected: configv1.GCPPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.BareMetalPlatformType,
				},
			},
		},
		expected: configv1.BareMetalPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: kubemarkPlatform,
				},
			},
		},
		expected: kubemarkPlatform,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.VSpherePlatformType,
				},
			},
		},
		expected: configv1.VSpherePlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.NonePlatformType,
				},
			},
		},
		expected: configv1.NonePlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.OvirtPlatformType,
				},
			},
		},
		expected: configv1.OvirtPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.OvirtPlatformType,
			},
		},
		expected: "",
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.PowerVSPlatformType,
				},
			},
		},
		expected: configv1.PowerVSPlatformType,
	}}

	for _, test := range tests {
		res, err := getProviderFromInfrastructure(test.infra)
		// empty expected string means we were expecting it to error
		if err != nil && test.expected != "" {
			t.Errorf("failed getProviderFromInfrastructure: %v", err)
		}
		if test.expected != res {
			t.Errorf("failed getProviderFromInfrastructure. Expected: %q, got: %q", test.expected, res)
		}
	}
}

func TestGetImagesFromJSONFile(t *testing.T) {
	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile")
	}
	if img.ClusterAPIControllerAWS != expectedAWSImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedAWSImage, img.ClusterAPIControllerAWS)
	}
	if img.ClusterAPIControllerLibvirt != expectedLibvirtImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedLibvirtImage, img.ClusterAPIControllerLibvirt)
	}
	if img.ClusterAPIControllerOpenStack != expectedOpenstackImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedOpenstackImage, img.ClusterAPIControllerOpenStack)
	}
	if img.ClusterAPIControllerBareMetal != expectedBareMetalImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedBareMetalImage, img.ClusterAPIControllerBareMetal)
	}
	if img.ClusterAPIControllerAzure != expectedAzureImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedAzureImage, img.ClusterAPIControllerAzure)
	}
	if img.ClusterAPIControllerGCP != expectedGCPImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedGCPImage, img.ClusterAPIControllerGCP)
	}
	if img.ClusterAPIControllerOvirt != expectedOvirtImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedOvirtImage, img.ClusterAPIControllerOvirt)
	}
	if img.ClusterAPIControllerVSphere != expectedVSphereImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedVSphereImage, img.ClusterAPIControllerVSphere)
	}
	if img.ClusterAPIControllerPowerVS != expectedPowerVSImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedPowerVSImage, img.ClusterAPIControllerPowerVS)
	}
	if img.MachineAPIControllerOpenStack != expectedMAPOImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedMAPOImage, img.MachineAPIControllerOpenStack)
	}
}

func TestGetProviderControllerFromImages(t *testing.T) {
	tests := []struct {
		name          string
		provider      configv1.PlatformType
		featureGate   configv1.FeatureGate
		expectedImage string
	}{{
		provider:      configv1.AWSPlatformType,
		expectedImage: expectedAWSImage,
	},
		{
			provider:      configv1.LibvirtPlatformType,
			expectedImage: expectedLibvirtImage,
		},
		{
			provider:      configv1.OpenStackPlatformType,
			expectedImage: expectedOpenstackImage,
		},
		{
			provider:      configv1.BareMetalPlatformType,
			expectedImage: expectedBareMetalImage,
		},
		{
			provider:      configv1.AzurePlatformType,
			expectedImage: expectedAzureImage,
		},
		{
			provider:      configv1.GCPPlatformType,
			expectedImage: expectedGCPImage,
		},
		{
			provider:      kubemarkPlatform,
			expectedImage: clusterAPIControllerKubemark,
		},
		{
			provider:      configv1.VSpherePlatformType,
			expectedImage: expectedVSphereImage,
		},
		{
			provider:      configv1.NonePlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.OvirtPlatformType,
			expectedImage: expectedOvirtImage,
		},
		{
			provider:      configv1.PowerVSPlatformType,
			expectedImage: expectedPowerVSImage,
		},
		{
			name:     "valid MAPO Feature Gate",
			provider: configv1.OpenStackPlatformType,
			featureGate: configv1.FeatureGate{
				Spec: configv1.FeatureGateSpec{
					FeatureGateSelection: configv1.FeatureGateSelection{
						FeatureSet: configv1.CustomNoUpgrade,
						CustomNoUpgrade: &configv1.CustomFeatureGates{
							Enabled: []string{MAPOFeature},
						},
					},
				},
			},
			expectedImage: expectedMAPOImage,
		},
		{
			name:     "MAPO both enabled and disabled",
			provider: configv1.OpenStackPlatformType,
			featureGate: configv1.FeatureGate{
				Spec: configv1.FeatureGateSpec{
					FeatureGateSelection: configv1.FeatureGateSelection{
						FeatureSet: configv1.CustomNoUpgrade,
						CustomNoUpgrade: &configv1.CustomFeatureGates{
							Enabled:  []string{MAPOFeature},
							Disabled: []string{MAPOFeature},
						},
					},
				},
			},
			expectedImage: expectedOpenstackImage,
		},
		{
			name:          "Feature Gate Unset does not error",
			provider:      configv1.OpenStackPlatformType,
			featureGate:   configv1.FeatureGate{},
			expectedImage: expectedOpenstackImage,
		},
	}

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile, %v", err)
	}

	for _, test := range tests {
		res, err := getProviderControllerFromImages(test.provider, &test.featureGate, *img)
		if err != nil {
			t.Errorf("failed getProviderControllerFromImages: %v", err)
		}
		if test.expectedImage != res {
			t.Errorf("failed getProviderControllerFromImages. Expected: %q, got: %q", test.expectedImage, res)
		}
	}
}

func TestGetTerminationHandlerFromImages(t *testing.T) {
	tests := []struct {
		provider      configv1.PlatformType
		expectedImage string
	}{{
		provider:      configv1.AWSPlatformType,
		expectedImage: expectedAWSImage,
	},
		{
			provider:      configv1.LibvirtPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.OpenStackPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.BareMetalPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.AzurePlatformType,
			expectedImage: expectedAzureImage,
		},
		{
			provider:      configv1.GCPPlatformType,
			expectedImage: expectedGCPImage,
		},
		{
			provider:      kubemarkPlatform,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.VSpherePlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.NonePlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.OvirtPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.PowerVSPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
	}

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile, %v", err)
	}

	for _, test := range tests {
		res, err := getTerminationHandlerFromImages(test.provider, *img)
		if err != nil {
			t.Errorf("failed getTerminationHandlerFromImages: %v", err)
		}
		if test.expectedImage != res {
			t.Errorf("failed getTerminationHandlerFromImages. Expected: %q, got: %q", test.expectedImage, res)
		}
	}
}

func TestGetMachineAPIOperatorFromImages(t *testing.T) {
	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile, %v", err)
	}

	res, err := getMachineAPIOperatorFromImages(*img)
	if err != nil {
		t.Errorf("failed getMachineAPIOperatorFromImages : %v", err)
	}
	if res != expectedMachineAPIOperatorImage {
		t.Errorf("failed getMachineAPIOperatorFromImages. Expected: %s, got: %s", expectedMachineAPIOperatorImage, res)
	}
}

func TestGetKubeRBACProxyFromImages(t *testing.T) {
	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile, %v", err)
	}

	res, err := getKubeRBACProxyFromImages(*img)
	if err != nil {
		t.Errorf("failed getKubeRBACProxyFromImages : %v", err)
	}
	if res != expectedKubeRBACProxyImage {
		t.Errorf("failed getKubeRBACProxyFromImages. Expected: %s, got: %s", expectedKubeRBACProxyImage, res)
	}
}
