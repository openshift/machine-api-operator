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

package windows

import (
	"fmt"
	"strings"

	machinev1 "github.com/openshift/api/machine/v1beta1"
)

const (
	// these powershell tag constants are based on the secret creation in WMCO
	// they should follow how the secret is created there, see
	// https://github.com/openshift/windows-machine-config-operator/blob/master/pkg/secrets/secrets.go
	powershellOpenTag  = "<powershell>"
	powershellCloseTag = "</powershell>\n<persist>true</persist>"
)

// Return the supplied string wrapped with the powershell tags.
// If the string already has the tags, do not add them a second a time.
func AddPowershellTags(target string) string {
	if HasPowershellTags(target) {
		return target
	}

	return fmt.Sprintf("%s%s%s", powershellOpenTag, target, powershellCloseTag)
}

// Returns true if the string is wrapped with open and close tags for powershell.
func HasPowershellTags(target string) bool {
	return strings.HasPrefix(target, powershellOpenTag) && strings.HasSuffix(target, powershellCloseTag)
}

// Returns true if the Machine has the operating system label for a Windows instance.
func IsMachineOSWindows(machine machinev1.Machine) bool {
	osid, found := machine.Labels["machine.openshift.io/os-id"]
	if found && osid == "Windows" {
		return true
	}
	return false
}

// Return the supplied string with its powershell tags removed.
// This function will only remove the tags if both open and close exist,
// otherwise it returns the original.
func RemovePowershellTags(target string) string {
	if !HasPowershellTags(target) {
		return target
	}

	target = strings.TrimPrefix(target, powershellOpenTag)
	target = strings.TrimSuffix(target, powershellCloseTag)

	return target
}
