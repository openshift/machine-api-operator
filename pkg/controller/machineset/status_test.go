/*
Copyright 2026 The Kubernetes Authors.

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
	"context"
	"testing"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestMachineSetStatusLabelSelectorMatchesScaleSubresource verifies that status.labelSelector is the
// serialized label selector string expected by the CRD scale subresource (labelSelectorPath
// -> .status.labelSelector), which the apiserver maps to autoscaling/v1 Scale status.selector.
func TestMachineSetStatusLabelSelectorMatchesScaleSubresource(t *testing.T) {
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
			g := NewWithT(t)

			ms := &machinev1.MachineSet{
				Spec: machinev1.MachineSetSpec{
					Selector: tc.spec,
				},
			}

			got := (&ReconcileMachineSet{}).calculateStatus(ms, nil).LabelSelector
			want := metav1.FormatLabelSelector(&ms.Spec.Selector)
			g.Expect(got).To(Equal(want), "status.labelSelector must match Scale status.selector for HPA")
		})
	}
}

func TestUpdateMachineSetStatusUpdatesLabelSelectorWithoutReplicaChanges(t *testing.T) {
	t.Helper()

	g := NewWithT(t)

	ms := &machinev1.MachineSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "machineset-test",
			Namespace:  "openshift-machine-api",
			Generation: 1,
		},
		Status: machinev1.MachineSetStatus{
			Replicas:             0,
			FullyLabeledReplicas: 0,
			ReadyReplicas:        0,
			AvailableReplicas:    0,
			ObservedGeneration:   1,
		},
	}

	cl := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithRuntimeObjects(ms.DeepCopy()).
		WithStatusSubresource(&machinev1.MachineSet{}).
		Build()

	current := &machinev1.MachineSet{}
	key := client.ObjectKeyFromObject(ms)
	g.Expect(cl.Get(context.Background(), key, current)).To(Succeed(), "failed to fetch machineset")

	newStatus := current.Status
	newStatus.LabelSelector = "machine.openshift.io/cluster-api-cluster=test-cluster"

	updated, err := updateMachineSetStatus(cl, current, newStatus)
	g.Expect(err).NotTo(HaveOccurred(), "failed to update machineset status")
	g.Expect(updated.Status.LabelSelector).To(Equal(newStatus.LabelSelector))

	stored := &machinev1.MachineSet{}
	g.Expect(cl.Get(context.Background(), key, stored)).To(Succeed(), "failed to refetch machineset")
	g.Expect(stored.Status.LabelSelector).To(Equal(newStatus.LabelSelector))
}
