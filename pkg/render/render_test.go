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

func TestClusterAWSManifest(t *testing.T) {
	config := OperatorConfig{
		Provider: "AWS",
		AWS: &awsConfig{
			ClusterName:      "TestClusterManifest-ClusterName",
			ClusterID:        "TestClusterManifest-ClusterID",
			Region:           "TestClusterManifest-Region",
			AvailabilityZone: "TestClusterManifest-AvailabilityZone",
			Image:            "TestClusterManifest-Image",
			Replicas:         "TestClusterManifest-Replicas",
		},
	}

	testRenderManifest(t, "../../machines/aws/cluster.yaml", &config, `
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

func TestMachineSetAWSManifest(t *testing.T) {
	config := OperatorConfig{
		Provider: "aws",
		AWS: &awsConfig{
			ClusterName:      "TestClusterManifest-ClusterName",
			ClusterID:        "TestClusterManifest-ClusterID",
			Region:           "TestClusterManifest-Region",
			AvailabilityZone: "TestClusterManifest-AvailabilityZone",
			Image:            "TestClusterManifest-Image",
			Replicas:         "TestClusterManifest-Replicas",
		},
	}

	testRenderManifest(t, "../../machines/aws/machine-set.yaml", &config, `
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

func TestMachineSetLibvirtManifest(t *testing.T) {
	config := OperatorConfig{
		Provider: "libvirt",
		Libvirt: &libvirtConfig{
			URI:         "qemu+tcp://host_private_ip/system",
			Replicas:    "2",
			ClusterName: "test",
		},
	}

	testRenderManifest(t, "../../machines/libvirt/machine-set.yaml", &config, `
---
apiVersion: cluster.k8s.io/v1alpha1
kind: MachineSet
metadata:
  name: worker
  namespace: default
  labels:
    sigs.k8s.io/cluster-api-cluster: test
    sigs.k8s.io/cluster-api-machine-role: worker
    sigs.k8s.io/cluster-api-machine-type: worker
spec:
  replicas: 2
  selector:
    matchLabels:
      sigs.k8s.io/cluster-api-machineset: worker
      sigs.k8s.io/cluster-api-cluster: test
      sigs.k8s.io/cluster-api-machine-role: worker
      sigs.k8s.io/cluster-api-machine-type: worker
  template:
    metadata:
      labels:
        sigs.k8s.io/cluster-api-machineset: worker
        sigs.k8s.io/cluster-api-cluster: test
        sigs.k8s.io/cluster-api-machine-role: infra
        sigs.k8s.io/cluster-api-machine-type: worker
    spec:
      providerConfig:
        value:
          apiVersion: libvirtproviderconfig/v1alpha1
          kind: LibvirtMachineProviderConfig
          domainMemory: 2048
          domainVcpu: 2
          ignKey: /var/lib/libvirt/images/worker.ign
          volume:
            poolName: default
            baseVolumeID: /var/lib/libvirt/images/coreos_base
          networkInterfaceName: tectonic
          networkInterfaceAddress: 192.168.124.12
          autostart: false
          uri: qemu+tcp://host_private_ip/system
      versions:
        kubelet: ""
        controlPlane: ""`)
}

func TestClusterapiControllerManifest(t *testing.T) {
	config := OperatorConfig{
		Provider: "libvirt",
		Libvirt: &libvirtConfig{
			URI:         "qemu+tcp://host_private_ip/system",
			Replicas:    "2",
			ClusterName: "test",
		},
	}

	testRenderManifest(t, "../../manifests/clusterapi-controller.yaml", &config, `
apiVersion: apps/v1beta1
kind: Deployment
metadata:
  name: clusterapi-controllers
  namespace: kube-system
  labels:
    api: clusterapi
    k8s-app: controller
    tectonic-operators.coreos.com/managed-by: machine-api-operator
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 65534
  selector:
    matchLabels:
      api: clusterapi
      k8s-app: controller
  replicas: 1
  template:
    metadata:
      labels:
        api: clusterapi
        k8s-app: controller
    spec:
      nodeSelector:
        node-role.kubernetes.io/master: ""
      tolerations:
      - effect: NoSchedule
        key: node-role.kubernetes.io/master
      - key: CriticalAddonsOnly
        operator: Exists
      - effect: NoExecute
        key: node.alpha.kubernetes.io/notReady
        operator: Exists
      - effect: NoExecute
        key: node.alpha.kubernetes.io/unreachable
        operator: Exists
      containers:
      - name: controller-manager
        image: gcr.io/k8s-cluster-api/controller-manager:0.0.7
        command:
        - "./controller-manager"
        resources:
          requests:
            cpu: 100m
            memory: 20Mi
          limits:
            cpu: 100m
            memory: 30Mi
      - name: libvirt-machine-controller
        image: quay.io/alberto_lamela/libvirt-machine-controller:0.0.1-dev # TODO: move this to openshift org
        env:
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
        command:
          - /machine-controller
        args:
          - --log-level=debug
        resources:
          requests:
            cpu: 100m
            memory: 20Mi
          limits:
            cpu: 100m
            memory: 30Mi`)
}
