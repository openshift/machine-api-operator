package operator

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

var yamlContent = `
apiVersion: metal3.io/v1alpha1
kind: Provisioning
metadata:
  name: test
spec:
  provisioningInterface: "ensp0"
  provisioningIP: "172.30.20.3"
  provisioningNetworkCIDR: "172.30.20.0/24"
  provisioningDHCPExternal: false
  provisioningDHCPRange: "172.30.20.11, 172.30.20.101"
  provisioningOSDownloadURL: "http://172.22.0.1/images/rhcos-44.81.202001171431.0-openstack.x86_64.qcow2.gz?sha256=e98f83a2b9d4043719664a2be75fe8134dc6ca1fdbde807996622f8cc7ecd234"
`
var (
	expectedProvisioningInterface    = "ensp0"
	expectedProvisioningIP           = "172.30.20.3"
	expectedProvisioningNetworkCIDR  = "172.30.20.0/24"
	expectedProvisioningDHCPExternal = false
	expectedProvisioningDHCPRange    = "172.30.20.11, 172.30.20.101"
	expectedOSImageURL               = "http://172.22.0.1/images/rhcos-44.81.202001171431.0-openstack.x86_64.qcow2.gz?sha256=e98f83a2b9d4043719664a2be75fe8134dc6ca1fdbde807996622f8cc7ecd234"
	expectedProvisioningIPCIDR       = "172.30.20.3/24"
	expectedDeployKernelURL          = "http://172.30.20.3:6180/images/ironic-python-agent.kernel"
	expectedDeployRamdiskURL         = "http://172.30.20.3:6180/images/ironic-python-agent.initramfs"
	expectedIronicEndpoint           = "http://172.30.20.3:6385/v1/"
	expectedIronicInspectorEndpoint  = "http://172.30.20.3:5050/v1/"
	expectedHttpPort                 = "6180"
)

func TestGenerateRandomPassword(t *testing.T) {
	pwd := generateRandomPassword()
	if pwd == "" {
		t.Errorf("Expected a valid string but got null")
	}
}

func newOperatorWithBaremetalConfig() *OperatorConfig {
	return &OperatorConfig{
		targetNamespace,
		Controllers{
			"docker.io/openshift/origin-aws-machine-controllers:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
		},
		BaremetalControllers{
			"quay.io/openshift/origin-baremetal-operator:v4.2.0",
			"quay.io/openshift/origin-ironic:v4.2.0",
			"quay.io/openshift/origin-ironic-inspector:v4.2.0",
			"quay.io/openshift/origin-ironic-ipa-downloader:v4.2.0",
			"quay.io/openshift/origin-ironic-machine-os-downloader:v4.2.0",
			"quay.io/openshift/origin-ironic-static-ip-manager:v4.2.0",
		},
	}
}

//Testing the case where the password does already exist
func TestCreateMariadbPasswordSecret(t *testing.T) {
	kubeClient := fakekube.NewSimpleClientset(nil...)
	operatorConfig := newOperatorWithBaremetalConfig()
	client := kubeClient.CoreV1()

	// First create a mariadb password secret
	if err := createMariadbPasswordSecret(kubeClient.CoreV1(), operatorConfig); err != nil {
		t.Fatalf("Failed to create first Mariadb password. %s ", err)
	}
	// Read and get Mariadb password from Secret just created.
	oldMaridbPassword, err := client.Secrets(operatorConfig.TargetNamespace).Get(baremetalSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failure getting the first Mariadb password that just got created.")
	}
	oldPassword, ok := oldMaridbPassword.StringData[baremetalSecretKey]
	if !ok || oldPassword == "" {
		t.Fatal("Failure reading first Mariadb password from Secret.")
	}

	// The pasword definitely exists. Try creating again.
	if err := createMariadbPasswordSecret(kubeClient.CoreV1(), operatorConfig); err != nil {
		t.Fatal("Failure creating second Mariadb password.")
	}
	newMaridbPassword, err := client.Secrets(operatorConfig.TargetNamespace).Get(baremetalSecretName, metav1.GetOptions{})
	if err != nil {
		t.Fatal("Failure getting the second Mariadb password.")
	}
	newPassword, ok := newMaridbPassword.StringData[baremetalSecretKey]
	if !ok || newPassword == "" {
		t.Fatal("Failure reading second Mariadb password from Secret.")
	}
	if oldPassword != newPassword {
		t.Fatalf("Both passwords do not match.")
	} else {
		t.Logf("First Mariadb password is being preserved over re-creation as expected.")
	}
}

func TestGetBaremetalProvisioningConfig(t *testing.T) {
	testConfigResource := "test"
	u := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := yaml.Unmarshal([]byte(yamlContent), &u); err != nil {
		t.Errorf("failed to unmarshall input yaml content:%v", err)
	}
	dynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), u)
	baremetalConfig, err := getBaremetalProvisioningConfig(dynamicClient, testConfigResource)
	if err != nil {
		t.Logf("Unstructed Config:  %+v", u)
		t.Fatalf("Failed to get Baremetal Provisioning Interface from CR %s", testConfigResource)
	}
	if baremetalConfig.ProvisioningInterface != expectedProvisioningInterface ||
		baremetalConfig.ProvisioningIp != expectedProvisioningIP ||
		baremetalConfig.ProvisioningNetworkCIDR != expectedProvisioningNetworkCIDR ||
		baremetalConfig.ProvisioningDHCPExternal != expectedProvisioningDHCPExternal ||
		baremetalConfig.ProvisioningDHCPRange != expectedProvisioningDHCPRange {
		t.Logf("Expected: ProvisioningInterface: %s, ProvisioningIP: %s, ProvisioningNetworkCIDR: %s, ProvisioningDHCPExternal: %t, expectedProvisioningDHCPRange: %s, Got: %+v", expectedProvisioningInterface, expectedProvisioningIP, expectedProvisioningNetworkCIDR, expectedProvisioningDHCPExternal, expectedProvisioningDHCPRange, baremetalConfig)
		t.Fatalf("failed getBaremetalProvisioningConfig. One or more BaremetalProvisioningConfig items do not match the expected config.")
	}
}

func TestGetIncorrectBaremetalProvisioningCR(t *testing.T) {
	incorrectConfigResource := "test1"
	u := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := yaml.Unmarshal([]byte(yamlContent), &u); err != nil {
		t.Errorf("failed to unmarshall input yaml content:%v", err)
	}
	dynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), u)
	baremetalConfig, err := getBaremetalProvisioningConfig(dynamicClient, incorrectConfigResource)
	if err != nil {
		t.Logf("Unable to get Baremetal Provisioning Config from CR %s as expected", incorrectConfigResource)
	}
	if baremetalConfig.ProvisioningInterface != "" {
		t.Errorf("BaremetalProvisioningConfig is not expected to be set.")
	}
}

func TestGetMetal3DeploymentConfig(t *testing.T) {
	testConfigResource := "test"
	u := &unstructured.Unstructured{Object: map[string]interface{}{}}
	if err := yaml.Unmarshal([]byte(yamlContent), &u); err != nil {
		t.Errorf("failed to unmarshall input yaml content:%v", err)
	}
	dynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), u)
	baremetalConfig, err := getBaremetalProvisioningConfig(dynamicClient, testConfigResource)
	if err != nil {
		t.Logf("Unstructed Config:  %+v", u)
		t.Errorf("Failed to get Baremetal Provisioning Config from CR %s", testConfigResource)
	}
	actualCacheURL := getMetal3DeploymentConfig("CACHEURL", baremetalConfig)
	if actualCacheURL != nil {
		t.Errorf("CacheURL is found to be %s. CACHEURL is not expected.", *actualCacheURL)
	} else {
		t.Logf("CacheURL is not available as expected.")
	}
	actualOSImageURL := getMetal3DeploymentConfig("RHCOS_IMAGE_URL", baremetalConfig)
	if actualOSImageURL != nil {
		t.Logf("Actual OS Image Download URL is %s, Expected is %s", *actualOSImageURL, expectedOSImageURL)
		if *actualOSImageURL != expectedOSImageURL {
			t.Errorf("Actual %s and Expected %s OS Image Download URLs do not match", *actualOSImageURL, expectedOSImageURL)
		}
	} else {
		t.Errorf("OS Image Download URL is not available.")
	}
	actualProvisioningIPCIDR := getMetal3DeploymentConfig("PROVISIONING_IP", baremetalConfig)
	if actualProvisioningIPCIDR != nil {
		t.Logf("Actual ProvisioningIP with CIDR is %s, Expected is %s", *actualProvisioningIPCIDR, expectedProvisioningIPCIDR)
		if *actualProvisioningIPCIDR != expectedProvisioningIPCIDR {
			t.Errorf("Actual %s and Expected %s Provisioning IPs with CIDR do not match", *actualProvisioningIPCIDR, expectedProvisioningIPCIDR)
		}
	} else {
		t.Errorf("Provisioning IP with CIDR is not available.")
	}
	actualProvisioningInterface := getMetal3DeploymentConfig("PROVISIONING_INTERFACE", baremetalConfig)
	if actualProvisioningInterface != nil {
		t.Logf("Actual Provisioning Interface is %s, Expected is %s", *actualProvisioningInterface, expectedProvisioningInterface)
		if *actualProvisioningInterface != expectedProvisioningInterface {
			t.Errorf("Actual %s and Expected %s Provisioning Interfaces do not match", *actualProvisioningIPCIDR, expectedProvisioningIPCIDR)
		}
	} else {
		t.Errorf("Provisioning Interface is not available.")
	}
	actualDeployKernelURL := getMetal3DeploymentConfig("DEPLOY_KERNEL_URL", baremetalConfig)
	if actualDeployKernelURL != nil {
		t.Logf("Actual Deploy Kernel URL is %s, Expected is %s", *actualDeployKernelURL, expectedDeployKernelURL)
		if *actualDeployKernelURL != expectedDeployKernelURL {
			t.Errorf("Actual %s and Expected %s Deploy Kernel URLs do not match", *actualDeployKernelURL, expectedDeployKernelURL)
		}
	} else {
		t.Errorf("Deploy Kernel URL is not available.")
	}
	actualDeployRamdiskURL := getMetal3DeploymentConfig("DEPLOY_RAMDISK_URL", baremetalConfig)
	if actualDeployRamdiskURL != nil {
		t.Logf("Actual Deploy Ramdisk URL is %s, Expected is %s", *actualDeployRamdiskURL, expectedDeployRamdiskURL)
		if *actualDeployRamdiskURL != expectedDeployRamdiskURL {
			t.Errorf("Actual %s and Expected %s Deploy Ramdisk URLs do not match", *actualDeployRamdiskURL, expectedDeployRamdiskURL)
		}
	} else {
		t.Errorf("Deploy Ramdisk URL is not available.")
	}
	actualIronicEndpoint := getMetal3DeploymentConfig("IRONIC_ENDPOINT", baremetalConfig)
	if actualIronicEndpoint != nil {
		t.Logf("Actual Ironic Endpoint is %s, Expected is %s", *actualIronicEndpoint, expectedIronicEndpoint)
		if *actualIronicEndpoint != expectedIronicEndpoint {
			t.Errorf("Actual %s and Expected %s Ironic Endpoints do not match", *actualIronicEndpoint, expectedIronicEndpoint)
		}
	} else {
		t.Errorf("Ironic Endpoint is not available.")
	}
	actualIronicInspectorEndpoint := getMetal3DeploymentConfig("IRONIC_INSPECTOR_ENDPOINT", baremetalConfig)
	if actualIronicInspectorEndpoint != nil {
		t.Logf("Actual Ironic Inspector Endpoint is %s, Expected is %s", *actualIronicInspectorEndpoint, expectedIronicInspectorEndpoint)
		if *actualIronicInspectorEndpoint != expectedIronicInspectorEndpoint {
			t.Errorf("Actual %s and Expected %s Ironic Inspector Endpoints do not match", *actualIronicInspectorEndpoint, expectedIronicInspectorEndpoint)
		}
	} else {
		t.Errorf("Ironic Inspector Endpoint is not available.")
	}
	actualHttpPort := getMetal3DeploymentConfig("HTTP_PORT", baremetalConfig)
	t.Logf("Actual Http Port is %s, Expected is %s", *actualHttpPort, expectedHttpPort)
	if *actualHttpPort != expectedHttpPort {
		t.Errorf("Actual %s and Expected %s Http Ports do not match", *actualHttpPort, expectedHttpPort)
	}
	actualDHCPRange := getMetal3DeploymentConfig("DHCP_RANGE", baremetalConfig)
	if actualDHCPRange != nil {
		t.Logf("Actual DHCP Range is %s, Expected is %s", *actualDHCPRange, expectedProvisioningDHCPRange)
		if *actualDHCPRange != expectedProvisioningDHCPRange {
			t.Errorf("Actual %s and Expected %s DHCP Range do not match", *actualDHCPRange, expectedProvisioningDHCPRange)
		}
	} else {
		t.Errorf("Provisioning DHCP Range is not available.")
	}
}
