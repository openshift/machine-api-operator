package xoperator

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/golang/glog"
	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest/marshal"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// ParseManifestsForVersion generates the upgrade definition for the given version.
// This assumes the manifest directory has a structure similar to the following one:
//
// - manifest_root
//      |___________version_1_dir
//      |              |____________component_1.yaml
//      |              |____________component_2_dir
//      |                              |______________component_2_1.json
//      |                              |______________component_2_2.json
//      |___________version_2_dir
//                     |____________component_1.yaml
//                     |____________component_2.yaml
//                     |____________component_3.yaml
//
//      ...
//
// This method will register any CRDs that are observed with the unmarshal library so that CRD
// instances can also be updated. The CRD must be observed before the CR.
func ParseManifestsForVersion(manifestDir, version string) (*types.UpgradeDefinition, error) {
	if version == "" {
		return nil, nil
	}

	var specList []types.UpgradeSpec

	dir := filepath.Join(manifestDir, version)
	_, err := os.Stat(dir)
	if err != nil {
		glog.Errorf("Failed to stat dir %q: %v", dir, err)
		return nil, err
	}

	if err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			glog.V(4).Infof("Skipping path %q, because it's a directory", path)
			return nil
		}

		bytes, err := ioutil.ReadFile(path)
		if err != nil {
			glog.Errorf("Failed to read file %q: %v", path, err)
			return err
		}

		spec, err := types.UpgradeSpecFromBytes(bytes)
		if err != nil {
			return fmt.Errorf("error manifest: %v", err)
		}

		if crd, ok := spec.Spec.(*v1beta1ext.CustomResourceDefinition); ok {
			marshal.RegisterCustomResourceDefinition(crd)
		}

		specList = append(specList, spec)
		return nil
	}); err != nil {
		return nil, err
	}

	return &types.UpgradeDefinition{Version: version, Items: specList}, nil
}

// isManifestDirEmpty returns whether the manifest is empty.
func (xo *xoperator) isManifestDirEmpty() (bool, error) {
	files, err := ioutil.ReadDir(xo.manifestDir)
	return len(files) == 0, err
}
