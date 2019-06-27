package disruption

import (
	"reflect"
	"strings"
	"testing"
	"time"

	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	healthcheckingv1alpha1 "github.com/openshift/machine-api-operator/pkg/apis/healthchecking/v1alpha1"
	maotesting "github.com/openshift/machine-api-operator/pkg/util/testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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
		namespace: maotesting.Namespace,
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

func newMachineSet(name string, size int32) *mapiv1.MachineSet {
	return &mapiv1.MachineSet{
		TypeMeta: metav1.TypeMeta{Kind: "MachineSet"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: maotesting.Namespace,
			Labels:    maotesting.FooBar(),
			UID:       uuid.NewUUID(),
		},
		Spec: mapiv1.MachineSetSpec{
			Replicas: &size,
			Selector: *maotesting.NewSelectorFooBar(),
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
			Namespace: maotesting.Namespace,
			Labels:    maotesting.FooBar(),
			UID:       uuid.NewUUID(),
		},
		Spec: mapiv1.MachineDeploymentSpec{
			Replicas: &size,
			Selector: *maotesting.NewSelectorFooBar(),
		},
	}
}

type expectedMachineCount struct {
	count   int32
	healthy int32
}

func TestGetExpectedMachineCount(t *testing.T) {
	mdbMinAvailable := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbMaxUnavailable := maotesting.NewMaxUnavailableMachineDisruptionBudget(1)

	node := maotesting.NewNode("node", true)

	// will check the expected result when the machine does not owned by controller
	machine := maotesting.NewMachine("machine1", node.Name)

	// will check the expected result when the machine owned by MachineSet controller
	machineSet := newMachineSet("ms1", 3)
	machineControlledByMachineSet := maotesting.NewMachine("machine2", node.Name)
	updateMachineOwnerToMachineSet(machineControlledByMachineSet, machineSet)

	// will check the expected result when the machine owned by MachineDeployment controller
	machineSetControlledByDeployment := newMachineSet("ms2", 4)
	machineDeployment := newMachineDeployment("md1", 4)
	updateMachineSetOwnerToMachineDeployment(machineSetControlledByDeployment, machineDeployment)
	machineControlledByMachineDeployment := maotesting.NewMachine("machine3", node.Name)
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
	mdbWithSelector := maotesting.NewMinAvailableMachineDisruptionBudget(3)

	mdbWithoutSelector := maotesting.NewMinAvailableMachineDisruptionBudget(3)
	mdbWithoutSelector.Spec.Selector = nil

	mdbWithEmptySelector := maotesting.NewMinAvailableMachineDisruptionBudget(3)
	mdbWithEmptySelector.Spec.Selector = &metav1.LabelSelector{}

	mdbWithBadSelector := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithBadSelector.Spec.Selector = &metav1.LabelSelector{
		MatchLabels:      map[string]string{},
		MatchExpressions: []metav1.LabelSelectorRequirement{{Operator: "fake"}},
	}

	node := maotesting.NewNode("node", true)

	machineWithLabels1 := maotesting.NewMachine("machineWithLabels1", node.Name)
	machineWithLabels2 := maotesting.NewMachine("machineWithLabels2", node.Name)
	machineWithoutLabels := maotesting.NewMachine("machineWithoutLabels", node.Name)
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
	node := maotesting.NewNode("node", true)

	currentTime := metav1.NewTime(time.Now())
	timeAfterTwoMinutes := currentTime.Add(2 * time.Minute)
	timeBeforeThreeMinutes := metav1.NewTime(currentTime.Add(-3 * time.Minute))

	machine := maotesting.NewMachine("machine", node.Name)
	deletedMachine := maotesting.NewMachine("deletedMachine", node.Name)
	deletedMachine.DeletionTimestamp = &currentTime
	disruptedMachineBeforeTimeout := maotesting.NewMachine("disruptedMachineBeforeTimeout", node.Name)
	disruptedMachineAfterTimeout := maotesting.NewMachine("disruptedMachineAfterTimeout", node.Name)

	mdbWithDisruptedMachines := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithDisruptedMachines.Status.DisruptedMachines = map[string]metav1.Time{
		disruptedMachineBeforeTimeout.Name: timeBeforeThreeMinutes,
		disruptedMachineAfterTimeout.Name:  currentTime,
	}
	mdbWithoutDisruptedMachines := maotesting.NewMinAvailableMachineDisruptionBudget(1)

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
	healthyNode := maotesting.NewNode("healthyNode", true)
	unhealthyNode := maotesting.NewNode("unhealthyNode", false)

	currentTime := metav1.NewTime(time.Now())
	timeAfterThreeMinutes := metav1.NewTime(currentTime.Add(3 * time.Minute))
	timeBeforeThreeMinutes := metav1.NewTime(currentTime.Add(-3 * time.Minute))

	healthyMachine := maotesting.NewMachine("healthyMachine", healthyNode.Name)
	unhealthyMachine := maotesting.NewMachine("unhealthyMachine", unhealthyNode.Name)
	deletedMachine := maotesting.NewMachine("deletedMachine", healthyNode.Name)
	deletedMachine.DeletionTimestamp = &currentTime
	disruptedMachineBeforeTimeout := maotesting.NewMachine("disruptedMachineBeforeTimeout", healthyNode.Name)
	disruptedMachineAfterTimeout := maotesting.NewMachine("disruptedMachineAfterTimeout", healthyNode.Name)

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
	node := maotesting.NewNode("node", true)

	machineWithoutLabels := maotesting.NewMachine("machineWithoutLabels", node.Name)
	machineWithoutLabels.Labels = map[string]string{}
	machineWithWrongLabel := maotesting.NewMachine("machineWithoutLabels", node.Name)
	machineWithWrongLabel.Labels = map[string]string{"wrongLabel": ""}
	machineWithRightLabel := maotesting.NewMachine("machineWithRightLabel", node.Name)

	mdbWithRightLabel1 := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithRightLabel1.Name = "mdbWithRightLabel1"
	mdbWithRightLabel2 := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithRightLabel2.Name = "mdbWithRightLabel2"
	mdbWithWrongSelector := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithWrongSelector.Name = "mdbWithWrongSelector"
	mdbWithWrongSelector.Spec.Selector = maotesting.NewSelector(map[string]string{"wrongSelector": ""})

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
	node := maotesting.NewNode("node", true)

	currentTime := metav1.NewTime(time.Now())
	timeAfterTwoMinutes := currentTime.Add(2 * time.Minute)
	timeBeforeThreeMinutes := metav1.NewTime(currentTime.Add(-3 * time.Minute))

	machineWithWrongLabel := maotesting.NewMachine("machineWithWrongLabel", node.Name)
	machineWithWrongLabel.Labels = map[string]string{"wrongLabel": ""}
	machineWithRightLabel := maotesting.NewMachine("machineWithRightLabel", node.Name)
	disruptedMachineBeforeTimeout := maotesting.NewMachine("disruptedMachineBeforeTimeout", node.Name)
	disruptedMachineAfterTimeout := maotesting.NewMachine("disruptedMachineAfterTimeout", node.Name)

	mdbWithRightLabel := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithWrongSelector := maotesting.NewMinAvailableMachineDisruptionBudget(1)
	mdbWithWrongSelector.Spec.Selector = &metav1.LabelSelector{
		MatchLabels:      map[string]string{},
		MatchExpressions: []metav1.LabelSelectorRequirement{{Operator: "fake"}},
	}
	mdbWithDisruptedMachines := maotesting.NewMinAvailableMachineDisruptionBudget(1)
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
			Namespace: maotesting.Namespace,
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
			if !tc.expected.error {
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
