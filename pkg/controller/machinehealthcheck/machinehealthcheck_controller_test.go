package machinehealthcheck

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	mapiv1beta1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	maotesting "github.com/openshift/machine-api-operator/pkg/util/testing"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	namespace = "openshift-machine-api"
)

func init() {
	// Add types to scheme
	mapiv1beta1.AddToScheme(scheme.Scheme)
}

func TestHasMatchingLabels(t *testing.T) {
	machine := maotesting.NewMachine("machine", "node")
	testsCases := []struct {
		machine            *mapiv1beta1.Machine
		machineHealthCheck *mapiv1beta1.MachineHealthCheck
		expected           bool
	}{
		{
			machine:            machine,
			machineHealthCheck: maotesting.NewMachineHealthCheck("foobar"),
			expected:           true,
		},
		{
			machine: machine,
			machineHealthCheck: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "NoMatchingLabels",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"no": "match",
						},
					},
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{},
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
			node: maotesting.NewNode("hasCondition", true),
			condition: &corev1.NodeCondition{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			},
			expected: &corev1.NodeCondition{
				Type:               corev1.NodeReady,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: maotesting.KnownDate,
			},
		},
		{
			node: maotesting.NewNode("doesNotHaveCondition", true),
			condition: &corev1.NodeCondition{
				Type:   corev1.NodeDiskPressure,
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

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(initObjects ...runtime.Object) *ReconcileMachineHealthCheck {
	fakeClient := fake.NewFakeClient(initObjects...)
	return &ReconcileMachineHealthCheck{
		client:    fakeClient,
		scheme:    scheme.Scheme,
		namespace: namespace,
	}
}

type expectedReconcile struct {
	result reconcile.Result
	error  bool
}

func TestReconcile(t *testing.T) {
	// healthy node
	nodeHealthy := maotesting.NewNode("healthy", true)
	nodeHealthy.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithNodehealthy"),
	}
	machineWithNodeHealthy := maotesting.NewMachine("machineWithNodehealthy", nodeHealthy.Name)

	// recently unhealthy node
	nodeRecentlyUnhealthy := maotesting.NewNode("recentlyUnhealthy", false)
	nodeRecentlyUnhealthy.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: time.Now()}
	nodeRecentlyUnhealthy.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithNodeRecentlyUnhealthy"),
	}
	machineWithNodeRecentlyUnhealthy := maotesting.NewMachine("machineWithNodeRecentlyUnhealthy", nodeRecentlyUnhealthy.Name)

	// node without machine annotation
	nodeWithoutMachineAnnotation := maotesting.NewNode("withoutMachineAnnotation", true)
	nodeWithoutMachineAnnotation.Annotations = map[string]string{}

	// node annotated with machine that does not exist
	nodeAnnotatedWithNoExistentMachine := maotesting.NewNode("annotatedWithNoExistentMachine", true)
	nodeAnnotatedWithNoExistentMachine.Annotations[machineAnnotationKey] = "annotatedWithNoExistentMachine"

	// node annotated with machine without owner reference
	nodeAnnotatedWithMachineWithoutOwnerReference := maotesting.NewNode("annotatedWithMachineWithoutOwnerReference", true)
	nodeAnnotatedWithMachineWithoutOwnerReference.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithoutOwnerController"),
	}
	machineWithoutOwnerController := maotesting.NewMachine("machineWithoutOwnerController", nodeAnnotatedWithMachineWithoutOwnerReference.Name)
	machineWithoutOwnerController.OwnerReferences = nil

	// node annotated with machine without node reference
	nodeAnnotatedWithMachineWithoutNodeReference := maotesting.NewNode("annotatedWithMachineWithoutNodeReference", true)
	nodeAnnotatedWithMachineWithoutNodeReference.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineWithoutNodeRef"),
	}
	machineWithoutNodeRef := maotesting.NewMachine("machineWithoutNodeRef", nodeAnnotatedWithMachineWithoutNodeReference.Name)
	machineWithoutNodeRef.Status.NodeRef = nil

	machineHealthCheck := maotesting.NewMachineHealthCheck("machineHealthCheck")

	// remediationReboot
	nodeUnhealthyForTooLong := maotesting.NewNode("nodeUnhealthyForTooLong", false)
	nodeUnhealthyForTooLong.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineUnhealthyForTooLong"),
	}
	machineUnhealthyForTooLong := maotesting.NewMachine("machineUnhealthyForTooLong", nodeUnhealthyForTooLong.Name)

	testCases := []struct {
		testCase string
		machine  *mapiv1beta1.Machine
		node     *corev1.Node
		expected expectedReconcile
	}{
		{
			testCase: "machine unhealthy",
			machine:  machineUnhealthyForTooLong,
			node:     nodeUnhealthyForTooLong,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			testCase: "machine with node unhealthy",
			machine:  machineWithNodeHealthy,
			node:     nodeHealthy,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			testCase: "machine with node likely to go unhealthy",
			machine:  machineWithNodeRecentlyUnhealthy,
			node:     nodeRecentlyUnhealthy,
			expected: expectedReconcile{
				result: reconcile.Result{
					Requeue:      true,
					RequeueAfter: 300 * time.Second,
				},
				error: false,
			},
		},
		{
			testCase: "no target: no machine and bad node annotation",
			machine:  nil,
			node:     nodeWithoutMachineAnnotation,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			testCase: "no target: no machine",
			machine:  nil,
			node:     nodeAnnotatedWithNoExistentMachine,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			testCase: "machine no controller owner",
			machine:  machineWithoutOwnerController,
			node:     nodeAnnotatedWithMachineWithoutOwnerReference,
			expected: expectedReconcile{
				result: reconcile.Result{},
				error:  false,
			},
		},
		{
			testCase: "machine no noderef",
			machine:  machineWithoutNodeRef,
			node:     nodeAnnotatedWithMachineWithoutNodeReference,
			expected: expectedReconcile{
				result: reconcile.Result{
					RequeueAfter: timeoutForMachineToHaveNode,
				},
				error: false,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			var objects []runtime.Object
			objects = append(objects, machineHealthCheck)
			if tc.machine != nil {
				objects = append(objects, tc.machine)
			}
			objects = append(objects, tc.node)
			r := newFakeReconciler(objects...)

			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: machineHealthCheck.GetNamespace(),
					Name:      machineHealthCheck.GetName(),
				},
			}
			result, err := r.Reconcile(request)
			if tc.expected.error != (err != nil) {
				var errorExpectation string
				if !tc.expected.error {
					errorExpectation = "no"
				}
				t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.node.Name, errorExpectation, err)
			}

			if result != tc.expected.result {
				if tc.expected.result.Requeue {
					before := tc.expected.result.RequeueAfter
					after := tc.expected.result.RequeueAfter + time.Second
					if after < result.RequeueAfter || before > result.RequeueAfter {
						t.Errorf("Test case: %s. Expected RequeueAfter between: %v and %v, got: %v", tc.node.Name, before, after, result)
					}
				} else {
					t.Errorf("Test case: %s. Expected: %v, got: %v", tc.node.Name, tc.expected.result, result)
				}
			}
		})
	}
}

func TestHasMachineSetOwner(t *testing.T) {
	machineWithMachineSet := maotesting.NewMachine("machineWithMachineSet", "node")
	machineWithNoMachineSet := maotesting.NewMachine("machineWithNoMachineSet", "node")
	machineWithNoMachineSet.OwnerReferences = nil

	testsCases := []struct {
		target   target
		expected bool
	}{
		{
			target: target{
				Machine: *machineWithNoMachineSet,
			},
			expected: false,
		},
		{
			target: target{
				Machine: *machineWithMachineSet,
			},
			expected: true,
		},
	}

	for _, tc := range testsCases {
		if got := tc.target.hasMachineSetOwner(); got != tc.expected {
			t.Errorf("Test case: Machine %s. Expected: %t, got: %t", tc.target.Machine.Name, tc.expected, got)
		}
	}

}

func TestApplyRemediationReboot(t *testing.T) {
	nodeUnhealthyForTooLong := maotesting.NewNode("nodeUnhealthyForTooLong", false)
	nodeUnhealthyForTooLong.Annotations = map[string]string{
		machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machineUnhealthyForTooLong"),
	}
	machineUnhealthyForTooLong := maotesting.NewMachine("machineUnhealthyForTooLong", nodeUnhealthyForTooLong.Name)
	machineHealthCheck := maotesting.NewMachineHealthCheck("machineHealthCheck")
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "",
			Name:      nodeUnhealthyForTooLong.Name,
		},
	}
	r := newFakeReconciler(nodeUnhealthyForTooLong, machineUnhealthyForTooLong, machineHealthCheck)
	target := target{
		Node:    nodeUnhealthyForTooLong,
		Machine: *machineUnhealthyForTooLong,
		MHC:     mapiv1beta1.MachineHealthCheck{},
	}
	if err := target.remediationStrategyReboot(r); err != nil {
		t.Fatalf("unexpected error %v", err)
	}

	node := &corev1.Node{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, node); err != nil {
		t.Errorf("Expected: no error, got: %v", err)
	}

	if _, ok := node.Annotations[machineRebootAnnotationKey]; !ok {
		t.Errorf("Expected: node to have reboot annotion %s, got: %v", machineRebootAnnotationKey, node.Annotations)
	}
}

func TestMHCRequestsFromMachine(t *testing.T) {
	testCases := []struct {
		testCase         string
		mhcs             []*mapiv1beta1.MachineHealthCheck
		machine          *mapiv1beta1.Machine
		expectedRequests []reconcile.Request
	}{
		{
			testCase: "at least one match",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "noMatch",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine: maotesting.NewMachine("test", "node1"),
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: namespace,
						Name:      "match",
					},
				},
			},
		},
		{
			testCase: "more than one match",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match1",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match2",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine: maotesting.NewMachine("test", "node1"),
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: namespace,
						Name:      "match1",
					},
				},
				{
					NamespacedName: client.ObjectKey{
						Namespace: namespace,
						Name:      "match2",
					},
				},
			},
		},
		{
			testCase: "no match",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "noMatch1",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "noMatch2",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine:          maotesting.NewMachine("test", "node1"),
			expectedRequests: nil,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			var objects []runtime.Object
			objects = append(objects, runtime.Object(tc.machine))
			for i := range tc.mhcs {
				objects = append(objects, runtime.Object(tc.mhcs[i]))
			}

			o := handler.MapObject{
				Meta:   tc.machine.GetObjectMeta(),
				Object: tc.machine,
			}
			requests := newFakeReconciler(objects...).mhcRequestsFromMachine(o)
			if !reflect.DeepEqual(requests, tc.expectedRequests) {
				t.Errorf("Expected: %v, got: %v", tc.expectedRequests, requests)
			}
		})
	}
}

func TestMHCRequestsFromNode(t *testing.T) {
	testCases := []struct {
		testCase         string
		mhcs             []*mapiv1beta1.MachineHealthCheck
		node             *corev1.Node
		machine          *mapiv1beta1.Machine
		expectedRequests []reconcile.Request
	}{
		{
			testCase: "at least one match",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "noMatch",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine: maotesting.NewMachine("fakeMachine", "node1"),
			node:    maotesting.NewNode("node1", true),
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: namespace,
						Name:      "match",
					},
				},
			},
		},
		{
			testCase: "more than one match",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match1",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "match2",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine: maotesting.NewMachine("fakeMachine", "node1"),
			node:    maotesting.NewNode("node1", true),
			expectedRequests: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: namespace,
						Name:      "match1",
					},
				},
				{
					NamespacedName: client.ObjectKey{
						Namespace: namespace,
						Name:      "match2",
					},
				},
			},
		},
		{
			testCase: "no match",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "noMatch1",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "noMatch2",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine:          maotesting.NewMachine("fakeMachine", "node1"),
			node:             maotesting.NewNode("node1", true),
			expectedRequests: nil,
		},
		{
			testCase: "node has bad machine annotation",
			mhcs: []*mapiv1beta1.MachineHealthCheck{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "mhc1",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"no": "match",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			machine:          maotesting.NewMachine("noNodeAnnotation", "node1"),
			node:             maotesting.NewNode("node1", true),
			expectedRequests: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			var objects []runtime.Object
			objects = append(objects, runtime.Object(tc.machine), runtime.Object(tc.node))
			for i := range tc.mhcs {
				objects = append(objects, runtime.Object(tc.mhcs[i]))
			}

			o := handler.MapObject{
				Meta:   tc.node.GetObjectMeta(),
				Object: tc.node,
			}
			requests := newFakeReconciler(objects...).mhcRequestsFromNode(o)
			if !reflect.DeepEqual(requests, tc.expectedRequests) {
				t.Errorf("Expected: %v, got: %v", tc.expectedRequests, requests)
			}
		})
	}
}

func TestGetMachinesFromMHC(t *testing.T) {
	machines := []mapiv1beta1.Machine{
		*maotesting.NewMachine("test1", "node1"),
		*maotesting.NewMachine("test2", "node2"),
	}
	testCases := []struct {
		testCase         string
		mhc              *mapiv1beta1.MachineHealthCheck
		machines         []mapiv1beta1.Machine
		expectedMachines []mapiv1beta1.Machine
		expectedError    bool
	}{
		{
			testCase:         "at least one match",
			mhc:              maotesting.NewMachineHealthCheck("foobar"),
			machines:         machines,
			expectedMachines: machines,
		},
		{
			testCase: "no match",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "match",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"dont": "match",
						},
					},
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{},
			},
			machines:         machines,
			expectedMachines: nil,
		},
		{
			testCase: "bad selector",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "match",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"bad selector": "''",
						},
					},
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{},
			},
			machines:         machines,
			expectedMachines: nil,
			expectedError:    true,
		},
	}

	for _, tc := range testCases {
		var objects []runtime.Object
		objects = append(objects, runtime.Object(tc.mhc))
		for i := range tc.machines {
			objects = append(objects, runtime.Object(&tc.machines[i]))
		}

		t.Run(tc.testCase, func(t *testing.T) {
			got, err := newFakeReconciler(objects...).getMachinesFromMHC(*tc.mhc)
			if !equality.Semantic.DeepEqual(got, tc.expectedMachines) {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, got, tc.expectedMachines)
			}
			if tc.expectedError != (err != nil) {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, err, tc.expectedError)
			}
		})
	}
}

func TestGetTargetsFromMHC(t *testing.T) {
	machine1 := maotesting.NewMachine("match1", "node1")
	machine2 := maotesting.NewMachine("match2", "node2")
	mhc := maotesting.NewMachineHealthCheck("findTargets")
	testCases := []struct {
		testCase        string
		mhc             *mapiv1beta1.MachineHealthCheck
		machines        []*mapiv1beta1.Machine
		nodes           []*corev1.Node
		expectedTargets []target
		expectedError   bool
	}{
		{
			testCase: "at least one match",
			mhc:      mhc,
			machines: []*mapiv1beta1.Machine{
				machine1,
				{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "noMatch",
						Namespace:       namespace,
						Labels:          map[string]string{"no": "match"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec: mapiv1beta1.MachineSpec{},
				},
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match1"),
						},
						Labels: map[string]string{},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node2",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match2"),
						},
						Labels: map[string]string{},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{},
					},
				},
			},
			expectedTargets: []target{
				{
					MHC:     *mhc,
					Machine: *machine1,
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node1",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match1"),
							},
							Labels: map[string]string{},
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{},
						},
					},
				},
			},
		},
		{
			testCase: "more than one match",
			mhc:      mhc,
			machines: []*mapiv1beta1.Machine{
				machine1,
				machine2,
			},
			nodes: []*corev1.Node{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node1",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match1"),
						},
						Labels: map[string]string{},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node2",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match2"),
						},
						Labels: map[string]string{},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{},
					},
				},
			},
			expectedTargets: []target{
				{
					MHC:     *mhc,
					Machine: *machine1,
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node1",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match1"),
							},
							Labels: map[string]string{},
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{},
						},
					},
				},
				{
					MHC:     *mhc,
					Machine: *machine2,
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node2",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "match2"),
							},
							Labels: map[string]string{},
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{},
						},
					},
				},
			},
		},
		{
			testCase: "machine has no node",
			mhc:      mhc,
			machines: []*mapiv1beta1.Machine{
				{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "noNodeRef",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec:   mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{},
				},
			},
			nodes: []*corev1.Node{},
			expectedTargets: []target{
				{
					MHC: *mhc,
					Machine: mapiv1beta1.Machine{
						TypeMeta: metav1.TypeMeta{Kind: "Machine"},
						ObjectMeta: metav1.ObjectMeta{
							Annotations:     make(map[string]string),
							Name:            "noNodeRef",
							Namespace:       namespace,
							Labels:          map[string]string{"foo": "bar"},
							OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						},
						Spec:   mapiv1beta1.MachineSpec{},
						Status: mapiv1beta1.MachineStatus{},
					},
					Node: nil,
				},
			},
		},
		{
			testCase: "node not found",
			mhc:      mhc,
			machines: []*mapiv1beta1.Machine{
				machine1,
			},
			nodes: []*corev1.Node{},
			expectedTargets: []target{
				{
					MHC:     *mhc,
					Machine: *machine1,
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node1",
							Namespace: metav1.NamespaceNone,
						},
					},
				},
			},
		},
	}
	for _, tc := range testCases {
		var objects []runtime.Object
		objects = append(objects, runtime.Object(tc.mhc))
		for i := range tc.machines {
			objects = append(objects, runtime.Object(tc.machines[i]))
		}
		for i := range tc.nodes {
			objects = append(objects, runtime.Object(tc.nodes[i]))
		}

		t.Run(tc.testCase, func(t *testing.T) {
			got, err := newFakeReconciler(objects...).getTargetsFromMHC(*tc.mhc)
			if !equality.Semantic.DeepEqual(got, tc.expectedTargets) {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, got, tc.expectedTargets)
			}
			if tc.expectedError != (err != nil) {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, err, tc.expectedError)
			}
		})
	}
}

func TestGetNodeFromMachine(t *testing.T) {
	testCases := []struct {
		testCase      string
		machine       *mapiv1beta1.Machine
		node          *corev1.Node
		expectedNode  *corev1.Node
		expectedError bool
	}{
		{
			testCase: "match",
			machine: &mapiv1beta1.Machine{
				TypeMeta: metav1.TypeMeta{Kind: "Machine"},
				ObjectMeta: metav1.ObjectMeta{
					Annotations:     make(map[string]string),
					Name:            "machine",
					Namespace:       namespace,
					Labels:          map[string]string{"foo": "bar"},
					OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
				},
				Spec: mapiv1beta1.MachineSpec{},
				Status: mapiv1beta1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name:      "node",
						Namespace: metav1.NamespaceNone,
					},
				},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node",
					Namespace: metav1.NamespaceNone,
					Annotations: map[string]string{
						machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
					},
					Labels: map[string]string{},
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "Node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expectedNode: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node",
					Namespace: metav1.NamespaceNone,
					Annotations: map[string]string{
						machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
					},
					Labels: map[string]string{},
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "Node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expectedError: false,
		},
		{
			testCase: "no nodeRef",
			machine: &mapiv1beta1.Machine{
				TypeMeta: metav1.TypeMeta{Kind: "Machine"},
				ObjectMeta: metav1.ObjectMeta{
					Annotations:     make(map[string]string),
					Name:            "machine",
					Namespace:       namespace,
					Labels:          map[string]string{"foo": "bar"},
					OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
				},
				Spec:   mapiv1beta1.MachineSpec{},
				Status: mapiv1beta1.MachineStatus{},
			},
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "node",
					Namespace: metav1.NamespaceNone,
					Annotations: map[string]string{
						machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
					},
					Labels: map[string]string{},
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "Node",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{},
				},
			},
			expectedNode:  nil,
			expectedError: false,
		},
		{
			testCase: "node not found",
			machine: &mapiv1beta1.Machine{
				TypeMeta: metav1.TypeMeta{Kind: "Machine"},
				ObjectMeta: metav1.ObjectMeta{
					Annotations:     make(map[string]string),
					Name:            "machine",
					Namespace:       namespace,
					Labels:          map[string]string{"foo": "bar"},
					OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
				},
				Spec: mapiv1beta1.MachineSpec{},
				Status: mapiv1beta1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name:      "nonExistingNode",
						Namespace: metav1.NamespaceNone,
					},
				},
			},
			node:          maotesting.NewNode("anyNode", true),
			expectedNode:  &corev1.Node{},
			expectedError: true,
		},
	}
	for _, tc := range testCases {
		var objects []runtime.Object
		objects = append(objects, runtime.Object(tc.machine), runtime.Object(tc.node))
		t.Run(tc.testCase, func(t *testing.T) {
			got, err := newFakeReconciler(objects...).getNodeFromMachine(*tc.machine)
			if !equality.Semantic.DeepEqual(got, tc.expectedNode) {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, got, tc.expectedNode)
			}
			if tc.expectedError != (err != nil) {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, err, tc.expectedError)
			}
		})
	}
}

func TestNeedsRemediation(t *testing.T) {
	knownDate := metav1.Time{Time: time.Date(1985, 06, 03, 0, 0, 0, 0, time.Local)}
	machineFailed := machinePhaseFailed
	testCases := []struct {
		testCase                 string
		target                   *target
		expectedNeedsRemediation bool
		expectedNextCheck        time.Duration
		expectedError            bool
	}{
		{
			testCase: "healthy: does not met conditions criteria",
			target: &target{
				Machine: *maotesting.NewMachine("test", "node"),
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{},
						UID:    "uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:               corev1.NodeReady,
								Status:             corev1.ConditionTrue,
								LastTransitionTime: knownDate,
							},
						},
					},
				},
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "300s",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: false,
			expectedNextCheck:        time.Duration(0),
			expectedError:            false,
		},
		{
			testCase: "unhealthy: node does not exist",
			target: &target{
				Machine: *maotesting.NewMachine("test", "node"),
				Node:    &corev1.Node{},
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "300s",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: true,
			expectedNextCheck:        time.Duration(0),
			expectedError:            false,
		},
		{
			testCase: "unhealthy: nodeRef nil longer than timeout",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "machine",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec: mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{
						LastUpdated: &metav1.Time{Time: time.Now().Add(time.Duration(-timeoutForMachineToHaveNode) - 1*time.Second)},
					},
				},
				Node: nil,
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "300s",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: true,
			expectedNextCheck:        time.Duration(0),
			expectedError:            false,
		},
		{
			testCase: "unhealthy: meet conditions criteria",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "machine",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec: mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{
						LastUpdated: &metav1.Time{Time: time.Now().Add(time.Duration(-timeoutForMachineToHaveNode) - 1*time.Second)},
					},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{},
						UID:    "uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:               corev1.NodeReady,
								Status:             corev1.ConditionFalse,
								LastTransitionTime: metav1.Time{Time: time.Now().Add(time.Duration(-400) * time.Second)},
							},
						},
					},
				},
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "300s",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: true,
			expectedNextCheck:        time.Duration(0),
			expectedError:            false,
		},
		{
			testCase: "unhealthy: machine phase failed",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "machine",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec: mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{
						Phase: &machineFailed,
					},
				},
				Node: nil,
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "300s",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: true,
			expectedNextCheck:        time.Duration(0),
			expectedError:            false,
		},
		{
			testCase: "healthy: meet conditions criteria but timeout",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "machine",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec: mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{
						LastUpdated: &metav1.Time{Time: time.Now().Add(time.Duration(-timeoutForMachineToHaveNode) - 1*time.Second)},
					},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{},
						UID:    "uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:               corev1.NodeReady,
								Status:             corev1.ConditionFalse,
								LastTransitionTime: metav1.Time{Time: time.Now().Add(time.Duration(-200) * time.Second)},
							},
						},
					},
				},
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "300s",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: false,
			expectedNextCheck:        time.Duration(1 * time.Minute), // 300-200 rounded
			expectedError:            false,
		},
		{
			testCase: "error bad conditions timeout",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "machine",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec: mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{
						LastUpdated: &metav1.Time{Time: time.Now().Add(time.Duration(-timeoutForMachineToHaveNode) - 1*time.Second)},
					},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "node",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{},
						UID:    "uid",
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{
								Type:               corev1.NodeReady,
								Status:             corev1.ConditionFalse,
								LastTransitionTime: metav1.Time{Time: time.Now().Add(time.Duration(-200) * time.Second)},
							},
						},
					},
				},
				MHC: mapiv1beta1.MachineHealthCheck{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: namespace,
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "MachineHealthCheck",
					},
					Spec: mapiv1beta1.MachineHealthCheckSpec{
						Selector: metav1.LabelSelector{
							MatchLabels: map[string]string{
								"foo": "bar",
							},
						},
						UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
							{
								Type:    "Ready",
								Status:  "Unknown",
								Timeout: "badTimeout",
							},
							{
								Type:    "Ready",
								Status:  "False",
								Timeout: "300s",
							},
						},
					},
					Status: mapiv1beta1.MachineHealthCheckStatus{},
				},
			},
			expectedNeedsRemediation: false,
			expectedNextCheck:        time.Duration(0),
			expectedError:            true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			needsRemediation, nextCheck, err := tc.target.needsRemediation()
			if needsRemediation != tc.expectedNeedsRemediation {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, needsRemediation, tc.expectedNeedsRemediation)
			}
			if tc.expectedNextCheck == time.Duration(0) {
				if nextCheck != tc.expectedNextCheck {
					t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, int(nextCheck), int(tc.expectedNextCheck))
				}
			}
			if tc.expectedNextCheck != time.Duration(0) {
				now := time.Now()
				// since isUnhealthy will check timeout against now() again, the nextCheck must be slightly lower to the
				// margin calculated here
				if now.Add(nextCheck).Before(now.Add(tc.expectedNextCheck)) {
					t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, nextCheck, tc.expectedNextCheck)
				}
			}
			if tc.expectedError != (err != nil) {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, err, tc.expectedError)
			}
		})
	}
}

func TestIsMaster(t *testing.T) {
	testCases := []struct {
		testCase string
		target   *target
		expected bool
	}{
		{
			testCase: "no master",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "test",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec:   mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{},
				},
				MHC: mapiv1beta1.MachineHealthCheck{},
			},
		},
		{
			testCase: "node master",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "test",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec:   mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{
							nodeMasterLabel: "",
						},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{},
				},
				MHC: mapiv1beta1.MachineHealthCheck{},
			},
			expected: true,
		},
		{
			testCase: "machine master",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations: make(map[string]string),
						Name:        "test",
						Namespace:   namespace,
						Labels: map[string]string{
							machineRoleLabel: machineMasterRole,
						},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec:   mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{},
				},
				Node: &corev1.Node{},
				MHC:  mapiv1beta1.MachineHealthCheck{},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			if got := tc.target.isMaster(); got != tc.expected {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, got, tc.expected)
			}
		})
	}
}

func TestMinDuration(t *testing.T) {
	testCases := []struct {
		testCase  string
		durations []time.Duration
		expected  time.Duration
	}{
		{
			testCase: "empty slice",
			expected: time.Duration(0),
		},
		{
			testCase: "find min",
			durations: []time.Duration{
				time.Duration(1),
				time.Duration(2),
				time.Duration(3),
			},
			expected: time.Duration(1),
		},
	}
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			if got := minDuration(tc.durations); got != tc.expected {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, got, tc.expected)
			}
		})
	}
}

func TestStringPointerDeref(t *testing.T) {
	value := "test"
	testCases := []struct {
		stringPointer *string
		expected      string
	}{
		{
			stringPointer: nil,
			expected:      "",
		},
		{
			stringPointer: &value,
			expected:      value,
		},
	}
	for _, tc := range testCases {
		if got := derefStringPointer(tc.stringPointer); got != tc.expected {
			t.Errorf("Got: %v, expected: %v", got, tc.expected)
		}
	}
}

func TestRemediate(t *testing.T) {
	testCases := []struct {
		testCase      string
		target        *target
		expectedError bool
		deletion      bool
	}{
		{
			testCase: "no master",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "test",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec:   mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{},
				},
				MHC: mapiv1beta1.MachineHealthCheck{},
			},
			deletion:      true,
			expectedError: false,
		},
		{
			testCase: "node master",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations:     make(map[string]string),
						Name:            "test",
						Namespace:       namespace,
						Labels:          map[string]string{"foo": "bar"},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						UID:             "uid",
					},
					//Spec:   mapiv1beta1.MachineSpec{},
					//Status: mapiv1beta1.MachineStatus{},
				},
				Node: &corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test",
						Namespace: metav1.NamespaceNone,
						Annotations: map[string]string{
							machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
						},
						Labels: map[string]string{
							nodeMasterLabel: "",
						},
					},
					TypeMeta: metav1.TypeMeta{
						Kind: "Node",
					},
					Status: corev1.NodeStatus{},
				},
				MHC: mapiv1beta1.MachineHealthCheck{},
			},
			deletion:      false,
			expectedError: false,
		},
		{
			testCase: "machine master",
			target: &target{
				Machine: mapiv1beta1.Machine{
					TypeMeta: metav1.TypeMeta{Kind: "Machine"},
					ObjectMeta: metav1.ObjectMeta{
						Annotations: make(map[string]string),
						Name:        "test",
						Namespace:   namespace,
						Labels: map[string]string{
							machineRoleLabel: machineMasterRole,
						},
						OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
					},
					Spec:   mapiv1beta1.MachineSpec{},
					Status: mapiv1beta1.MachineStatus{},
				},
				Node: &corev1.Node{},
				MHC:  mapiv1beta1.MachineHealthCheck{},
			},
			deletion:      false,
			expectedError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			var objects []runtime.Object
			objects = append(objects, runtime.Object(&tc.target.Machine))
			r := newFakeReconciler(objects...)
			if err := tc.target.remediate(r); (err != nil) != tc.expectedError {
				t.Errorf("Case: %v. Got: %v, expected error: %v", tc.testCase, err, tc.expectedError)
			}
			machine := &mapiv1beta1.Machine{}
			err := r.client.Get(context.TODO(), namespacedName(machine), machine)
			if tc.deletion {
				if err != nil {
					if !errors.IsNotFound(err) {
						t.Errorf("Expected not found error, got: %v", err)
					}
				} else {
					t.Errorf("Expected not found error while getting remediated machine")
				}
			}
			if !tc.deletion {
				if !equality.Semantic.DeepEqual(*machine, tc.target.Machine) {
					t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, *machine, tc.target.Machine)
				}
			}
		})
	}
}

func TestReconcileStatus(t *testing.T) {
	testCases := []struct {
		testCase       string
		mhc            *mapiv1beta1.MachineHealthCheck
		totalTargets   int
		currentHealthy int
	}{
		{
			testCase: "status gets new values",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector: metav1.LabelSelector{},
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{},
			},
			totalTargets:   10,
			currentHealthy: 5,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			var objects []runtime.Object
			objects = append(objects, runtime.Object(tc.mhc))
			r := newFakeReconciler(objects...)

			if err := r.reconcileStatus(tc.mhc, tc.totalTargets, tc.currentHealthy); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			mhc := &mapiv1beta1.MachineHealthCheck{}
			if err := r.client.Get(context.TODO(), namespacedName(tc.mhc), mhc); err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if *mhc.Status.ExpectedMachines != tc.totalTargets {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, mhc.Status.ExpectedMachines, tc.totalTargets)
			}
			if *mhc.Status.CurrentHealthy != tc.currentHealthy {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, mhc.Status.CurrentHealthy, tc.currentHealthy)
			}
		})
	}
}

func TestHealthCheckTargets(t *testing.T) {
	now := time.Now()
	testCases := []struct {
		testCase               string
		targets                []target
		currentHealthy         int
		needRemediationTargets []target
		nextCheckTimesLen      int
		errList                []error
	}{
		{
			testCase: "one healthy, one unhealthy",
			targets: []target{
				{
					Machine: mapiv1beta1.Machine{
						TypeMeta: metav1.TypeMeta{Kind: "Machine"},
						ObjectMeta: metav1.ObjectMeta{
							Annotations:     make(map[string]string),
							Name:            "machine",
							Namespace:       namespace,
							Labels:          map[string]string{"foo": "bar"},
							OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						},
						Spec:   mapiv1beta1.MachineSpec{},
						Status: mapiv1beta1.MachineStatus{},
					},
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
							},
							Labels: map[string]string{},
							UID:    "uid",
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:               corev1.NodeReady,
									Status:             corev1.ConditionTrue,
									LastTransitionTime: metav1.Time{},
								},
							},
						},
					},
					MHC: mapiv1beta1.MachineHealthCheck{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: namespace,
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "MachineHealthCheck",
						},
						Spec: mapiv1beta1.MachineHealthCheckSpec{
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
								{
									Type:    "Ready",
									Status:  "Unknown",
									Timeout: "300s",
								},
								{
									Type:    "Ready",
									Status:  "False",
									Timeout: "300s",
								},
							},
						},
						Status: mapiv1beta1.MachineHealthCheckStatus{},
					},
				},
				{
					Machine: mapiv1beta1.Machine{
						TypeMeta: metav1.TypeMeta{Kind: "Machine"},
						ObjectMeta: metav1.ObjectMeta{
							Annotations:     make(map[string]string),
							Name:            "machine",
							Namespace:       namespace,
							Labels:          map[string]string{"foo": "bar"},
							OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						},
						Spec:   mapiv1beta1.MachineSpec{},
						Status: mapiv1beta1.MachineStatus{},
					},
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
							},
							Labels: map[string]string{},
							UID:    "uid",
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:               corev1.NodeReady,
									Status:             corev1.ConditionFalse,
									LastTransitionTime: metav1.Time{Time: now.Add(time.Duration(-400) * time.Second)},
								},
							},
						},
					},
					MHC: mapiv1beta1.MachineHealthCheck{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: namespace,
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "MachineHealthCheck",
						},
						Spec: mapiv1beta1.MachineHealthCheckSpec{
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
								{
									Type:    "Ready",
									Status:  "Unknown",
									Timeout: "300s",
								},
								{
									Type:    "Ready",
									Status:  "False",
									Timeout: "300s",
								},
							},
						},
						Status: mapiv1beta1.MachineHealthCheckStatus{},
					},
				},
			},
			currentHealthy: 1,
			needRemediationTargets: []target{
				{
					Machine: mapiv1beta1.Machine{
						TypeMeta: metav1.TypeMeta{Kind: "Machine"},
						ObjectMeta: metav1.ObjectMeta{
							Annotations:     make(map[string]string),
							Name:            "machine",
							Namespace:       namespace,
							Labels:          map[string]string{"foo": "bar"},
							OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						},
						Spec:   mapiv1beta1.MachineSpec{},
						Status: mapiv1beta1.MachineStatus{},
					},
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
							},
							Labels: map[string]string{},
							UID:    "uid",
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:               corev1.NodeReady,
									Status:             corev1.ConditionFalse,
									LastTransitionTime: metav1.Time{Time: now.Add(time.Duration(-400) * time.Second)},
								},
							},
						},
					},
					MHC: mapiv1beta1.MachineHealthCheck{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: namespace,
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "MachineHealthCheck",
						},
						Spec: mapiv1beta1.MachineHealthCheckSpec{
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
								{
									Type:    "Ready",
									Status:  "Unknown",
									Timeout: "300s",
								},
								{
									Type:    "Ready",
									Status:  "False",
									Timeout: "300s",
								},
							},
						},
						Status: mapiv1beta1.MachineHealthCheckStatus{},
					},
				},
			},
			nextCheckTimesLen: 0,
			errList:           []error{},
		},
		{
			testCase: "two checkTimes",
			targets: []target{
				{
					Machine: mapiv1beta1.Machine{
						TypeMeta: metav1.TypeMeta{Kind: "Machine"},
						ObjectMeta: metav1.ObjectMeta{
							Annotations:     make(map[string]string),
							Name:            "machine",
							Namespace:       namespace,
							Labels:          map[string]string{"foo": "bar"},
							OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						},
						Spec: mapiv1beta1.MachineSpec{},
						Status: mapiv1beta1.MachineStatus{
							LastUpdated: &metav1.Time{Time: now.Add(time.Duration(-timeoutForMachineToHaveNode) + 1*time.Minute)},
						},
					},
					Node: nil,
					MHC: mapiv1beta1.MachineHealthCheck{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: namespace,
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "MachineHealthCheck",
						},
						Spec: mapiv1beta1.MachineHealthCheckSpec{
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
								{
									Type:    "Ready",
									Status:  "Unknown",
									Timeout: "300s",
								},
								{
									Type:    "Ready",
									Status:  "False",
									Timeout: "300s",
								},
							},
						},
						Status: mapiv1beta1.MachineHealthCheckStatus{},
					},
				},
				{
					Machine: mapiv1beta1.Machine{
						TypeMeta: metav1.TypeMeta{Kind: "Machine"},
						ObjectMeta: metav1.ObjectMeta{
							Annotations:     make(map[string]string),
							Name:            "machine",
							Namespace:       namespace,
							Labels:          map[string]string{"foo": "bar"},
							OwnerReferences: []metav1.OwnerReference{{Kind: "MachineSet"}},
						},
						Spec:   mapiv1beta1.MachineSpec{},
						Status: mapiv1beta1.MachineStatus{},
					},
					Node: &corev1.Node{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "node",
							Namespace: metav1.NamespaceNone,
							Annotations: map[string]string{
								machineAnnotationKey: fmt.Sprintf("%s/%s", namespace, "machine"),
							},
							Labels: map[string]string{},
							UID:    "uid",
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "Node",
						},
						Status: corev1.NodeStatus{
							Conditions: []corev1.NodeCondition{
								{
									Type:               corev1.NodeReady,
									Status:             corev1.ConditionFalse,
									LastTransitionTime: metav1.Time{Time: now.Add(time.Duration(-200) * time.Second)},
								},
							},
						},
					},
					MHC: mapiv1beta1.MachineHealthCheck{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test",
							Namespace: namespace,
						},
						TypeMeta: metav1.TypeMeta{
							Kind: "MachineHealthCheck",
						},
						Spec: mapiv1beta1.MachineHealthCheckSpec{
							Selector: metav1.LabelSelector{
								MatchLabels: map[string]string{
									"foo": "bar",
								},
							},
							UnhealthyConditions: []mapiv1beta1.UnhealthyCondition{
								{
									Type:    "Ready",
									Status:  "Unknown",
									Timeout: "300s",
								},
								{
									Type:    "Ready",
									Status:  "False",
									Timeout: "300s",
								},
							},
						},
						Status: mapiv1beta1.MachineHealthCheckStatus{},
					},
				},
			},
			currentHealthy:    0,
			nextCheckTimesLen: 2,
			errList:           []error{},
		},
	}
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			currentHealhty, needRemediationTargets, nextCheckTimes, errList := healthCheckTargets(tc.targets)
			if currentHealhty != tc.currentHealthy {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, currentHealhty, tc.currentHealthy)
			}
			if !equality.Semantic.DeepEqual(needRemediationTargets, tc.needRemediationTargets) {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, needRemediationTargets, tc.needRemediationTargets)
			}
			if len(nextCheckTimes) != tc.nextCheckTimesLen {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, len(nextCheckTimes), tc.nextCheckTimesLen)
			}
			if !equality.Semantic.DeepEqual(errList, tc.errList) {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, errList, tc.errList)
			}
		})
	}
}

func TestIsAllowedRemediation(t *testing.T) {
	// short circuit if ever more than 2 out of 5 go unhealthy
	maxUnhealthyInt := intstr.FromInt(2)
	maxUnhealthyString := intstr.FromString("40%")

	testCases := []struct {
		testCase string
		mhc      *mapiv1beta1.MachineHealthCheck
		expected bool
	}{
		{
			testCase: "not above maxUnhealthy",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector:     metav1.LabelSelector{},
					MaxUnhealthy: &maxUnhealthyInt,
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{
					ExpectedMachines: IntPtr(5),
					CurrentHealthy:   IntPtr(3),
				},
			},
			expected: true,
		},
		{
			testCase: "above maxUnhealthy",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector:     metav1.LabelSelector{},
					MaxUnhealthy: &maxUnhealthyInt,
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{
					ExpectedMachines: IntPtr(5),
					CurrentHealthy:   IntPtr(2),
				},
			},
			expected: false,
		},
		{
			testCase: "not above maxUnhealthy (percentange)",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector:     metav1.LabelSelector{},
					MaxUnhealthy: &maxUnhealthyString,
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{
					ExpectedMachines: IntPtr(5),
					CurrentHealthy:   IntPtr(3),
				},
			},
			expected: true,
		},
		{
			testCase: "above maxUnhealthy (percentange)",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector:     metav1.LabelSelector{},
					MaxUnhealthy: &maxUnhealthyString,
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{
					ExpectedMachines: IntPtr(5),
					CurrentHealthy:   IntPtr(2),
				},
			},
			expected: false,
		},
		{
			testCase: "nil values",
			mhc: &mapiv1beta1.MachineHealthCheck{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: namespace,
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "MachineHealthCheck",
				},
				Spec: mapiv1beta1.MachineHealthCheckSpec{
					Selector:     metav1.LabelSelector{},
					MaxUnhealthy: &maxUnhealthyString,
				},
				Status: mapiv1beta1.MachineHealthCheckStatus{
					ExpectedMachines: nil,
					CurrentHealthy:   nil,
				},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			if got := isAllowedRemediation(tc.mhc); got != tc.expected {
				t.Errorf("Case: %v. Got: %v, expected: %v", tc.testCase, got, tc.expected)
			}
		})
	}
}

func IntPtr(i int) *int {
	return &i
}
