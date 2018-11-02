package main

import (
	"bytes"
	"io/ioutil"
	"text/template"

	"github.com/golang/glog"
)

// PopulateTemplate receives a template file path and renders its content populated with the config
func PopulateTemplate(config interface{}, path string) ([]byte, error) {

	data, err := ioutil.ReadFile(path)
	if err != nil {
		glog.Fatalf("failed reading file, %v", err)
	}

	buf := &bytes.Buffer{}
	tmpl, err := template.New("").Option("missingkey=error").Parse(string(data))
	if err != nil {
		return nil, err
	}

	if err := tmpl.Execute(buf, config); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}
