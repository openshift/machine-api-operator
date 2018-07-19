package render

import (
	"bytes"
	"io/ioutil"
	"path/filepath"
	"sync"
	"testing"

	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	machineAPI "github.com/coreos/tectonic-config/config/machine-api"
)

var (
	expected         map[string]bool
	created          map[string]bool
	s                *sync.Once
	expecUpgradeSpec []types.UpgradeSpec
)

// TestManifests is to ensure that a template is populated with the expected values
func TestManifests(t *testing.T) {

	// Create a config for populating the template
	config := &machineAPI.OperatorConfig{}
	config.RegistryHTTPSecret = "dummybase64secret"

	manifest := "../../manifests/registry/registry.yaml"
	m, err := filepath.Abs(manifest)
	if err != nil {
		t.Fatalf("Failed to obtain absolute path of manifest %q: %v", manifest, err)
	}
	data, err := ioutil.ReadFile(m)
	if err != nil {
		t.Fatalf("Failed to ingest manifest %q: %v", m, err)
	}

	rendered, err := Manifests(config, data)
	if err != nil {
		t.Fatalf("Failed to render manifest template: %v", err)
	}

	if bytes.Compare(rendered, expectedDNSService) != 0 {
		t.Errorf("Rendered manifest does not meet expectations \nExpected:\n %v \nReceive:\n %v", string(expectedDNSService), string(rendered))
	}
}
