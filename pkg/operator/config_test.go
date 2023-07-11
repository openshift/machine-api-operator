package operator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	imagev1 "github.com/openshift/api/image/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"
)

const (
	expectedAlibabaImage   = "quay.io/openshift/origin-alibaba-machine-controllers"
	expectedAWSImage       = "quay.io/openshift/origin-aws-machine-controllers"
	expectedAzureImage     = "quay.io/openshift/origin-azure-machine-controllers"
	expectedBareMetalImage = "quay.io/openshift/origin-baremetal-machine-controllers"
	expectedGCPImage       = "quay.io/openshift/origin-gcp-machine-controllers"
	expectedLibvirtImage   = "quay.io/openshift/origin-libvirt-machine-controllers"
	expectedOpenstackImage = "quay.io/openshift/origin-openstack-machine-api-provider"
	expectedOvirtImage     = "quay.io/openshift/origin-ovirt-machine-controllers"
	expectedPowerVSImage   = "quay.io/openshift/origin-powervs-machine-controllers"
	expectedVSphereImage   = "quay.io/openshift/origin-machine-api-operator"
	expectedNutanixImage   = "quay.io/openshift/origin-nutanix-machine-controllers"

	expectedKubeRBACProxyImage      = "quay.io/openshift/origin-kube-rbac-proxy"
	expectedMachineAPIOperatorImage = "quay.io/openshift/origin-machine-api-operator"

	manifestDir             = "../../install"
	imagesConfigMapManifest = "0000_30_machine-api-operator_01_images.configmap.yaml"
	imageReferencesManifest = "image-references"
)

// createImagesJSONFromManifest returns the path of a temporary file containing
// images.json extracted from the deployment manifests
func createImagesJSONFromManifest() (path string, err error) {
	imagesJSONData, err := extractImagesJSONFromManifest()
	if err != nil {
		return "", err
	}

	f, err := os.CreateTemp("", "images.json")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer func() {
		err = f.Close()
	}()

	_, err = f.Write(imagesJSONData)
	if err != nil {
		return "", fmt.Errorf("failed to write data to file: %w", err)
	}
	return f.Name(), nil
}

// extractImagesJSONFromManifest parses the manifest file containing the images
// configmap and returns the contents of images.json from it
func extractImagesJSONFromManifest() ([]byte, error) {
	imagesConfigMapPath := filepath.Join(manifestDir, imagesConfigMapManifest)
	imagesConfigMapData, err := os.ReadFile(imagesConfigMapPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read images configmap manifest %s: %w", imagesConfigMapPath, err)
	}

	imagesConfigMap := &corev1.ConfigMap{}
	err = yaml.Unmarshal(imagesConfigMapData, imagesConfigMap)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal images configmap manifest %s: %w", imagesConfigMapPath, err)
	}

	imagesJSONData, ok := imagesConfigMap.Data["images.json"]
	if !ok {
		return nil, fmt.Errorf("failed to find images.json in images configmap manifest")
	}

	return []byte(imagesJSONData), nil
}

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
					Type: configv1.AlibabaCloudPlatformType,
				},
			},
		},
		expected: configv1.AlibabaCloudPlatformType,
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
	}, {
		infra: &configv1.Infrastructure{
			Status: configv1.InfrastructureStatus{
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.NutanixPlatformType,
				},
			},
		},
		expected: configv1.NutanixPlatformType,
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
	imagesJSONFile, err := createImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Fatal(fmt.Errorf("failed getImagesFromJSONFile, %v", err))
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
	if img.ClusterAPIControllerNutanix != expectedNutanixImage {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedNutanixImage, img.ClusterAPIControllerNutanix)
	}
}

func TestGetProviderControllerFromImages(t *testing.T) {
	tests := []struct {
		name          string
		provider      configv1.PlatformType
		featureGate   configv1.FeatureGate
		expectedImage string
	}{
		{
			provider:      configv1.AWSPlatformType,
			expectedImage: expectedAWSImage,
		},
		{
			provider:      configv1.AlibabaCloudPlatformType,
			expectedImage: expectedAlibabaImage,
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
			provider:      configv1.NutanixPlatformType,
			expectedImage: expectedNutanixImage,
		},
		{
			provider:      configv1.ExternalPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
	}

	imagesJSONFile, err := createImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Fatal(fmt.Errorf("failed getImagesFromJSONFile, %v", err))
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
	}{
		{
			provider:      configv1.AWSPlatformType,
			expectedImage: expectedAWSImage,
		},
		{
			provider:      configv1.AlibabaCloudPlatformType,
			expectedImage: expectedAlibabaImage,
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
		{
			provider:      configv1.NutanixPlatformType,
			expectedImage: clusterAPIControllerNoOp,
		},
	}

	imagesJSONFile, err := createImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Fatal(fmt.Errorf("failed getImagesFromJSONFile, %v", err))
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
	imagesJSONFile, err := createImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Fatal(fmt.Errorf("failed getImagesFromJSONFile, %v", err))
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
	imagesJSONFile, err := createImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	img, err := getImagesFromJSONFile(imagesJSONFile)
	if err != nil {
		t.Fatal(fmt.Errorf("failed getImagesFromJSONFile, %v", err))
	}

	res, err := getKubeRBACProxyFromImages(*img)
	if err != nil {
		t.Errorf("failed getKubeRBACProxyFromImages : %v", err)
	}
	if res != expectedKubeRBACProxyImage {
		t.Errorf("failed getKubeRBACProxyFromImages. Expected: %s, got: %s", expectedKubeRBACProxyImage, res)
	}
}

func TestImagesInImageReferences(t *testing.T) {
	imagesJSONData, err := extractImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}

	imageMap := make(map[string]string)
	err = json.Unmarshal([]byte(imagesJSONData), &imageMap)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to unmarshal images json file: %w", err))
	}

	imageReferencesPath := filepath.Join(manifestDir, imageReferencesManifest)
	imageReferencesData, err := os.ReadFile(imageReferencesPath)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to read image references manifest %s: %w", imageReferencesPath, err))
	}

	imageStream := imagev1.ImageStream{}
	err = yaml.Unmarshal(imageReferencesData, &imageStream)
	if err != nil {
		t.Fatal(fmt.Errorf("failed to unmarshal image references manifest %s: %w", imageReferencesPath, err))
	}

	// Get all referenced images
	referencedImages := make(map[string]struct{})
	for _, tag := range imageStream.Spec.Tags {
		referencedImages[tag.From.Name] = struct{}{}
	}

	// Check that every entry in images.json matches an entry in image-references
	for _, image := range imageMap {
		if _, ok := referencedImages[image]; !ok {
			t.Errorf("no tagged image in %s references %s from images.json", imageReferencesPath, image)
		}
	}
}
