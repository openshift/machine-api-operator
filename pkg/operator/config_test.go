package operator

import (
	"testing"

	v1 "k8s.io/api/core/v1"
)

var (
	imagesJSONFile                  = "fixtures/images.json"
	expectedAWSImage                = "docker.io/openshift/origin-aws-machine-controllers:v4.0.0"
	expectedLibvirtImage            = "docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0"
	expectedOpenstackImage          = "docker.io/openshift/origin-openstack-machine-controllers:v4.0.0"
	expectedBaremetalImage          = "quay.io/oglok/cluster-api-provider-baremetal@sha256:007b36754737d948bb88913de8cad49613f5e960176781944e04fee250f68b7a"
	expectedMachineAPIOperatorImage = "docker.io/openshift/origin-machine-api-operator:v4.0.0"
)

func TestInstallConfigFromClusterConfig(t *testing.T) {
	data := make(map[string]string)
	data[InstallConfigKey] = `
admin:
  email: test
  password: test
  sshKey: |
    test
baseDomain: a-domain.com
clusterID: a7265676-7dc3-4ff3-8759-f2d6e3934e76
machines:
- name: master
  platform: {}
  replicas: 3
- name: worker
  platform: {}
  replicas: 3
metadata:
  creationTimestamp: null
  name: test
networking:
  podCIDR: 10.2.0.0/16
  serviceCIDR: 10.3.0.0/16
  type: flannel
platform:
  aws:
    region: us-east-1
    vpcCIDRBlock: 10.0.0.0/16
    vpcID: ""
pullSecret: “"
`
	cfg := v1.ConfigMap{
		Data: data,
	}

	res, err := getInstallConfigFromClusterConfig(&cfg)
	if err != nil {
		t.Errorf("failed to get install config: %v", err)
	}
	if res.InstallPlatform.AWS != nil && res.InstallPlatform.Libvirt == nil && res.InstallPlatform.OpenStack == nil && res.InstallPlatform.Baremetal == nil {
		t.Logf("got install config successfully: %+v", res)
	} else {
		t.Errorf("failed to getInstallConfigFromClusterConfig. Expected aws to be not nil, got: %+v", res)
	}
}

func TestGetProviderFromInstallConfig(t *testing.T) {
	var notNil = "not nil"
	tests := []struct {
		ic       *InstallConfig
		expected Provider
	}{{
		ic: &InstallConfig{
			InstallPlatform{
				AWS:       notNil,
				Libvirt:   nil,
				OpenStack: nil,
				Baremetal: nil,
			},
		},
		expected: AWSProvider,
	},
		{
			ic: &InstallConfig{
				InstallPlatform{
					AWS:       nil,
					Libvirt:   notNil,
					OpenStack: nil,
					Baremetal: nil,
				},
			},
			expected: LibvirtProvider,
		},
		{
			ic: &InstallConfig{
				InstallPlatform{
					AWS:       nil,
					Libvirt:   nil,
					OpenStack: notNil,
					Baremetal: nil,
				},
			},
			expected: OpenStackProvider,
		},
		{
			ic: &InstallConfig{
				InstallPlatform{
					AWS:       nil,
					Libvirt:   nil,
					OpenStack: nil,
					Baremetal: notNil,
				},
			},
			expected: BaremetalProvider,
		}}

	for _, test := range tests {
		res, err := getProviderFromInstallConfig(test.ic)
		if err != nil {
			t.Errorf("failed getProviderFromInstallConfig: %v", err)
		}
		if test.expected != res {
			t.Errorf("failed getProviderFromInstallConfig. Expected: %q, got: %q", test.expected, res)
		}
	}

	// More than one installPlatform should error
	ic := &InstallConfig{
		InstallPlatform{
			AWS:       nil,
			Libvirt:   notNil,
			OpenStack: notNil,
			Baremetal: nil,
		},
	}
	res, err := getProviderFromInstallConfig(ic)
	if err == nil {
		t.Errorf("failed getProviderFromInstallConfig. Expected error, got: %v", res)
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
	if img.ClusterAPIControllerBaremetal != expectedBaremetal {
		t.Errorf("failed getImagesFromJSONFile. Expected: %s, got: %s", expectedBaremetalImage, img.ClusterAPIControllerBaremetal)
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
			provider:      BaremetalProvider,
			expectedImage: expectedBaremetalImage,
		}}

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
