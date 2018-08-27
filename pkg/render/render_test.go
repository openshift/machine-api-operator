package render

import (
	"io/ioutil"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

var testConfig = OperatorConfig{
	AWSCredentialsSecret: "TestClusterManifest-AWSCredentialsSecret",

	ClusterConfig: ClusterConfig{
		KeyPairName: "TestClusterManifest-KeyPairName",
		Region:      "TestClusterManifest-Region",
		VPCName:     "TestClusterManifest-VPCName",
	},

	MachineConfig: MachineConfig{
		AMI:                "TestClusterManifest-AMI",
		AvailabilityZone:   "TestClusterManifest-AvailabilityZone",
		Subnet:             "TestClusterManifest-Subnet",
		IAMInstanceProfile: "TestClusterManifest-IAMInstanceProfile",
		SecurityGroups: []string{
			"TestClusterManifest-SecurityGroup-0",
			"TestClusterManifest-SecurityGroup-1",
		},
	},
}

const expectedClusterYAML = `
---
apiVersion: cluster.k8s.io/v1alpha1
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
      region: "TestClusterManifest-Region"
      keyPairName: "TestClusterManifest-KeyPairName"
      vpcName: "TestClusterManifest-VPCName"
`

const expectedMachineSetYAML = `
---
apiVersion: cluster.k8s.io/v1alpha1
kind: MachineSet
metadata:
  name: worker
  namespace: test
  labels:
    sigs.k8s.io/cluster-api-cluster: test
    sigs.k8s.io/cluster-api-machine-role: worker
    sigs.k8s.io/cluster-api-machine-type: worker
spec:
  replicas: 3
  selector:
    matchLabels:
      sigs.k8s.io/cluster-api-machineset: worker
      sigs.k8s.io/cluster-api-cluster: test
  template:
    metadata:
      labels:
        sigs.k8s.io/cluster-api-machineset: worker
        sigs.k8s.io/cluster-api-cluster: test
        sigs.k8s.io/cluster-api-machine-role: worker
        sigs.k8s.io/cluster-api-machine-type: worker
    spec:
      providerConfig:
        value:
          apiVersion: awsproviderconfig/v1alpha1
          kind: AWSMachineProviderConfig
          ami:
            id: "TestClusterManifest-AMI"
          credentialsSecret:
            name: "TestClusterManifest-AWSCredentialsSecret"
          instanceType: m4.xlarge
          placement:
            region: "TestClusterManifest-Region"
            availabilityZone: "TestClusterManifest-AvailabilityZone"
          subnet:
            id: "TestClusterManifest-Subnet"
          iamInstanceProfile:
            id: "TestClusterManifest-IAMInstanceProfile"
          keyName: "TestClusterManifest-KeyPairName"
          tags:
            - name: openshift-node-group-config
              value: node-config-worker
            - name: host-type
              value: worker
            - name: sub-host-type
              value: default
          securityGroups:
            - id: "TestClusterManifest-SecurityGroup-0"
            - id: "TestClusterManifest-SecurityGroup-1"
          publicIP: true
`

var renderTests = []struct {
	in  string
	out string
}{
	{"../../machines/cluster.yaml", expectedClusterYAML},
	{"../../machines/machine-set.yaml", expectedMachineSetYAML},
}

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

func TestRendering(t *testing.T) {
	for _, tt := range renderTests {
		t.Run(path.Base(tt.in), func(t *testing.T) {
			testRenderManifest(t, tt.in, &testConfig, tt.out)
		})
	}
}
