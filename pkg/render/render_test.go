package render

import (
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
)

func testRenderManifest(t *testing.T, filename string, config *OperatorConfig, expectedConfig string) {
	t.Helper()

	manifest, err := filepath.Abs(filename)
	if err != nil {
		t.Fatalf("Failed to obtain absolute path of manifest %q: %v", filename, err)
	}

	data, err := ioutil.ReadFile(manifest)
	if err != nil {
		t.Fatalf("Failed to ingest manifest %q: %v", manifest, err)
	}

	actual, err := Manifests(config, data)
	if err != nil {
		t.Fatalf("Failed to render manifest template: %v", err)
	}

	a := strings.TrimSpace(expectedConfig)
	b := strings.TrimSpace(string(actual))

	if a != b {
		t.Errorf("Expected:\n%v\nGot:\n%v", a, b)
	}
}

func TestClusterManifest(t *testing.T) {
	config := OperatorConfig{
		VpcName:       "TestClusterManifest-VpcName",
		SshKey:        "TestClusterManifest-SshKey",
		ClusterName:   "TestClusterManifest-ClusterName",
		ClusterDomain: "TestClusterManifest.ClusterDomain", // TODO(frobware) - currently not a template value
		Region:        "TestClusterManifest-Region",
		Image:         "TestClusterManifest-Image",
	}

	testRenderManifest(t, "../../machines/cluster.yaml", &config, `
apiVersion: "cluster.k8s.io/v1alpha1"
kind: Cluster
metadata:
  name: test
  namespace: test
spec:
  clusterNetwork:
    services:
      cidrBlocks:
        - "10.0.0.1/24"
    pods:
      cidrBlocks:
        - "10.0.0.2/24"
    serviceDomain: example.com
  providerConfig:
    value:
      apiVersion: awsproviderconfig/v1alpha1
      kind: AWSClusterProviderConfig
      clusterId: TestClusterManifest-VpcName
      clusterVersionRef:
        namespace: test
        name: test
      hardware:
        aws:
          region: TestClusterManifest-Region
          keyPairName: TestClusterManifest-SshKey
      defaultHardwareSpec:
        aws:
          instanceType: m4.large
      machineSets:
      - nodeType: Master
        size: 1
      - shortName: infra
        nodeType: Compute
        infra: true
        size: 1
      - shortName: compute
        nodeType: Compute
        size: 1`)
}

func TestMachineSetManifest(t *testing.T) {
	config := OperatorConfig{
		VpcName:       "TestMachineSetManifest-VpcName",
		SshKey:        "TestMachineSetManifest-SshKey",
		ClusterName:   "TestMachineSetManifest-ClusterName",
		ClusterDomain: "TestMachineSetManifest.ClusterDomain", // TODO(frobware) - currently not a template value
		Region:        "TestMachineSetManifest-Region",
		Image:         "TestMachineSetManifest-Image",
	}

	testRenderManifest(t, "../../machines/machine-set.yaml", &config, `
apiVersion: cluster.k8s.io/v1alpha1
kind: MachineSet
metadata:
  name: worker
  namespace: test
  labels:
    machineapioperator.openshift.io/cluster: test
spec:
  replicas: 3
  selector:
    matchLabels:
      machineapioperator.openshift.io/machineset: worker
      machineapioperator.openshift.io/cluster: test
  template:
    metadata:
      labels:
        machineapioperator.openshift.io/machineset: worker
        machineapioperator.openshift.io/cluster: test
    spec:
      providerConfig:
        value:
          apiVersion: awsproviderconfig/v1alpha1
          kind: AWSMachineProviderConfig
          clusterId: TestMachineSetManifest-VpcName
          clusterHardware:
            aws:
              keyPairName: TestMachineSetManifest-SshKey
              region: TestMachineSetManifest-Region
          hardware:
            aws:
              instanceType: m4.large
          infra: false
          vmImage:
            awsImage: TestMachineSetManifest-Image
      versions:
        kubelet: 0.0.0
        controlPlane: 0.0.0
      roles:
      - Master`)
}
