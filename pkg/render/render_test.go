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

func TestClusterapiControllerManifest(t *testing.T) {
	config := OperatorConfig{
		TargetNamespace: "go-test",
		Provider:        "libvirt",
		Libvirt: &LibvirtConfig{
			URI:         "qemu+tcp://host_private_ip/system",
			NetworkName: "testNet",
			IPRange:     "192.168.124.0/24",
			Replicas:    "2",
			ClusterID:   "test",
		},
		Images: &Images{
			ClusterAPIControllerManagerLibvirt: "docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0",
			ClusterAPIControllerLibvirt:        "docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0",
		},
	}

	testRenderManifest(t, "../../owned-manifests/clusterapi-manager-controllers.yaml", &config, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: clusterapi-manager-controllers
  namespace: go-test
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
        image: docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0
        command:
        - "./manager"
        resources:
          requests:
            cpu: 100m
            memory: 20Mi
          limits:
            cpu: 100m
            memory: 30Mi
      - name: libvirt-machine-controller
        image: docker.io/openshift/origin-libvirt-machine-controllers:v4.0.0
        env:
          - name: NODE_NAME
            valueFrom:
              fieldRef:
                fieldPath: spec.nodeName
        command:
          - /machine-controller-manager
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
