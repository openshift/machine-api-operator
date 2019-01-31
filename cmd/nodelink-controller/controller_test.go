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

	mapiv1alpha1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
)

func node(taints *[]corev1.Taint) *corev1.Node {
	return &corev1.Node{
		Spec: corev1.NodeSpec{
			Taints: *taints,
		},
	}
}

func machine(taints *[]corev1.Taint) *mapiv1alpha1.Machine {
	return &mapiv1alpha1.Machine{
		Spec: mapiv1alpha1.MachineSpec{
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
