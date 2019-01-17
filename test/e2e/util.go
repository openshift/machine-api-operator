package main

import (
	"sort"

	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

func sortByNewestCreationTimestamp(machines []capiv1alpha1.Machine) {
	sort.Slice(machines, func(i, j int) bool {
		if machines[i].CreationTimestamp.Equal(&machines[j].CreationTimestamp) {
			return machines[i].Name < machines[j].Name
		}
		return machines[i].CreationTimestamp.After(machines[j].CreationTimestamp.Time)
	})
}
