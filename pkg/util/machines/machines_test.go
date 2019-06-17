package machines

import (
	"reflect"
	"testing"

	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	mapitesting "github.com/openshift/machine-api-operator/pkg/util/testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func init() {
	// Add types to scheme
	mapiv1.AddToScheme(scheme.Scheme)
	healthcheckingv1alpha1.AddToScheme(scheme.Scheme)
}

type expectedMdbs struct {
	mdbs  []*healthcheckingv1alpha1.MachineDisruptionBudget
	error bool
}

func TestGetMachineMachineDisruptionBudgets(t *testing.T) {
	mdb1 := mapitesting.NewMinAvailableMachineDisruptionBudget(3)
	mdb1.Name = "mdb1"

	mdb2 := mapitesting.NewMaxUnavailableMachineDisruptionBudget(3)
	mdb2.Name = "mdb2"

	mdbWithWrongSelector := mapitesting.NewMinAvailableMachineDisruptionBudget(3)
	mdbWithWrongSelector.Name = "mdbmachineWithoutLabelsithWrongSelector"
	mdbWithWrongSelector.Spec.Selector = &metav1.LabelSelector{
		MatchLabels:      map[string]string{},
		MatchExpressions: []metav1.LabelSelectorRequirement{{Operator: "fake"}},
	}

	mdbWithEmptySelector := mapitesting.NewMinAvailableMachineDisruptionBudget(3)
	mdbWithEmptySelector.Name = "mdbWithEmptySelector"
	mdbWithEmptySelector.Spec.Selector = &metav1.LabelSelector{}

	machine := mapitesting.NewMachine("machine", "node")

	machineWithoutLabels := mapitesting.NewMachine("machineWithoutLabels", "node")
	machineWithoutLabels.Labels = map[string]string{}

	machineWithWrongLabels := mapitesting.NewMachine("machineWithWrongLabels", "node")
	machineWithWrongLabels.Labels = map[string]string{"wrong": "wrong"}

	testsCases := []struct {
		machine  *mapiv1.Machine
		expected expectedMdbs
	}{
		{
			machine: machine,
			expected: expectedMdbs{
				mdbs:  []*healthcheckingv1alpha1.MachineDisruptionBudget{mdb1, mdb2},
				error: false,
			},
		},
		{
			machine: machineWithoutLabels,
			expected: expectedMdbs{
				mdbs:  nil,
				error: true,
			},
		},
		{
			machine: machineWithWrongLabels,
			expected: expectedMdbs{
				mdbs:  nil,
				error: false,
			},
		},
	}

	fakeClient := fake.NewFakeClient(mdb1, mdb2, mdbWithEmptySelector, mdbWithWrongSelector)
	for _, tc := range testsCases {
		mdbs, err := GetMachineMachineDisruptionBudgets(fakeClient, tc.machine)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error == true {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.machine.Name, errorExpectation, err)
		}

		if !reflect.DeepEqual(mdbs, tc.expected.mdbs) {
			t.Errorf("Test case: %s. Expected MDB's: %v, got: %v", tc.machine.Name, tc.expected.mdbs, mdbs)
		}
	}
}

type expectedMachines struct {
	machinesNames []string
	error         bool
}

func TestGetMahcinesByLabelSelector(t *testing.T) {
	emptyLabelSelector := &metav1.LabelSelector{}
	badLabelSelector := &metav1.LabelSelector{
		MatchLabels:      map[string]string{},
		MatchExpressions: []metav1.LabelSelectorRequirement{{Operator: "fake"}},
	}
	labelSelector := mapitesting.NewSelectorFooBar()

	machine1 := mapitesting.NewMachine("machine1", "node")
	machine2 := mapitesting.NewMachine("machine2", "node")
	machineWithWrongLabels := mapitesting.NewMachine("machineWithWrongLabels", "node")
	machineWithWrongLabels.Labels = map[string]string{"wrong": "wrong"}

	fakeClient := fake.NewFakeClient(machine1, machine2, machineWithWrongLabels)

	testsCases := []struct {
		name          string
		labelSelector *metav1.LabelSelector
		expected      expectedMachines
	}{
		{
			name:          "empty LabelSelector",
			labelSelector: emptyLabelSelector,
			expected: expectedMachines{
				machinesNames: nil,
				error:         false,
			},
		},
		{
			name:          "bad LabelSelector",
			labelSelector: badLabelSelector,
			expected: expectedMachines{
				machinesNames: nil,
				error:         true,
			},
		},
		{
			name:          "correct LabelSelector",
			labelSelector: labelSelector,
			expected: expectedMachines{
				machinesNames: []string{machine1.Name, machine2.Name},
				error:         false,
			},
		},
	}

	for _, tc := range testsCases {
		machines, err := GetMahcinesByLabelSelector(fakeClient, tc.labelSelector, mapitesting.Namespace)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error == true {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.name, errorExpectation, err)
		}

		if (tc.expected.machinesNames == nil) != (machines == nil) {
			t.Errorf("Test case: %s. Expected Machines: %v, got: %v", tc.name, tc.expected.machinesNames, machines)
		}

		if machines != nil {
			machineNames := []string{}
			for _, m := range machines.Items {
				machineNames = append(machineNames, m.Name)
			}

			if !reflect.DeepEqual(machineNames, tc.expected.machinesNames) {
				t.Errorf("Test case: %s. Expected Machines: %v, got: %v", tc.name, tc.expected.machinesNames, machineNames)
			}
		}
	}
}

type expectedNode struct {
	node  *corev1.Node
	error bool
}

func TestGetNodeByMachine(t *testing.T) {
	node := mapitesting.NewNode("node", true)
	machineWithNode := mapitesting.NewMachine("machineWithNode", node.Name)

	machineWithoutNodeRef := mapitesting.NewMachine("machine", "node")
	machineWithoutNodeRef.Status.NodeRef = nil

	machineWithoutNode := mapitesting.NewMachine("machine", "noNode")

	fakeClient := fake.NewFakeClient(node)

	testsCases := []struct {
		machine  *mapiv1.Machine
		expected expectedNode
	}{
		{
			machine: machineWithNode,
			expected: expectedNode{
				node:  node,
				error: false,
			},
		},
		{
			machine: machineWithoutNodeRef,
			expected: expectedNode{
				node:  nil,
				error: true,
			},
		},
		{
			machine: machineWithoutNode,
			expected: expectedNode{
				node:  nil,
				error: true,
			},
		},
	}

	for _, tc := range testsCases {
		node, err := GetNodeByMachine(fakeClient, tc.machine)
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error == true {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.machine.Name, errorExpectation, err)
		}

		if tc.expected.node != nil && node.Name != tc.expected.node.Name {
			t.Errorf("Test case: %s. Expected node: %v, got: %v", tc.machine.Name, tc.expected.node, node)
		}
	}
}

func TestIsMaster(t *testing.T) {
	// master node
	masterNode := mapitesting.NewNode("master", true)
	masterNode.Labels["node-role.kubernetes.io/master"] = ""

	// master machine
	masterMachine := mapitesting.NewMachine("master", masterNode.Name)
	masterMachine.Labels["machine.openshift.io/cluster-api-machine-role"] = "master"
	masterMachine.Labels["machine.openshift.io/cluster-api-machine-type"] = "master"

	// worker node
	workerNode := mapitesting.NewNode("worker", true)
	workerNode.Labels["node-role.kubernetes.io/worker"] = ""

	// worker machine
	workerMachine := mapitesting.NewMachine("worker", workerNode.Name)
	workerMachine.Labels["machine.openshift.io/cluster-api-machine-role"] = "worker"
	workerMachine.Labels["machine.openshift.io/cluster-api-machine-type"] = "worker"

	testCases := []struct {
		machine  *mapiv1.Machine
		expected bool
	}{
		{
			machine:  masterMachine,
			expected: true,
		},
		{
			machine:  workerMachine,
			expected: false,
		},
	}
	fakeClient := fake.NewFakeClient(masterNode, workerNode)
	for _, tc := range testCases {
		if got := IsMaster(fakeClient, tc.machine); got != tc.expected {
			t.Errorf("Test case: %s. Expected: %t, got: %t", tc.machine.Name, tc.expected, got)
		}
	}

}
