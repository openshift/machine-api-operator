package operator

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
)

var (
	imagesJSONFile                    = "fixtures/images.json"
	expectedAWSImage                  = "docker.io/openshift/origin-aws-machine-controllers:v4.0.0"
	expectedLibvirtImage              = "docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0"
	expectedOpenstackImage            = "docker.io/openshift/origin-openstack-machine-controllers:v4.0.0"
	expectedMachineAPIOperatorImage   = "docker.io/openshift/origin-machine-api-operator:v4.0.0"
	expectedKubeRBACProxyImage        = "docker.io/openshift/origin-kube-rbac-proxy:v4.0.0"
	expectedBareMetalImage            = "quay.io/openshift/origin-baremetal-machine-controllers:v4.0.0"
	expectedAzureImage                = "quay.io/openshift/origin-azure-machine-controllers:v4.0.0"
	expectedGCPImage                  = "quay.io/openshift/origin-gcp-machine-controllers:v4.0.0"
	expectedBaremetalOperator         = "quay.io/openshift/origin-baremetal-operator:v4.2.0"
	expectedIronic                    = "quay.io/openshift/origin-ironic:v4.2.0"
	expectedIronicInspector           = "quay.io/openshift/origin-ironic-inspector:v4.2.0"
	expectedIronicIpaDownloader       = "quay.io/openshift/origin-ironic-ipa-downloader:v4.2.0"
	expectedIronicMachineOsDownloader = "quay.io/openshift/origin-ironic-machine-os-downloader:v4.3.0"
	expectedIronicStaticIpManager     = "quay.io/openshift/origin-ironic-static-ip-manager:v4.2.0"
	expectedOvirtImage                = "quay.io/openshift/origin-ovirt-machine-controllers"
	expectedVSphereImage              = "docker.io/openshift/origin-machine-api-operator:v4.0.0"
)

func TestGetProviderFromInfrastructure(t *testing.T) {
	tests := []struct {
		infra    *configv1.Infrastructure
		expected configv1.PlatformType
	}{{
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.AWSPlatformType,
			},
		},
		expected: configv1.AWSPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.LibvirtPlatformType,
			},
		},
		expected: configv1.LibvirtPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.OpenStackPlatformType,
			},
		},
		expected: configv1.OpenStackPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.AzurePlatformType,
			},
		},
		expected: configv1.AzurePlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.GCPPlatformType,
			},
		},
		expected: configv1.GCPPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.BareMetalPlatformType,
			},
		},
		expected: configv1.BareMetalPlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: kubemarkPlatform,
			},
		},
		expected: kubemarkPlatform,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.VSpherePlatformType,
			},
		},
		expected: configv1.VSpherePlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.NonePlatformType,
			},
		},
		expected: configv1.NonePlatformType,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.OvirtPlatformType,
			},
		},
		expected: configv1.OvirtPlatformType,
	}}

	for _, test := range tests {
		res, err := getProviderFromInfrastructure(test.infra)
		if err != nil {
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
}

func TestGetProviderControllerFromImages(t *testing.T) {
	tests := []struct {
		provider      configv1.PlatformType
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
	}

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile, %v", err)
	}

	for _, test := range tests {
		res, err := getProviderControllerFromImages(test.provider, *img)
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
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.GCPPlatformType,
			expectedImage: clusterAPIControllerNoOp,
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

func TestGetBaremetalControllers(t *testing.T) {
	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Errorf("failed getImagesFromJSONFile, %v", err)
	}

	baremetalControllers := newBaremetalControllers(*img, true)

	if baremetalControllers.BaremetalOperator != expectedBaremetalOperator ||
		baremetalControllers.Ironic != expectedIronic ||
		baremetalControllers.IronicInspector != expectedIronicInspector ||
		baremetalControllers.IronicIpaDownloader != expectedIronicIpaDownloader ||
		baremetalControllers.IronicMachineOsDownloader != expectedIronicMachineOsDownloader ||
		baremetalControllers.IronicStaticIpManager != expectedIronicStaticIpManager {
		t.Errorf("failed getAdditionalProviderImages. One or more BaremetalController images do not match the expected images.")
	}
}
