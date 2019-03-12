package machinehealthcheck

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	mapiv1alpha1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	namespace         = "openshift-machine-api"
	badConditionsData = `items:
- name: Ready 
  timeout: 60s
  status: Unknown`
)

var (
	knownDate = metav1.Time{Time: time.Date(1985, 06, 03, 0, 0, 0, 0, time.Local)}
)

func init() {
	// Add types to scheme
	mapiv1alpha1.AddToScheme(scheme.Scheme)
	healthcheckingv1alpha1.AddToScheme(scheme.Scheme)
}

func node(name string, ready bool) *v1.Node {
	nodeReadyStatus := corev1.ConditionTrue
	if !ready {
		nodeReadyStatus = corev1.ConditionUnknown
	}

	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceNone,
			Annotations: map[string]string{
				"machine": fmt.Sprintf("%s/%s", namespace, "fakeMachine"),
			},
			Labels: map[string]string{},
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             nodeReadyStatus,
					LastTransitionTime: knownDate,
				},
			},
		},
	}
}

func machine(name string) *mapiv1alpha1.Machine {
	return &mapiv1alpha1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"foo": "a",
				"bar": "b",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: ownerControllerKind,
				},
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		Spec: mapiv1alpha1.MachineSpec{},
	}
}

func machineHealthCheck(name string) *healthcheckingv1alpha1.MachineHealthCheck {
	return &healthcheckingv1alpha1.MachineHealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "MachineHealthCheck",
		},
		Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"foo": "a",
					"bar": "b",
				},
			},
		},
		Status: healthcheckingv1alpha1.MachineHealthCheckStatus{},
	}
}

func configMap(name string, data string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		Data: map[string]string{
			"conditions": data,
		},
	}
}

func TestHasMatchingLabels(t *testing.T) {
	machine := machine("machine")
	testsCases := []struct {
		machine            *mapiv1alpha1.Machine
		machineHealthCheck *healthcheckingv1alpha1.MachineHealthCheck
		expected           bool
	}{
		{
			machine: machine,
			machineHealthCheck: &healthcheckingv1alpha1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "MatchingLabels",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"foo": "a",
							"bar": "b",
						},
					},
				},
				Status: healthcheckingv1alpha1.MachineHealthCheckStatus{},
			},
			expected: true,
		},
		{
			machine: machine,
			machineHealthCheck: &healthcheckingv1alpha1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "NoMatchingLabels",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: healthcheckingv1alpha1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"no": "match",
						},
					},
				},
				Status: healthcheckingv1alpha1.MachineHealthCheckStatus{},
			},
			expected: false,
		},
	}

	for _, tc := range testsCases {
		if got := hasMatchingLabels(tc.machineHealthCheck, tc.machine); got != tc.expected {
			t.Errorf("Test case: %s. Expected: %t, got: %t", tc.machineHealthCheck.Name, tc.expected, got)
		}
	}
}

func TestGetNodeCondition(t *testing.T) {

	testsCases := []struct {
		node      *corev1.Node
		condition *corev1.NodeCondition
		expected  *corev1.NodeCondition
	}{
		{
			node: node("hasCondition", true),
			condition: &corev1.NodeCondition{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
			expected: &corev1.NodeCondition{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: knownDate,
			},
		},
		{
			node: node("doesNotHaveCondition", true),
			condition: &corev1.NodeCondition{
				Type:   corev1.NodeOutOfDisk,
				Status: corev1.ConditionTrue,
			},
			expected: nil,
		},
	}

	for _, tc := range testsCases {
		got := conditions.GetNodeCondition(tc.node, tc.condition.Type)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Test case: %s. Expected: %v, got: %v", tc.node.Name, tc.expected, got)
		}
	}

}

type expectedReconcile struct {
	result reconcile.Result
	error  bool
}

func TestReconcile(t *testing.T) {
	nodeHealthy := node("healthy", true)
	nodeUnhealthy := node("unhealthy", false)
	nodeWithNoMachineAnnotation := node("noAnnotated", true)
	nodeWithNoMachineAnnotation.Annotations = map[string]string{}
	nodeAnnotatedWithNoExistentMachine := node("noExistentMachine", true)
	nodeAnnotatedWithNoExistentMachine.Annotations[machineAnnotationKey] = "noExistentMachine"
	fakeMachine := machine("fakeMachine")
	fakeMachine.Status = mapiv1alpha1.MachineStatus{
		NodeRef: &corev1.ObjectReference{
			Namespace: "",
			Name:      "healthy",
		},
	}

	testsCases := []struct {
		node     *v1.Node
		expected expectedReconcile
	}{
		{
			node: nodeHealthy,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			node: nodeUnhealthy,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			node: nodeWithNoMachineAnnotation,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			node: nodeAnnotatedWithNoExistentMachine,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
	}

	machineHealthCheck := machineHealthCheck("machineHealthCheck")
	allMachineHealthChecks := &healthcheckingv1alpha1.MachineHealthCheckList{
		Items: []healthcheckingv1alpha1.MachineHealthCheck{
			*machineHealthCheck,
		},
	}

	r := newFakeReconciler(nodeHealthy, nodeUnhealthy, fakeMachine, allMachineHealthChecks)
	for _, tc := range testsCases {
		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Namespace: "",
				Name:      tc.node.Name,
			},
		}
		result, err := r.Reconcile(request)
		if result != tc.expected.result {
			t.Errorf("Test case: %v. Expected: %v, got: %v", tc.node.Name, tc.expected.result, result)
		}
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected %s error, got: %v", tc.node.Name, errorExpectation, err)
		}
	}
}

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) *ReconcileMachineHealthCheck {
	fakeClient := fake.NewFakeClient(initObjects...)
	return &ReconcileMachineHealthCheck{
		client:    fakeClient,
		scheme:    scheme.Scheme,
		namespace: namespace,
	}
}

func TestHasMachineSetOwner(t *testing.T) {
	machineWithMachineSet := machine("machineWithMachineSet")
	machineWithNoMachineSet := machine("machineWithNoMachineSet")
	machineWithNoMachineSet.OwnerReferences = nil

	testsCases := []struct {
		machine  *mapiv1alpha1.Machine
		expected bool
	}{
		{
			machine:  machineWithNoMachineSet,
			expected: false,
		},
		{
			machine:  machineWithMachineSet,
			expected: true,
		},
	}

	for _, tc := range testsCases {
		if got := hasMachineSetOwner(*tc.machine); got != tc.expected {
			t.Errorf("Test case: Machine %s. Expected: %t, got: %t", tc.machine.Name, tc.expected, got)
		}
	}

}

func TestUnhealthyForTooLong(t *testing.T) {
	nodeUnhealthyForTooLong := node("nodeUnhealthyForTooLong", false)
	nodeRecentlyUnhealthy := node("nodeRecentlyUnhealthy", false)
	nodeRecentlyUnhealthy.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: time.Now()}
	testsCases := []struct {
		node     *corev1.Node
		expected bool
	}{
		{
			node:     nodeUnhealthyForTooLong,
			expected: true,
		},
		{
			node:     nodeRecentlyUnhealthy,
			expected: false,
		},
	}
	for _, tc := range testsCases {
		if got := unhealthyForTooLong(&tc.node.Status.Conditions[0], time.Minute); got != tc.expected {
			t.Errorf("Test case: %s. Expected: %t, got: %t", tc.node.Name, tc.expected, got)
		}
	}
}

func testRemediation(t *testing.T, remediationWaitTime time.Duration, initObjects ...runtime.Object) {
	nodeHealthy := node("nodeHealthy", true)
	nodeHealthy.Annotations = map[string]string{
		"machine": fmt.Sprintf("%s/%s", namespace, "machineWithNodehealthy"),
	}

	nodeRecentlyUnhealthy := node("nodeRecentlyUnhealthy", false)
	nodeRecentlyUnhealthy.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: time.Now()}
	nodeRecentlyUnhealthy.Annotations = map[string]string{
		"machine": fmt.Sprintf("%s/%s", namespace, "machineWithNodeRecentlyUnhealthy"),
	}

	machineWithNodeRecentlyUnhealthy := machine("machineWithNodeRecentlyUnhealthy")
	machineWithNodeRecentlyUnhealthy.Status = mapiv1alpha1.MachineStatus{
		NodeRef: &corev1.ObjectReference{
			Namespace: "",
			Name:      "nodeRecentlyUnhealthy",
		},
	}

	machineWithNodeHealthy := machine("machineWithNodehealthy")
	machineWithNodeHealthy.Status = mapiv1alpha1.MachineStatus{
		NodeRef: &corev1.ObjectReference{
			Namespace: "",
			Name:      "nodeHealthy",
		},
	}

	machineWithNoOwnerController := machine("machineWithNoOwnerController")
	machineWithNoOwnerController.OwnerReferences = nil

	machineWithNoNodeRef := machine("machineWithNoNodeRef")

	testsCases := []struct {
		machine  *mapiv1alpha1.Machine
		expected expectedReconcile
	}{
		{
			machine: machineWithNodeHealthy,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			machine: machineWithNodeRecentlyUnhealthy,
			expected: expectedReconcile{
				result: reconcile.Result{
					Requeue:      true,
					RequeueAfter: remediationWaitTime,
				},
				error: false,
			},
		},
		{
			machine: machineWithNoOwnerController,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			machine: machineWithNoNodeRef,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  true,
			},
		},
	}

	initObjects = append(initObjects, nodeHealthy)
	initObjects = append(initObjects, nodeRecentlyUnhealthy)
	initObjects = append(initObjects, machineWithNodeHealthy)
	initObjects = append(initObjects, machineWithNodeRecentlyUnhealthy)
	initObjects = append(initObjects, machineWithNoOwnerController)
	initObjects = append(initObjects, machineWithNoNodeRef)

	r := newFakeReconciler(initObjects...)
	for _, tc := range testsCases {
		result, err := remediate(r, tc.machine)
		if result != tc.expected.result {
			t.Errorf("Test case: %s. Expected: %v, got: %v", tc.machine.Name, tc.expected.result, result)
		}
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error == true {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.machine.Name, errorExpectation, err)
		}
	}
}

func TestRemediateWithoutUnhealthyConditionsConfigMap(t *testing.T) {
	testRemediation(t, 5*time.Minute)
}

func TestRemediateWithUnhealthyConditionsConfigMap(t *testing.T) {
	cmBadConditions := configMap(healthcheckingv1alpha1.ConfigMapNodeUnhealthyConditions, badConditionsData)
	testRemediation(t, 1*time.Minute, cmBadConditions)
}

func TestIsMaster(t *testing.T) {
	masterMachine := machine("master")
	masterMachine.Labels["machine.openshift.io/cluster-api-machine-role"] = "master"
	masterMachine.Labels["machine.openshift.io/cluster-api-machine-type"] = "master"
	masterMachine.Status = mapiv1alpha1.MachineStatus{
		NodeRef: &corev1.ObjectReference{
			Namespace: "",
			Name:      "master",
		},
	}
	masterNode := node("master", true)
	masterNode.Annotations = map[string]string{
		"machine": fmt.Sprintf("%s/%s", namespace, "master"),
	}
	masterNode.Labels["node-role.kubernetes.io/master"] = ""

	workerMachine := machine("worker")
	workerMachine.Labels["machine.openshift.io/cluster-api-machine-role"] = "worker"
	workerMachine.Labels["machine.openshift.io/cluster-api-machine-type"] = "worker"

	workerMachine.Status = mapiv1alpha1.MachineStatus{
		NodeRef: &corev1.ObjectReference{
			Namespace: "",
			Name:      "worker",
		},
	}
	workerNode := node("worker", true)
	workerNode.Annotations = map[string]string{
		"machine": fmt.Sprintf("%s/%s", namespace, "worker"),
	}
	workerNode.Labels["node-role.kubernetes.io/worker"] = ""

	testCases := []struct {
		machine  *mapiv1alpha1.Machine
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
		if got := isMaster(*tc.machine, fakeClient); got != tc.expected {
			t.Errorf("Test case: %s. Expected: %t, got: %t", tc.machine.Name, tc.expected, got)
		}
	}

}
