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

package main

import (
	"reflect"
	"testing"

	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

func node(taints *[]corev1.Taint) *corev1.Node {
	return &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: *taints,
		},
	}
}

func machine(taints *[]corev1.Taint) *mapiv1.Machine {
	return &mapiv1.Machine{
		Spec: mapiv1.MachineSpec{
			Taints: *taints,
		},
	}
}

func TestAddTaintsToNode(t *testing.T) {
	testCases := []struct {
		description             string
		nodeTaints              []corev1.Taint
		machineTaints           []corev1.Taint
		expectedFinalNodeTaints []corev1.Taint
	}{
		{
			description:             "no previous taint on node. Machine adds none",
			nodeTaints:              []corev1.Taint{},
			machineTaints:           []corev1.Taint{},
			expectedFinalNodeTaints: []corev1.Taint{},
		},
		{
			description:             "no previous taint on node. Machine adds one",
			nodeTaints:              []corev1.Taint{},
			machineTaints:           []corev1.Taint{{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
			expectedFinalNodeTaints: []corev1.Taint{{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
		},
		{
			description:   "already taint on node. Machine adds another",
			nodeTaints:    []corev1.Taint{{Key: "key1", Value: "some-value", Effect: "Schedule"}},
			machineTaints: []corev1.Taint{{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
			expectedFinalNodeTaints: []corev1.Taint{{Key: "key1", Value: "some-value", Effect: "Schedule"},
				{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
		},
		{
			description:             "already taint on node. Machine adding same taint",
			nodeTaints:              []corev1.Taint{{Key: "key1", Value: "v1", Effect: "Schedule"}},
			machineTaints:           []corev1.Taint{{Key: "key1", Value: "v2", Effect: "Schedule"}},
			expectedFinalNodeTaints: []corev1.Taint{{Key: "key1", Value: "v1", Effect: "Schedule"}},
		},
	}

	for _, test := range testCases {
		machine := machine(&test.machineTaints)
		node := node(&test.nodeTaints)
		addTaintsToNode(node, machine)
		if !reflect.DeepEqual(node.Spec.Taints, test.expectedFinalNodeTaints) {
			t.Errorf("Test case: %s. Expected: %v, got: %v", test.description, test.expectedFinalNodeTaints, node.Spec.Taints)
		}
	}
}

func fakeController() *Controller {
	c := Controller{}
	c.machineAddress = make(map[string]*mapiv1.Machine)
	return &c
}

func TestAddUpdateDeleteMachine(t *testing.T) {
	testCases := []struct {
		description  string
		machine      mapiv1.Machine
		numAddresses int
	}{
		{
			description:  "Machine with no addresses",
			machine:      mapiv1.Machine{},
			numAddresses: 0,
		},
		{
			description: "Machine with one address",
			machine: mapiv1.Machine{
				Status: mapiv1.MachineStatus{
					Addresses: []corev1.NodeAddress{
						{
							Address: "192.168.1.1",
							Type:    "InternalIP",
						},
					},
				},
			},
			numAddresses: 1,
		},
		{
			description: "Machine with two addresses",
			machine: mapiv1.Machine{
				Status: mapiv1.MachineStatus{
					Addresses: []corev1.NodeAddress{
						{
							Address: "192.168.1.1",
							Type:    "InternalIP",
						},
						{
							Address: "172.0.20.2",
							Type:    "InternalIP",
						},
					},
				},
			},
			numAddresses: 2,
		},
		{
			description: "Use InternalIP only",
			machine: mapiv1.Machine{
				Status: mapiv1.MachineStatus{
					Addresses: []corev1.NodeAddress{
						{
							Address: "192.168.1.1",
							Type:    "InternalIP",
						},
						{
							Address: "10.0.20.2",
							Type:    "ExternalIP",
						},
						{
							Address: "host.example.com",
							Type:    "Hostname",
						},
					},
				},
			},
			numAddresses: 1,
		},
	}

	for _, test := range testCases {
		c := fakeController()
		c.addMachine(&test.machine)
		if len(c.machineAddress) != test.numAddresses {
			t.Errorf("Test case: %s, after addMachine(), Expected %d addresses, got %d", test.description, test.numAddresses, len(c.machineAddress))
		}
		c.updateMachine(mapiv1.Machine{}, &test.machine)
		if len(c.machineAddress) != test.numAddresses {
			t.Errorf("Test case: %s, after updateMachine(), Expected %d addresses, got %d", test.description, test.numAddresses, len(c.machineAddress))
		}
		c.deleteMachine(&test.machine)
		if len(c.machineAddress) > 0 {
			t.Errorf("Test case: %s, after deleteMachine(), Expected 0 addresses, got %d", test.description, len(c.machineAddress))
		}
	}
}
