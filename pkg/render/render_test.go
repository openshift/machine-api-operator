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
		ClusterName:      "TestClusterManifest-ClusterName",
		ClusterID:        "TestClusterManifest-ClusterID",
		Region:           "TestClusterManifest-Region",
		AvailabilityZone: "TestClusterManifest-AvailabilityZone",
		Image:            "TestClusterManifest-Image",
		Replicas:         "TestClusterManifest-Replicas",
	}

	testRenderManifest(t, "../../machines/cluster.yaml", &config, `
---
apiVersion: "cluster.k8s.io/v1alpha1"
kind: Cluster
metadata:
  name: TestClusterManifest-ClusterName
  namespace: default
spec:
  clusterNetwork:
    services:
      cidrBlocks:
      - "10.0.0.1/24"
    pods:
      cidrBlocks:
      - "10.0.0.2/24"
    serviceDomain: unused
`)
}

func TestMachineSetManifest(t *testing.T) {
	config := OperatorConfig{
		ClusterName:      "TestClusterManifest-ClusterName",
		ClusterID:        "TestClusterManifest-ClusterID",
		Region:           "TestClusterManifest-Region",
		AvailabilityZone: "TestClusterManifest-AvailabilityZone",
		Image:            "TestClusterManifest-Image",
		Replicas:         "TestClusterManifest-Replicas",
	}

	testRenderManifest(t, "../../machines/machine-set.yaml", &config, `
---
apiVersion: cluster.k8s.io/v1alpha1
kind: MachineSet
metadata:
  name: worker
  namespace: default
  labels:
    sigs.k8s.io/cluster-api-cluster: TestClusterManifest-ClusterName
    sigs.k8s.io/cluster-api-machine-role: worker
    sigs.k8s.io/cluster-api-machine-type: worker
spec:
  replicas: TestClusterManifest-Replicas
  selector:
    matchLabels:
      sigs.k8s.io/cluster-api-machineset: worker
      sigs.k8s.io/cluster-api-cluster: TestClusterManifest-ClusterName
  template:
    metadata:
      labels:
        sigs.k8s.io/cluster-api-machineset: worker
        sigs.k8s.io/cluster-api-cluster: TestClusterManifest-ClusterName
        sigs.k8s.io/cluster-api-machine-role: worker
        sigs.k8s.io/cluster-api-machine-type: worker
    spec:
      providerConfig:
        value:
          apiVersion: aws.cluster.k8s.io/v1alpha1
          kind: AWSMachineProviderConfig
          ami:
            id: TestClusterManifest-Image
          instanceType: m4.large
          placement:
            region: TestClusterManifest-Region
            availabilityZone: TestClusterManifest-AvailabilityZone
          subnet:
            filters:
            - name: "tag:Name"
              values:
              - TestClusterManifest-ClusterName-worker-TestClusterManifest-AvailabilityZone
          publicIp: true
          iamInstanceProfile:
            id: TestClusterManifest-ClusterName-master-profile
          keyName: tectonic
          tags:
            - name: tectonicClusterID
              value: TestClusterManifest-ClusterID
          securityGroups:
            - filters:
              - name: "tag:Name"
                values:
                - TestClusterManifest-ClusterName_worker_sg
          userDataSecret:
            name: ignition-worker
      versions:
        kubelet: ""
        controlPlane: ""`)
}
