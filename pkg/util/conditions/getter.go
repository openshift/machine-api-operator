/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package conditions

import (
	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Getter interface defines methods that a Machine API object should implement in order to
// use the conditions package for getting conditions.
type Getter interface {
	runtime.Object
	metav1.Object

	// GetConditions returns the list of conditions for a cluster API object.
	GetConditions() machinev1.Conditions
}

// Get returns the condition with the given type, if the condition does not exists,
// it returns nil.
func Get(from interface{}, t machinev1.ConditionType) *machinev1.Condition {
	obj := getGetterObject(from)
	conditions := obj.GetConditions()
	if conditions == nil {
		return nil
	}

	for _, condition := range conditions {
		if condition.Type == t {
			return &condition
		}
	}
	return nil
}

func getGetterObject(from interface{}) Getter {
	switch obj := from.(type) {
	case machinev1.Machine:
		return &MachineWrapper{&obj}
	case machinev1.MachineHealthCheck:
		return &MachineHealthCheckWrapper{&obj}
	default:
		panic("type is not supported as conditions getter")
	}
}
