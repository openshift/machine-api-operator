package operator

import (
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-version-operator/lib/resourceread"
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
			provider:      kubemarkPlatform,
			expectedImage: clusterAPIControllerKubemark,
		},
		{
			provider:      configv1.VSpherePlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
		{
			provider:      configv1.NonePlatformType,
			expectedImage: clusterAPIControllerNoOp,
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

func TestPopulateTemplateMachineHealthCheckControllerEnabled(t *testing.T) {
	oc := &OperatorConfig{
		TargetNamespace: "test-namespace",
		Controllers: Controllers{
			Provider:                  "controllers-provider",
			NodeLink:                  "controllers-nodelink",
			MachineHealthCheck:        "controllers-machinehealthcheck",
			MachineHealthCheckEnabled: true,
		},
	}
	controllerBytes, err := PopulateTemplate(oc, "../../owned-manifests/machine-api-controllers.yaml")
	if err != nil {
		t.Errorf("failed to populate template: %v", err)
	}

	controller := resourceread.ReadDeploymentV1OrDie(controllerBytes)
	hcControllerFound := false
	hcControllerName := "machine-healthcheck-controller"
	for _, container := range controller.Spec.Template.Spec.Containers {
		if container.Name == hcControllerName {
			hcControllerFound = true
			break
		}
	}
	if !hcControllerFound {
		t.Errorf("failed to find %q container in %q deployment", hcControllerName, controller.Name)
	} else {
		t.Logf("found %q container in %q deployment", hcControllerName, controller.Name)
	}
}

func TestPopulateTemplateMachineHealthCheckControllerDisabled(t *testing.T) {
	oc := &OperatorConfig{
		TargetNamespace: "test-namespace",
		Controllers: Controllers{
			Provider:                  "controllers-provider",
			NodeLink:                  "controllers-nodelink",
			MachineHealthCheck:        "controllers-machinehealthcheck",
			MachineHealthCheckEnabled: false,
		},
	}
	controllerBytes, err := PopulateTemplate(oc, "../../owned-manifests/machine-api-controllers.yaml")
	if err != nil {
		t.Errorf("failed to populate template: %v", err)
	}

	controller := resourceread.ReadDeploymentV1OrDie(controllerBytes)
	hcControllerFound := false
	hcControllerName := "machine-healthcheck-controller"
	for _, container := range controller.Spec.Template.Spec.Containers {
		if container.Name == hcControllerName {
			hcControllerFound = true
			break
		}
	}
	if hcControllerFound {
		t.Errorf("did not expect to find %q container in %q deployment", hcControllerName, controller.Name)
	} else {
		t.Logf("did not found %q container in %q deployment", hcControllerName, controller.Name)
	}
}
