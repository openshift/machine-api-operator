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
	"testing"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestMachineSetStatusSelectorMatchesScaleSubresource verifies that status.selector is the
// serialized label selector string expected by the CRD scale subresource (labelSelectorPath
// -> .status.selector), which the apiserver maps to autoscaling/v1 Scale status.selector.
func TestMachineSetStatusSelectorMatchesScaleSubresource(t *testing.T) {
	tests := []struct {
		name string
		spec metav1.LabelSelector
	}{
		{
			name: "matchLabels",
			spec: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"machine.openshift.io/cluster-api-cluster":    "cluster-id",
					"machine.openshift.io/cluster-api-machineset": "workers",
				},
			},
		},
		{
			name: "matchExpressions",
			spec: metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "role", Operator: metav1.LabelSelectorOpIn, Values: []string{"worker", "infra"}},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ms := &machinev1.MachineSet{
				Spec: machinev1.MachineSetSpec{
					Selector: tc.spec,
				},
			}
			r := &ReconcileMachineSet{}
			got := r.calculateStatus(ms, nil).Selector
			want := metav1.FormatLabelSelector(&ms.Spec.Selector)
			if got != want {
				t.Fatalf("status.selector = %q, want %q (must match Scale status.selector for HPA)", got, want)
			}
		})
	}
}
