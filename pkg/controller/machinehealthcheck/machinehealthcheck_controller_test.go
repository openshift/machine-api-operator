package machinehealthcheck

import (
	"fmt"
	healthcheckingapis "github.com/openshift/machine-api-operator/pkg/apis"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	"k8s.io/api/core/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"reflect"
	capiv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

const (
	namespace = "openshift-cluster-api"
)

func init() {
	// Add types to scheme
	capiv1alpha1.AddToScheme(scheme.Scheme)
	healthcheckingapis.AddToScheme(scheme.Scheme)
}

func node(name string, ready bool) *v1.Node {
	nodeReadyStatus := corev1.ConditionTrue
	if !ready {
		nodeReadyStatus = corev1.ConditionFalse
	}

	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceNone,
			Annotations: map[string]string{
				"machine": fmt.Sprintf("%s/%s", namespace, "fakeMachine"),
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: nodeReadyStatus,
				},
			},
		},
	}
}

func machine(name string) *capiv1alpha1.Machine {
	return &capiv1alpha1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"foo": "a",
				"bar": "b",
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		Spec: capiv1alpha1.MachineSpec{},
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

func TestHasMatchingLabels(t *testing.T) {
	machine := machine("machine")
	testsCases := []struct {
		machine            *capiv1alpha1.Machine
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
			t.Errorf("Expected %t, got %t", tc.expected, got)
		}
	}
}

func TestIsHealthy(t *testing.T) {
	nodeHealthy := node("healthy", true)
	nodeUnhealthy := node("unhealthy", false)

	if health := isHealthy(nodeHealthy); !health {
		t.Errorf("Expected true, got %t", health)
	}
	if health := isHealthy(nodeUnhealthy); health {
		t.Errorf("Expected false, got %t", health)
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
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
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
		got := getNodeCondition(tc.node, tc.condition.Type)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Expected %v, got %v", tc.expected, got)
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
			t.Errorf("Expected %v, got: %v", tc.expected.result, result)
		}
		if tc.expected.error != (err != nil) {
			t.Errorf("Expected error, got %v", err)
		}
	}
}

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) reconcile.Reconciler {
	fakeClient := fake.NewFakeClient(initObjects...)
	return &ReconcileMachineHealthCheck{
		client: fakeClient,
		scheme: scheme.Scheme,
	}
}
