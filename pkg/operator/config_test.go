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
	expectedMachineAPIOperatorImage = "docker.io/openshift/origin-machine-api-operator:v4.0.0"
	expectedBareMetalImage          = "quay.io/openshift/origin-baremetal-machine-controllers:v4.0.0"
	expectedAzureImage              = "quay.io/openshift/origin-azure-machine-controllers:v4.0.0"
)

func TestGetProviderFromInfrastructure(t *testing.T) {
	tests := []struct {
		infra    *configv1.Infrastructure
		expected Provider
	}{{
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.AWSPlatform,
			},
		},
		expected: AWSProvider,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.LibvirtPlatform,
			},
		},
		expected: LibvirtProvider,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.NonePlatform,
			},
		},
		expected: NoneProvider,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.OpenStackPlatform,
			},
		},
		expected: OpenStackProvider,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.PlatformType("baremetal"),
			},
		},
		expected: BareMetalProvider,
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				Platform: configv1.AzurePlatform,
			},
		},
		expected: AzureProvider,
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
}

func TestGetProviderControllerFromImages(t *testing.T) {
	tests := []struct {
		provider      Provider
		expectedImage string
	}{{
		provider:      AWSProvider,
		expectedImage: expectedAWSImage,
	},
		{
			provider:      LibvirtProvider,
			expectedImage: expectedLibvirtImage,
		},
		{
			provider:      OpenStackProvider,
			expectedImage: expectedOpenstackImage,
		},
		{
			provider:      BareMetalProvider,
			expectedImage: expectedBareMetalImage,
		},
		{
			provider:      AzureProvider,
			expectedImage: expectedAzureImage,
		},
		{
			provider:      NoneProvider,
			expectedImage: "None",
		},
	}

	imagesJSONFile := "fixtures/images.json"
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

func TestGetMachineAPIOperatorFromImages(t *testing.T) {
	imagesJSONFile := "fixtures/images.json"
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
