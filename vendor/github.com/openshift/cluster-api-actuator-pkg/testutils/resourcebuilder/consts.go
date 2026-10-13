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

const (
	// OpenshiftMachineAPINamespaceName is the name of the OpenShift
	// Machine API namespace.
	OpenshiftMachineAPINamespaceName = "openshift-machine-api"

	// TestClusterIDValue is the clusterID in the test environment.
	TestClusterIDValue = "cluster-test-id"

	// MachineRoleLabelName is the name for the machine role label.
	MachineRoleLabelName = "machine.openshift.io/cluster-api-machine-role"

	// MachineTypeLabelName is the name for the machine type label.
	MachineTypeLabelName = "machine.openshift.io/cluster-api-machine-type"
)
