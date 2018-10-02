package render

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"text/template"

	"github.com/golang/glog"
)

const (
	providerAWS     = "aws"
	providerLibvirt = "libvirt"
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

	encodedCA := base64.StdEncoding.EncodeToString([]byte(config.APIServiceCA))

	tmplData := struct {
		OperatorConfig
		EncodedAPIServiceCA string
	}{
		OperatorConfig:      *config,
		EncodedAPIServiceCA: encodedCA,
	}

	if err := tmpl.Execute(buf, tmplData); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// PopulateTemplate takes the config object, and uses that to render the templated manifest
func PopulateTemplate(config *OperatorConfig, path string) ([]byte, error) {

	data, err := ioutil.ReadFile(path)
	if err != nil {
		glog.Fatalf("Error reading %#v", err)
	}

	populatedData, err := Manifests(config, data)
	if err != nil {
		glog.Fatalf("Unable to render manifests %q: %v", data, err)
	}
	return populatedData, nil
}
