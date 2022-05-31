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
	"reflect"
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMachineToDelete(t *testing.T) {
	msg := "something wrong with the machine"
	now := metav1.Now()
	mustDeleteMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}}
	betterDeleteMachine := &machinev1.Machine{Status: machinev1.MachineStatus{ErrorMessage: &msg}}
	deleteMeMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{DeleteNodeAnnotation: "yes"}}}
	oldDeleteMeMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{oldDeleteNodeAnnotation: "yes"}}}
	runningMachine := &machinev1.Machine{Status: machinev1.MachineStatus{NodeRef: &corev1.ObjectReference{}}}
	notYetRunningMachine := &machinev1.Machine{}

	tests := []struct {
		desc     string
		machines []*machinev1.Machine
		diff     int
		expect   []*machinev1.Machine
	}{
		{
			desc: "func=randomDeletePolicy, diff=0",
			diff: 0,
			machines: []*machinev1.Machine{
				runningMachine,
			},
			expect: []*machinev1.Machine{},
		},
		{
			desc: "func=randomDeletePolicy, diff>len(machines)",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
			},
			expect: []*machinev1.Machine{
				runningMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, diff>betterDelete",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
				betterDeleteMachine,
				runningMachine,
			},
			expect: []*machinev1.Machine{
				betterDeleteMachine,
				runningMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, diff<betterDelete",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
				betterDeleteMachine,
				betterDeleteMachine,
				betterDeleteMachine,
			},
			expect: []*machinev1.Machine{
				betterDeleteMachine,
				betterDeleteMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, diff<=mustDelete",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
				mustDeleteMachine,
				betterDeleteMachine,
				mustDeleteMachine,
			},
			expect: []*machinev1.Machine{
				mustDeleteMachine,
				mustDeleteMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, diff<=mustDelete+betterDelete",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
				mustDeleteMachine,
				runningMachine,
				betterDeleteMachine,
			},
			expect: []*machinev1.Machine{
				mustDeleteMachine,
				betterDeleteMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, diff<=mustDelete+betterDelete+couldDelete",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
				mustDeleteMachine,
				runningMachine,
			},
			expect: []*machinev1.Machine{
				mustDeleteMachine,
				runningMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, diff>betterDelete",
			diff: 2,
			machines: []*machinev1.Machine{
				runningMachine,
				betterDeleteMachine,
				runningMachine,
			},
			expect: []*machinev1.Machine{
				betterDeleteMachine,
				runningMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, annotated, diff=1",
			diff: 1,
			machines: []*machinev1.Machine{
				runningMachine,
				deleteMeMachine,
				runningMachine,
			},
			expect: []*machinev1.Machine{
				deleteMeMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, annotated (old), diff=1",
			diff: 1,
			machines: []*machinev1.Machine{
				runningMachine,
				oldDeleteMeMachine,
				runningMachine,
			},
			expect: []*machinev1.Machine{
				oldDeleteMeMachine,
			},
		},
		{
			desc: "func=randomDeletePolicy, delete non-running hosts first",
			diff: 3,
			machines: []*machinev1.Machine{
				runningMachine,
				notYetRunningMachine,
				deleteMeMachine,
				betterDeleteMachine,
			},
			expect: []*machinev1.Machine{
				deleteMeMachine,
				betterDeleteMachine,
				notYetRunningMachine,
			},
		},
	}

	for _, test := range tests {
		result := getMachinesToDeletePrioritized(test.machines, test.diff, randomDeletePolicy)
		if !reflect.DeepEqual(result, test.expect) {
			t.Errorf("[case %s]", test.desc)
		}
	}
}

func TestMachineNewestDelete(t *testing.T) {

	currentTime := metav1.Now()
	statusError := machinev1.MachineStatusError("I'm unhealthy!")
	mustDeleteMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &currentTime}}
	newest := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -1))}}
	new := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -5))}}
	old := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}}
	oldest := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}}
	annotatedMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{DeleteNodeAnnotation: "yes"}, CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}}
	unhealthyMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}, Status: machinev1.MachineStatus{ErrorReason: &statusError}}

	tests := []struct {
		desc     string
		machines []*machinev1.Machine
		diff     int
		expect   []*machinev1.Machine
	}{
		{
			desc: "func=newestDeletePriority, diff=1",
			diff: 1,
			machines: []*machinev1.Machine{
				new, oldest, old, mustDeleteMachine, newest,
			},
			expect: []*machinev1.Machine{mustDeleteMachine},
		},
		{
			desc: "func=newestDeletePriority, diff=2",
			diff: 2,
			machines: []*machinev1.Machine{
				new, oldest, mustDeleteMachine, old, newest,
			},
			expect: []*machinev1.Machine{mustDeleteMachine, newest},
		},
		{
			desc: "func=newestDeletePriority, diff=3",
			diff: 3,
			machines: []*machinev1.Machine{
				new, mustDeleteMachine, oldest, old, newest,
			},
			expect: []*machinev1.Machine{mustDeleteMachine, newest, new},
		},
		{
			desc: "func=newestDeletePriority, diff=1 (annotated)",
			diff: 1,
			machines: []*machinev1.Machine{
				new, oldest, old, newest, annotatedMachine,
			},
			expect: []*machinev1.Machine{annotatedMachine},
		},
		{
			desc: "func=newestDeletePriority, diff=1 (unhealthy)",
			diff: 1,
			machines: []*machinev1.Machine{
				new, oldest, old, newest, unhealthyMachine,
			},
			expect: []*machinev1.Machine{unhealthyMachine},
		},
	}

	for _, test := range tests {
		result := getMachinesToDeletePrioritized(test.machines, test.diff, newestDeletePriority)
		if !reflect.DeepEqual(result, test.expect) {
			t.Errorf("[case %s]", test.desc)
		}
	}
}

func TestMachineOldestDelete(t *testing.T) {

	currentTime := metav1.Now()
	statusError := machinev1.MachineStatusError("I'm unhealthy!")
	empty := &machinev1.Machine{}
	newest := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -1))}}
	new := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -5))}}
	old := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}}
	oldest := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}}
	annotatedMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{DeleteNodeAnnotation: "yes"}, CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}}
	unhealthyMachine := &machinev1.Machine{ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.NewTime(currentTime.Time.AddDate(0, 0, -10))}, Status: machinev1.MachineStatus{ErrorReason: &statusError}}

	tests := []struct {
		desc     string
		machines []*machinev1.Machine
		diff     int
		expect   []*machinev1.Machine
	}{
		{
			desc: "func=oldestDeletePriority, diff=1",
			diff: 1,
			machines: []*machinev1.Machine{
				empty, new, oldest, old, newest,
			},
			expect: []*machinev1.Machine{oldest},
		},
		{
			desc: "func=oldestDeletePriority, diff=2",
			diff: 2,
			machines: []*machinev1.Machine{
				new, oldest, old, newest, empty,
			},
			expect: []*machinev1.Machine{oldest, old},
		},
		{
			desc: "func=oldestDeletePriority, diff=3",
			diff: 3,
			machines: []*machinev1.Machine{
				new, oldest, old, newest, empty,
			},
			expect: []*machinev1.Machine{oldest, old, new},
		},
		{
			desc: "func=oldestDeletePriority, diff=4",
			diff: 4,
			machines: []*machinev1.Machine{
				new, oldest, old, newest, empty,
			},
			expect: []*machinev1.Machine{oldest, old, new, newest},
		},
		{
			desc: "func=oldestDeletePriority, diff=1 (annotated)",
			diff: 1,
			machines: []*machinev1.Machine{
				empty, new, oldest, old, newest, annotatedMachine,
			},
			expect: []*machinev1.Machine{annotatedMachine},
		},
		{
			desc: "func=oldestDeletePriority, diff=1 (unhealthy)",
			diff: 1,
			machines: []*machinev1.Machine{
				empty, new, oldest, old, newest, unhealthyMachine,
			},
			expect: []*machinev1.Machine{unhealthyMachine},
		},
	}

	for _, test := range tests {
		result := getMachinesToDeletePrioritized(test.machines, test.diff, oldestDeletePriority)
		if !reflect.DeepEqual(result, test.expect) {
			t.Errorf("[case %s]", test.desc)
		}
	}
}
