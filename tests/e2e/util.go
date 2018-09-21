package main

import (
	"bytes"
	"io/ioutil"
	"text/template"

	"github.com/golang/glog"
)

func renderTemplate(tmplData interface{}, data []byte) ([]byte, error) {
	buf := new(bytes.Buffer)

	tmpl, err := template.New("").Option("missingkey=error").Parse(string(data))
	if err != nil {
		return nil, err
	}

	if err := tmpl.Execute(buf, tmplData); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// RenderTemplateFromFile takes the config object, and uses that to render the templated manifest
func RenderTemplateFromFile(config interface{}, path string) ([]byte, error) {

	data, err := ioutil.ReadFile(path)
	if err != nil {
		glog.Fatalf("Error reading %#v", err)
	}

	populatedData, err := renderTemplate(config, data)
	if err != nil {
		glog.Fatalf("Unable to render manifests %q: %v", data, err)
	}
	return populatedData, nil
}
