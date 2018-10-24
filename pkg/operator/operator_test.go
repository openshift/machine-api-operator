package operator

import (
	"testing"

	"k8s.io/api/core/v1"
)

func TestMinIncomingConfig(t *testing.T) {
	data := make(map[string]string)
	data["mao-config"] = `
apiVersion: v1
kind: machineAPIOperatorConfig
provider: aws
targetNamespace: openshift-cluster-api
`
	cfg := v1.ConfigMap{
		Data: data,
	}

	optr := &Operator{}
	res, err := optr.mcFromClusterConfig(&cfg)
	if err != nil {
		t.Errorf("failed to get config: %v", err)
	}
	if res.APIVersion != "v1" {
		t.Errorf("failed to get config: %v", err)
	}
	if res.Kind != "machineAPIOperatorConfig" {
		t.Errorf("failed to get config: %v", err)
	}
	if res.Provider != "aws" {
		t.Errorf("failed to get config: %v", err)
	}
	if res.TargetNamespace != "openshift-cluster-api" {
		t.Errorf("failed to get config: %v", err)
	}
}
