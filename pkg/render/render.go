package render

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"text/template"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"

	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/xoperator"
)

// Manifests takes the config object that contains the templated value,
// and uses that to render the templated manifest.
// 'config' must be non-nil, 'data' is the rawdata of a manifest file.
func Manifests(config *OperatorConfig, data []byte) ([]byte, error) {
	if config == nil {
		return nil, fmt.Errorf("no config is given")
	}

	buf := new(bytes.Buffer)

	tmpl, err := template.New("").Option("missingkey=error").Parse(string(data))
	if err != nil {
		return nil, err
	}

	if err := tmpl.Execute(buf, config); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// PopulateTemplates converts the config to manifests using the templates. Returns the temp directory of the manifests.
// caller is responsible for cleaning up the directory
func PopulateTemplates(config *OperatorConfig, templateDir string) (string, error) {

	absDir, err := filepath.Abs(templateDir)
	if err != nil {
		return "", fmt.Errorf("Unable to get the absolute path of %q: %v", templateDir, err)

	}

	// Use system default temp dir to store processed manifests.
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		return dir, fmt.Errorf("Failed to create temp dir: %v", err)
	}

	// Render a manifest for each template
	if err := filepath.Walk(templateDir, func(path string, info os.FileInfo, err error) error {

		// Check if there was a walk error
		if err != nil {
			return fmt.Errorf("Error when walking %q, current path %q: %v", templateDir, path, err)
		}

		rel, err := filepath.Rel(absDir, path)
		if err != nil {
			return fmt.Errorf("Failed to obtain relative path of %q and %q: %v", absDir, path, err)
		}
		dstPath := filepath.Join(dir, rel)

		if info.IsDir() {
			return os.MkdirAll(dstPath, 0755)
		}

		template, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to read template file %v: %v", path, err)
		}

		manifests, err := Manifests(config, template)
		if err != nil {
			return fmt.Errorf("Unable to render manifests: %v", err)
		}

		if err := ioutil.WriteFile(dstPath, manifests, 0644); err != nil {
			return fmt.Errorf("Failed to write manifests to %q: %v", dstPath, err)
		}

		return nil

	}); err != nil {
		return dir, fmt.Errorf("Failed to process manifests: %v", err)
	}

	return dir, nil
}

// MakeRenderer returns a new renderer function for the given config object and manifest directory.
func MakeRenderer(config *OperatorConfig, manifestDir string) xoperator.Renderer {
	return func() []types.UpgradeSpec {
		specs, err := processManifestsDir(config, manifestDir)
		if err != nil {
			glog.Exitf("Failed to create upgrade spec: %v", err)
		}
		return specs
	}
}

// MakeRendererWithManifests returns a renderer function for the given config object and the list of manifests.
func MakeRendererWithManifests(config *OperatorConfig, manifests map[string][]byte) xoperator.Renderer {
	return func() []types.UpgradeSpec {

		// Order doesn't matter here because it's only used
		// by the renderer to render manifests into disk.
		// This is just to work around the function's signature.
		var files []types.File
		for path, data := range manifests {
			files = append(files, types.File{Path: path, Data: data})
		}
		specs, err := processManifests(config, files)
		if err != nil {
			glog.Exitf("Failed to create upgrade spec: %v", err)
		}
		return specs
	}
}

// Config reads the local config file.
func Config(configFile string) (*OperatorConfig, error) {
	config, err := ioutil.ReadFile(configFile)
	if err != nil {
		return nil, fmt.Errorf("read config file %q: %v", configFile, err)
	}

	// Marshal into machineAPI config object
	var operatorConfig OperatorConfig
	if err := yaml.Unmarshal(config, &operatorConfig); err != nil {
		return nil, fmt.Errorf("unmarshal config file: %v", err)
	}

	return &operatorConfig, nil
}

// processManifestsDir converts the templates in the templateDir with the cluster config.
func processManifestsDir(config *OperatorConfig, templateDir string) ([]types.UpgradeSpec, error) {
	var manifests []types.File

	// Render a manifest for each template
	if err := filepath.Walk(templateDir, func(path string, info os.FileInfo, err error) error {

		// Check if there was a walk error
		if err != nil {
			return fmt.Errorf("Error when walking %q, current path %q: %v", templateDir, path, err)
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		bytes, err := ioutil.ReadFile(path)
		if err != nil {
			return fmt.Errorf("Failed to read template file %v: %v", path, err)
		}

		manifests = append(manifests, types.File{Path: path, Data: bytes})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Failed to process manifests: %v", err)
	}

	return processManifests(config, manifests)
}

// processManifests converts the templates in the template list with the cluster config.
func processManifests(config *OperatorConfig, manifests []types.File) ([]types.UpgradeSpec, error) {
	var speclist []types.UpgradeSpec

	for _, f := range manifests {
		result, err := Manifests(config, f.Data)
		if err != nil {
			return nil, fmt.Errorf("Unable to render manifests %q: %v", f.Path, err)
		}

		// Create upgrade spec from manifest bytes
		spec, err := types.UpgradeSpecFromBytes(result)
		if err != nil {
			return nil, fmt.Errorf("Unable to create upgrade spec from manifest: %v", err)
		}

		speclist = append(speclist, spec)
	}
	return speclist, nil
}
