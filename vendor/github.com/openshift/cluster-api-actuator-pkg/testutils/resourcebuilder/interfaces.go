/*
Copyright 2022 Red Hat, Inc.

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

package resourcebuilder

import (
	machinev1 "github.com/openshift/api/machine/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// ControlPlaneMachineSetTemplateBuilder builds a ControlPlaneMachineSetTemplate.
// This is used to create templates for embedding within a ControlPlaneMachineSet.
type ControlPlaneMachineSetTemplateBuilder interface {
	BuildTemplate() machinev1.ControlPlaneMachineSetTemplate
}

// OpenShiftMachineV1Beta1FailureDomainsBuilder builds a FailureDomains.
// This is used for setting the failure domains in the OpenShift machine template.
type OpenShiftMachineV1Beta1FailureDomainsBuilder interface {
	BuildFailureDomains() machinev1.FailureDomains
}

// RawExtensionBuilder builds a raw extension.
// This is used to create generic provider specs for embedding within Machines.
type RawExtensionBuilder interface {
	BuildRawExtension() *runtime.RawExtension
}
