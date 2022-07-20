/*
Copyright 2018 The Kubernetes Authors.

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

package machineset

import (
	"fmt"
	"math"
	"sort"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type deletePriority float64

const (

	// DeleteNodeAnnotation marks nodes that will be given priority for deletion
	// when a machineset scales down. This annotation is given top priority on all delete policies.
	DeleteNodeAnnotation = "machine.openshift.io/delete-machine"

	// oldDeleteNodeAnnotation is the previous version of the DeleteNodeAnnotation.
	// This was changed so that the new version, compatible with the cluster api Kubernetes Autoscaler
	// provider could be preferred.
	oldDeleteNodeAnnotation = "machine.openshift.io/cluster-api-delete-machine"

	mustDelete    deletePriority = 100.0
	betterDelete  deletePriority = 50.0
	preferDelete  deletePriority = 40.0
	couldDelete   deletePriority = 20.0
	mustNotDelete deletePriority = 0.0

	secondsPerTenDays float64 = 864000
)

type deletePriorityFunc func(machine *machinev1.Machine) deletePriority

// maps the creation timestamp onto the 0-100 priority range
func oldestDeletePriority(machine *machinev1.Machine) deletePriority {
	if machine.DeletionTimestamp != nil && !machine.DeletionTimestamp.IsZero() {
		return mustDelete
	}
	if machine.ObjectMeta.Annotations != nil && (machine.ObjectMeta.Annotations[DeleteNodeAnnotation] != "" || machine.ObjectMeta.Annotations[oldDeleteNodeAnnotation] != "") {
		return mustDelete
	}
	if machine.Status.ErrorReason != nil || machine.Status.ErrorMessage != nil {
		return mustDelete
	}
	if machine.ObjectMeta.CreationTimestamp.Time.IsZero() {
		return mustNotDelete
	}
	d := metav1.Now().Sub(machine.ObjectMeta.CreationTimestamp.Time)
	if d.Seconds() < 0 {
		return mustNotDelete
	}
	return deletePriority(float64(mustDelete) * (1.0 - math.Exp(-d.Seconds()/secondsPerTenDays)))
}

func newestDeletePriority(machine *machinev1.Machine) deletePriority {
	if machine.DeletionTimestamp != nil && !machine.DeletionTimestamp.IsZero() {
		return mustDelete
	}
	if machine.ObjectMeta.Annotations != nil && (machine.ObjectMeta.Annotations[DeleteNodeAnnotation] != "" || machine.ObjectMeta.Annotations[oldDeleteNodeAnnotation] != "") {
		return mustDelete
	}
	if machine.Status.ErrorReason != nil || machine.Status.ErrorMessage != nil {
		return mustDelete
	}
	return mustDelete - oldestDeletePriority(machine)
}

func randomDeletePolicy(machine *machinev1.Machine) deletePriority {
	if machine.DeletionTimestamp != nil && !machine.DeletionTimestamp.IsZero() {
		return mustDelete
	}
	if machine.ObjectMeta.Annotations != nil && (machine.ObjectMeta.Annotations[DeleteNodeAnnotation] != "" || machine.ObjectMeta.Annotations[oldDeleteNodeAnnotation] != "") {
		return betterDelete
	}
	if machine.Status.ErrorReason != nil || machine.Status.ErrorMessage != nil {
		return betterDelete
	}
	// The machine doesn't have a Node yet, and therefore isn't running any workloads
	if machine.Status.NodeRef == nil {
		return preferDelete
	}
	return couldDelete
}

type sortableMachines struct {
	machines []*machinev1.Machine
	priority deletePriorityFunc
}

func (m sortableMachines) Len() int      { return len(m.machines) }
func (m sortableMachines) Swap(i, j int) { m.machines[i], m.machines[j] = m.machines[j], m.machines[i] }
func (m sortableMachines) Less(i, j int) bool {
	return m.priority(m.machines[j]) < m.priority(m.machines[i]) // high to low
}

func getMachinesToDeletePrioritized(filteredMachines []*machinev1.Machine, diff int, fun deletePriorityFunc) []*machinev1.Machine {
	if diff >= len(filteredMachines) {
		return filteredMachines
	} else if diff <= 0 {
		return []*machinev1.Machine{}
	}

	sortable := sortableMachines{
		machines: filteredMachines,
		priority: fun,
	}
	sort.Sort(sortable)

	return sortable.machines[:diff]
}

func getDeletePriorityFunc(ms *machinev1.MachineSet) (deletePriorityFunc, error) {
	// Map the Spec.DeletePolicy value to the appropriate delete priority function
	switch msdp := machinev1.MachineSetDeletePolicy(ms.Spec.DeletePolicy); msdp {
	case machinev1.RandomMachineSetDeletePolicy:
		return randomDeletePolicy, nil
	case machinev1.NewestMachineSetDeletePolicy:
		return newestDeletePriority, nil
	case machinev1.OldestMachineSetDeletePolicy:
		return oldestDeletePriority, nil
	case "":
		return randomDeletePolicy, nil
	default:
		return nil, fmt.Errorf("Unsupported delete policy %s. Must be one of 'Random', 'Newest', or 'Oldest'", msdp)
	}
}
