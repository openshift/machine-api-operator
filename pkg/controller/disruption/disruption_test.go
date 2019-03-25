package disruption

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const namespace = "openshift-machine-api"

var knownDate = metav1.Time{Time: time.Date(1985, 06, 03, 0, 0, 0, 0, time.Local)}

func init() {
	// Add types to scheme
	mapiv1.AddToScheme(scheme.Scheme)
	healthcheckingv1alpha1.AddToScheme(scheme.Scheme)
}

// newFakeReconciler returns a new reconcile.Reconciler with a fake client
func newFakeReconciler(recorder record.EventRecorder, initObjects ...runtime.Object) *ReconcileMachineDisruption {
	fakeClient := fake.NewFakeClient(initObjects...)
	return &ReconcileMachineDisruption{
		client:    fakeClient,
		recorder:  recorder,
		scheme:    scheme.Scheme,
		namespace: namespace,
	}
}

func fooBar() map[string]string {
	return map[string]string{"foo": "bar"}
}

func newSelector(labels map[string]string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: labels}
}

func newSelectorFooBar() *metav1.LabelSelector {
	return newSelector(fooBar())
}

func newMinAvailableMachineDisruptionBudget(minAvailable int32) *healthcheckingv1alpha1.MachineDisruptionBudget {
	return &healthcheckingv1alpha1.MachineDisruptionBudget{
		TypeMeta: metav1.TypeMeta{Kind: "MachineDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foobar",
			Namespace: namespace,
		},
		Spec: healthcheckingv1alpha1.MachineDisruptionBudgetSpec{
			MinAvailable: &minAvailable,
			Selector:     newSelectorFooBar(),
		},
	}
}

func newMaxUnavailableMachineDisruptionBudget(maxUnavailable int32) *healthcheckingv1alpha1.MachineDisruptionBudget {
	return &healthcheckingv1alpha1.MachineDisruptionBudget{
		TypeMeta: metav1.TypeMeta{Kind: "MachineDisruptionBudget"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foobar",
			Namespace: namespace,
		},
		Spec: healthcheckingv1alpha1.MachineDisruptionBudgetSpec{
			MaxUnavailable: &maxUnavailable,
			Selector:       newSelectorFooBar(),
		},
	}
}

func updateMachineOwnerToMachineSet(machine *mapiv1.Machine, ms *mapiv1.MachineSet) {
	var controllerReference metav1.OwnerReference
	var trueVar = true
	controllerReference = metav1.OwnerReference{
		UID:        ms.UID,
		APIVersion: controllerKindMachineSet.GroupVersion().String(),
		Kind:       controllerKindMachineSet.Kind,
		Name:       ms.Name,
		Controller: &trueVar,
	}
	machine.OwnerReferences = append(machine.OwnerReferences, controllerReference)
}

func newNode(name string, ready bool) *v1.Node {
	nodeReadyStatus := v1.ConditionTrue
	if !ready {
		nodeReadyStatus = v1.ConditionUnknown
	}

	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceNone,
			Annotations: map[string]string{
				"machine": fmt.Sprintf("%s/%s", namespace, "fakeMachine"),
			},
			Labels: map[string]string{},
			UID:    uuid.NewUUID(),
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		Status: v1.NodeStatus{
			Conditions: []v1.NodeCondition{
				{
					Type:               v1.NodeReady,
					Status:             nodeReadyStatus,
					LastTransitionTime: knownDate,
				},
			},
		},
	}
}

func newMachine(name string, nodeName string, phase string) *mapiv1.Machine {
	return &mapiv1.Machine{
		TypeMeta: metav1.TypeMeta{Kind: "Machine"},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: make(map[string]string),
			Name:        name,
			Namespace:   namespace,
			Labels:      fooBar(),
			UID:         uuid.NewUUID(),
		},
		Spec: mapiv1.MachineSpec{},
		Status: mapiv1.MachineStatus{
			NodeRef: &v1.ObjectReference{
				Name: nodeName,
			},
			Phase: &phase,
		},
	}
}

func newMachineSet(name string, size int32) *mapiv1.MachineSet {
	return &mapiv1.MachineSet{
		TypeMeta: metav1.TypeMeta{Kind: "MachineSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    fooBar(),
			UID:       uuid.NewUUID(),
		},
		Spec: mapiv1.MachineSetSpec{
			Replicas: &size,
			Selector: *newSelectorFooBar(),
		},
	}
}

func updateMachineSetOwnerToMachineDeployment(ms *mapiv1.MachineSet, md *mapiv1.MachineDeployment) {
	var controllerReference metav1.OwnerReference
	var trueVar = true
	controllerReference = metav1.OwnerReference{
		UID:        md.UID,
		APIVersion: controllerKindMachineDeployment.GroupVersion().String(),
		Kind:       controllerKindMachineDeployment.Kind,
		Name:       md.Name,
		Controller: &trueVar,
	}
	ms.OwnerReferences = append(ms.OwnerReferences, controllerReference)
}

func newMachineDeployment(name string, size int32) *mapiv1.MachineDeployment {
	return &mapiv1.MachineDeployment{
		TypeMeta: metav1.TypeMeta{Kind: "MachineDeployment"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    fooBar(),
			UID:       uuid.NewUUID(),
		},
		Spec: mapiv1.MachineDeploymentSpec{
			Replicas: &size,
			Selector: *newSelectorFooBar(),
		},
	}
}

type expectedMachineCount struct {
	count   int32
	healthy int32
}

func TestGetExpectedMachineCount(t *testing.T) {
	mdbMinAvailable := newMinAvailableMachineDisruptionBudget(1)
	mdbMaxUnavailable := newMaxUnavailableMachineDisruptionBudget(1)

	node := newNode("node", true)

	// will check the expected result when the machine does not owned by controller
	machine := newMachine("machine1", node.Name, "Running")

	// will check the expected result when the machine owned by MachineSet controller
	machineSet := newMachineSet("ms1", 3)
	machineControlledByMachineSet := newMachine("machine2", node.Name, "Running")
	updateMachineOwnerToMachineSet(machineControlledByMachineSet, machineSet)

	// will check the expected result when the machine owned by MachineDeployment controller
	machineSetControlledByDeployment := newMachineSet("ms2", 4)
	machineDeployment := newMachineDeployment("md1", 4)
	updateMachineSetOwnerToMachineDeployment(machineSetControlledByDeployment, machineDeployment)
	machineControlledByMachineDeployment := newMachine("machine3", node.Name, "Running")
	updateMachineOwnerToMachineSet(machineControlledByMachineDeployment, machineSetControlledByDeployment)

	testsCases := []struct {
		testName string
		mdb      *healthcheckingv1alpha1.MachineDisruptionBudget
		machines []mapiv1.Machine
		expected *expectedMachineCount
	}{
		{
			testName: "MDB with min available and machine without controller",
			mdb:      mdbMinAvailable,
			machines: []mapiv1.Machine{*machine},
			expected: &expectedMachineCount{
				count:   1,
				healthy: 1,
			},
		},
		{
			testName: "MDB with min available and machine controlled by machine set",
			mdb:      mdbMinAvailable,
			machines: []mapiv1.Machine{*machineControlledByMachineSet},
			expected: &expectedMachineCount{
				count:   1,
				healthy: 1,
			},
		},
		{
			testName: "MDB with min available and machine controlled by machine deployment",
			mdb:      mdbMinAvailable,
			machines: []mapiv1.Machine{*machineControlledByMachineDeployment},
			expected: &expectedMachineCount{
				count:   1,
				healthy: 1,
			},
		},
		{
			testName: "MDB with min available and two machines controlled by machine set and deployment",
			mdb:      mdbMinAvailable,
			machines: []mapiv1.Machine{
				*machineControlledByMachineSet,
				*machineControlledByMachineDeployment,
			},
			expected: &expectedMachineCount{
				count:   2,
				healthy: 1,
			},
		},
		{
			testName: "MDB with max unavailable and machine without controller",
			mdb:      mdbMaxUnavailable,
			machines: []mapiv1.Machine{*machine},
			expected: &expectedMachineCount{
				count:   1,
				healthy: 0,
			},
		},
		{
			testName: "MDB with max unavailable and machine controlled by machine set",
			mdb:      mdbMaxUnavailable,
			machines: []mapiv1.Machine{*machineControlledByMachineSet},
			expected: &expectedMachineCount{
				count:   3,
				healthy: 2,
			},
		},
		{
			testName: "MDB with max unavailable and machine controlled by machine deployment",
			mdb:      mdbMaxUnavailable,
			machines: []mapiv1.Machine{*machineControlledByMachineDeployment},
			expected: &expectedMachineCount{
				count:   4,
				healthy: 3,
			},
		},
		{
			testName: "MDB with max unavailable and two machines controlled by machine set and deployment",
			mdb:      mdbMaxUnavailable,
			machines: []mapiv1.Machine{
				*machineControlledByMachineSet,
				*machineControlledByMachineDeployment,
			},
			expected: &expectedMachineCount{
				count:   7,
				healthy: 6,
			},
		},
	}

	r := newFakeReconciler(
		nil,
		machineSet,
		machineSetControlledByDeployment,
		machineDeployment,
	)
	for _, tc := range testsCases {
		expectedCount, desiredHealthy := r.getExpectedMachineCount(tc.mdb, tc.machines)
		if expectedCount != tc.expected.count {
			t.Errorf("Test case: %v. Expected count: %v, got: %v", tc.testName, tc.expected.count, expectedCount)
		}

		if desiredHealthy != tc.expected.healthy {
			t.Errorf("Test case: %v. Expected healthy: %v, got: %v", tc.testName, tc.expected.healthy, desiredHealthy)
		}
	}
}

type expectedMachinesForMDB struct {
	machines []mapiv1.Machine
	error    bool
}

func TestGetMachinesForMachineDisruptionBudget(t *testing.T) {
	mdbWithSelector := newMinAvailableMachineDisruptionBudget(3)

	mdbWithoutSelector := newMinAvailableMachineDisruptionBudget(3)
	mdbWithoutSelector.Spec.Selector = nil

	mdbWithEmptySelector := newMinAvailableMachineDisruptionBudget(3)
	mdbWithEmptySelector.Spec.Selector = &metav1.LabelSelector{}

	mdbWithBadSelector := newMinAvailableMachineDisruptionBudget(1)
	mdbWithBadSelector.Spec.Selector = &metav1.LabelSelector{
		MatchLabels:      map[string]string{},
		MatchExpressions: []metav1.LabelSelectorRequirement{{Operator: "fake"}},
	}

	node := newNode("node", true)

	machineWithLabels1 := newMachine("machineWithLabels1", node.Name, "Running")
	machineWithLabels2 := newMachine("machineWithLabels2", node.Name, "Running")
	machineWithoutLabels := newMachine("machineWithoutLabels", node.Name, "Running")
	machineWithoutLabels.Labels = map[string]string{}

	testsCases := []struct {
		testName string
		mdb      *healthcheckingv1alpha1.MachineDisruptionBudget
		expected *expectedMachinesForMDB
	}{
		{
			testName: "machine disruption budget with selector",
			mdb:      mdbWithSelector,
			expected: &expectedMachinesForMDB{
				machines: []mapiv1.Machine{*machineWithLabels1, *machineWithLabels2},
				error:    false,
			},
		},
		{
			testName: "machine disruption budget without selector",
			mdb:      mdbWithoutSelector,
			expected: &expectedMachinesForMDB{
				machines: nil,
				error:    false,
			},
		},
		{
			testName: "machine disruption budget with empty selector",
			mdb:      mdbWithEmptySelector,
			expected: &expectedMachinesForMDB{
				machines: []mapiv1.Machine{},
				error:    false,
			},
		},
		{
			testName: "machine disruption budget with bad selector",
			mdb:      mdbWithBadSelector,
			expected: &expectedMachinesForMDB{
				machines: []mapiv1.Machine{},
				error:    true,
			},
		},
	}

	r := newFakeReconciler(
		nil,
		machineWithLabels1,
		machineWithLabels2,
		machineWithoutLabels,
	)
	for _, tc := range testsCases {
		machines, err := r.getMachinesForMachineDisruptionBudget(tc.mdb)

		if len(tc.expected.machines) != len(machines) {
			t.Errorf("Test case: %v. Expected number of machines: %v, got: %v", tc.testName, len(tc.expected.machines), len(machines))
		}
		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected %s error, got: %v", tc.testName, errorExpectation, err)
		}
	}
}

type expectedDisrupteMachines struct {
	machines    map[string]metav1.Time
	recheckTime *time.Time
}

func TestBuildDisruptedMachineMap(t *testing.T) {
	node := newNode("node", true)

	currentTime := metav1.NewTime(time.Now())
	timeAfterTwoMinutes := currentTime.Add(2 * time.Minute)
	timeBeforeThreeMinutes := metav1.NewTime(currentTime.Add(-3 * time.Minute))

	machine := newMachine("machine", node.Name, "Running")
	deletedMachine := newMachine("deletedMachine", node.Name, "Running")
	deletedMachine.DeletionTimestamp = &currentTime
	disruptedMachineBeforeTimeout := newMachine("disruptedMachineBeforeTimeout", node.Name, "Running")
	disruptedMachineAfterTimeout := newMachine("disruptedMachineAfterTimeout", node.Name, "Running")

	mdbWithDisruptedMachines := newMinAvailableMachineDisruptionBudget(1)
	mdbWithDisruptedMachines.Status.DisruptedMachines = map[string]metav1.Time{
		disruptedMachineBeforeTimeout.Name: timeBeforeThreeMinutes,
		disruptedMachineAfterTimeout.Name:  currentTime,
	}
	mdbWithoutDisruptedMachines := newMinAvailableMachineDisruptionBudget(1)

	testsCases := []struct {
		testName string
		mdb      *healthcheckingv1alpha1.MachineDisruptionBudget
		machines []mapiv1.Machine
		expected *expectedDisrupteMachines
	}{
		{
			testName: "MDB without disrupted machines",
			mdb:      mdbWithoutDisruptedMachines,
			machines: []mapiv1.Machine{*machine, *deletedMachine, *disruptedMachineBeforeTimeout, *disruptedMachineAfterTimeout},
			expected: &expectedDisrupteMachines{
				machines:    map[string]metav1.Time{},
				recheckTime: nil,
			},
		},
		{
			testName: "MDB with disrupted machines",
			mdb:      mdbWithDisruptedMachines,
			machines: []mapiv1.Machine{*machine, *deletedMachine, *disruptedMachineBeforeTimeout, *disruptedMachineAfterTimeout},
			expected: &expectedDisrupteMachines{
				machines: map[string]metav1.Time{
					disruptedMachineAfterTimeout.Name: currentTime,
				},
				recheckTime: &timeAfterTwoMinutes,
			},
		},
	}

	recorder := record.NewFakeRecorder(10)
	r := newFakeReconciler(recorder)
	for _, tc := range testsCases {
		disruptedMachines, recheckTime := r.buildDisruptedMachineMap(tc.machines, tc.mdb, currentTime.Time)

		if !reflect.DeepEqual(tc.expected.machines, disruptedMachines) {
			t.Errorf("Test case: %v. Expected machines: %v, got: %v", tc.testName, tc.expected.machines, disruptedMachines)
		}
		if tc.expected.recheckTime == nil {
			if recheckTime != nil {
				t.Errorf("Test case: %s. Expected %s recheckTime, got: %v", tc.testName, tc.expected.recheckTime, recheckTime)
			}
		} else if recheckTime == nil || !recheckTime.Equal(*tc.expected.recheckTime) {
			t.Errorf("Test case: %s. Expected %s recheckTime, got: %v", tc.testName, tc.expected.recheckTime, recheckTime)
		}
		if tc.expected.recheckTime != nil && recheckTime != nil {
			select {
			case event := <-recorder.Events:
				if !strings.Contains(event, "NotDeleted") {
					t.Errorf("Test case: %s. Expected %s event, got: %v", tc.testName, "NotDeleted", event)
				}
			default:
				t.Errorf("Test case: %s. Expected %s event, but no event occures", tc.testName, "NotDeleted")
			}
		}
	}
}

func TestCountHealthyMachines(t *testing.T) {
	healthyNode := newNode("healthyNode", true)
	unhealthyNode := newNode("unhealthyNode", false)

	currentTime := metav1.NewTime(time.Now())
	timeAfterThreeMinutes := metav1.NewTime(currentTime.Add(3 * time.Minute))
	timeBeforeThreeMinutes := metav1.NewTime(currentTime.Add(-3 * time.Minute))

	healthyMachine := newMachine("healthyMachine", healthyNode.Name, "Running")
	unhealthyMachine := newMachine("unhealthyMachine", unhealthyNode.Name, "Running")
	deletedMachine := newMachine("deletedMachine", healthyNode.Name, "Running")
	deletedMachine.DeletionTimestamp = &currentTime
	disruptedMachineBeforeTimeout := newMachine("disruptedMachineBeforeTimeout", healthyNode.Name, "Running")
	disruptedMachineAfterTimeout := newMachine("disruptedMachineAfterTimeout", healthyNode.Name, "Running")

	r := newFakeReconciler(nil, healthyNode, unhealthyNode)
	healthyMachinesCount := r.countHealthyMachines(
		[]mapiv1.Machine{
			*healthyMachine,
			*deletedMachine,
			*unhealthyMachine,
			*disruptedMachineBeforeTimeout,
			*disruptedMachineAfterTimeout,
		},
		map[string]metav1.Time{
			disruptedMachineBeforeTimeout.Name: timeBeforeThreeMinutes,
			disruptedMachineAfterTimeout.Name:  timeAfterThreeMinutes,
		},
		currentTime.Time,
	)

	expectedHealthyMachinesCount := int32(2)
	if healthyMachinesCount != expectedHealthyMachinesCount {
		t.Errorf("Expected %v healthy machines count, got: %v", expectedHealthyMachinesCount, healthyMachinesCount)
	}
}

func TestGetMachineDisruptionBudgetForMachine(t *testing.T) {
	node := newNode("node", true)

	machineWithoutLabels := newMachine("machineWithoutLabels", node.Name, "Running")
	machineWithoutLabels.Labels = map[string]string{}
	machineWithWrongLabel := newMachine("machineWithoutLabels", node.Name, "Running")
	machineWithWrongLabel.Labels = map[string]string{"wrongLabel": ""}
	machineWithRightLabel := newMachine("machineWithRightLabel", node.Name, "Running")

	mdbWithRightLabel1 := newMinAvailableMachineDisruptionBudget(1)
	mdbWithRightLabel1.Name = "mdbWithRightLabel1"
	mdbWithRightLabel2 := newMinAvailableMachineDisruptionBudget(1)
	mdbWithRightLabel2.Name = "mdbWithRightLabel2"
	mdbWithWrongSelector := newMinAvailableMachineDisruptionBudget(1)
	mdbWithWrongSelector.Name = "mdbWithWrongSelector"
	mdbWithWrongSelector.Spec.Selector = newSelector(map[string]string{"wrongSelector": ""})

	testsCases := []struct {
		testName string
		mdbs     []*healthcheckingv1alpha1.MachineDisruptionBudget
		machine  *mapiv1.Machine
		expected *healthcheckingv1alpha1.MachineDisruptionBudget
	}{
		{
			testName: "machine without labels",
			mdbs:     []*healthcheckingv1alpha1.MachineDisruptionBudget{mdbWithRightLabel1},
			machine:  machineWithoutLabels,
			expected: nil,
		},
		{
			testName: "machine with wrong label",
			mdbs:     []*healthcheckingv1alpha1.MachineDisruptionBudget{mdbWithRightLabel1},
			machine:  machineWithWrongLabel,
			expected: nil,
		},
		{
			testName: "MDB with wrong selector",
			mdbs:     []*healthcheckingv1alpha1.MachineDisruptionBudget{mdbWithWrongSelector},
			machine:  machineWithRightLabel,
			expected: nil,
		},
		{
			testName: "MDB with right selector",
			mdbs:     []*healthcheckingv1alpha1.MachineDisruptionBudget{mdbWithRightLabel1},
			machine:  machineWithRightLabel,
			expected: mdbWithRightLabel1,
		},
		{
			testName: "two MDB's with right selector",
			mdbs:     []*healthcheckingv1alpha1.MachineDisruptionBudget{mdbWithRightLabel1, mdbWithRightLabel2},
			machine:  machineWithRightLabel,
			expected: mdbWithRightLabel1,
		},
	}

	for _, tc := range testsCases {
		var recorder record.EventRecorder
		if len(tc.mdbs) > 1 {
			recorder = record.NewFakeRecorder(10)
		}

		objects := []runtime.Object{}
		for _, mdb := range tc.mdbs {
			objects = append(objects, mdb)
		}

		r := newFakeReconciler(recorder, objects...)
		mdb := r.getMachineDisruptionBudgetForMachine(tc.machine)
		if !reflect.DeepEqual(mdb, tc.expected) {
			t.Errorf("Expected %v machine disruption budget, got: %v", tc.expected, mdb)
		}
	}
}

type expectedReconcile struct {
	reconcile reconcile.Result
	event     *string
	error     bool
}

func TestReconcile(t *testing.T) {
	node := newNode("node", true)

	currentTime := metav1.NewTime(time.Now())
	timeAfterTwoMinutes := currentTime.Add(2 * time.Minute)
	timeBeforeThreeMinutes := metav1.NewTime(currentTime.Add(-3 * time.Minute))

	machineWithWrongLabel := newMachine("machineWithWrongLabel", node.Name, "Running")
	machineWithWrongLabel.Labels = map[string]string{"wrongLabel": ""}
	machineWithRightLabel := newMachine("machineWithRightLabel", node.Name, "Running")
	disruptedMachineBeforeTimeout := newMachine("disruptedMachineBeforeTimeout", node.Name, "Running")
	disruptedMachineAfterTimeout := newMachine("disruptedMachineAfterTimeout", node.Name, "Running")

	mdbWithRightLabel := newMinAvailableMachineDisruptionBudget(1)
	mdbWithWrongSelector := newMinAvailableMachineDisruptionBudget(1)
	mdbWithWrongSelector.Spec.Selector = &metav1.LabelSelector{
		MatchLabels:      map[string]string{},
		MatchExpressions: []metav1.LabelSelectorRequirement{{Operator: "fake"}},
	}
	mdbWithDisruptedMachines := newMinAvailableMachineDisruptionBudget(1)
	mdbWithDisruptedMachines.Status.DisruptedMachines = map[string]metav1.Time{
		disruptedMachineBeforeTimeout.Name: timeBeforeThreeMinutes,
		disruptedMachineAfterTimeout.Name:  currentTime,
	}

	noMachinesEvent := "NoMachines"

	testsCases := []struct {
		testName string
		mdb      *healthcheckingv1alpha1.MachineDisruptionBudget
		machines []*mapiv1.Machine
		expected *expectedReconcile
	}{
		{
			testName: "without MDB",
			mdb:      nil,
			machines: []*mapiv1.Machine{machineWithRightLabel},
			expected: &expectedReconcile{
				reconcile: reconcile.Result{},
				error:     false,
				event:     nil,
			},
		},
		{
			testName: "without machines",
			mdb:      mdbWithRightLabel,
			machines: []*mapiv1.Machine{machineWithWrongLabel},
			expected: &expectedReconcile{
				reconcile: reconcile.Result{},
				error:     false,
				event:     &noMachinesEvent,
			},
		},
		{
			testName: "with machines",
			mdb:      mdbWithRightLabel,
			machines: []*mapiv1.Machine{machineWithRightLabel},
			expected: &expectedReconcile{
				reconcile: reconcile.Result{},
				error:     false,
				event:     nil,
			},
		},
		{
			testName: "with MDB that has wrong selector",
			mdb:      mdbWithWrongSelector,
			machines: []*mapiv1.Machine{machineWithRightLabel},
			expected: &expectedReconcile{
				reconcile: reconcile.Result{},
				error:     false,
				event:     &noMachinesEvent,
			},
		},
		{
			testName: "with MDB that has dirupted machines",
			mdb:      mdbWithDisruptedMachines,
			machines: []*mapiv1.Machine{disruptedMachineBeforeTimeout, disruptedMachineAfterTimeout},
			expected: &expectedReconcile{
				reconcile: reconcile.Result{
					Requeue:      true,
					RequeueAfter: timeAfterTwoMinutes.Sub(currentTime.Time),
				},
				error: false,
				event: nil,
			},
		},
	}

	for _, tc := range testsCases {
		recorder := record.NewFakeRecorder(10)
		key := types.NamespacedName{
			Name:      "foobar",
			Namespace: namespace,
		}

		objects := []runtime.Object{}
		objects = append(objects, node)

		if tc.mdb != nil {
			objects = append(objects, tc.mdb)

		}

		for _, machine := range tc.machines {
			objects = append(objects, machine)
		}

		r := newFakeReconciler(recorder, objects...)
		result, err := r.Reconcile(reconcile.Request{NamespacedName: key})
		if result.Requeue != tc.expected.reconcile.Requeue ||
			result.RequeueAfter.Round(time.Minute) != tc.expected.reconcile.RequeueAfter {
			t.Errorf("Test case: %s. Expected: %v, got: %v", tc.testName, tc.expected.reconcile, result)
		}

		if tc.expected.error != (err != nil) {
			var errorExpectation string
			if !tc.expected.error == true {
				errorExpectation = "no"
			}
			t.Errorf("Test case: %s. Expected: %s error, got: %v", tc.testName, errorExpectation, err)
		}

		if tc.expected.event != nil {
			select {
			case event := <-recorder.Events:
				if !strings.Contains(event, noMachinesEvent) {
					t.Errorf("Test case: %s. Expected %s event, got: %v", tc.testName, noMachinesEvent, event)
				}
			default:
				t.Errorf("Test case: %s. Expected %s event, but no event occures", tc.testName, noMachinesEvent)
			}
		}
	}
}
