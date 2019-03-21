package controller

import (
	"github.com/openshift/machine-api-operator/pkg/controller/disruption"
	"github.com/openshift/machine-api-operator/pkg/controller/machinehealthcheck"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, disruption.Add)
	AddToManagerFuncs = append(AddToManagerFuncs, machinehealthcheck.Add)
}
