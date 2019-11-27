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

package machine

import (
	"context"
	"reflect"
	"testing"

	"github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	machinev1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	_ reconcile.Reconciler = &ReconcileMachine{}
)

func TestReconcileRequest(t *testing.T) {
	machineProvisioning := v1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "create",
			Namespace:  "default",
			Finalizers: []string{v1beta1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				v1beta1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: v1beta1.MachineSpec{
			ProviderSpec: v1beta1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
	}
	machineProvisioned := v1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "update",
			Namespace:  "default",
			Finalizers: []string{v1beta1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				v1beta1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: v1beta1.MachineSpec{
			ProviderSpec: v1beta1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "0.0.0.0",
				},
			},
		},
	}
	time := metav1.Now()
	machineDeleting := v1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete",
			Namespace:         "default",
			Finalizers:        []string{v1beta1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			DeletionTimestamp: &time,
			Labels: map[string]string{
				v1beta1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: v1beta1.MachineSpec{
			ProviderSpec: v1beta1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
	}
	machineFailed := v1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "failed",
			Namespace:  "default",
			Finalizers: []string{v1beta1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				v1beta1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: v1beta1.MachineSpec{
			ProviderID: pointer.StringPtr("providerID"),
			ProviderSpec: v1beta1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "0.0.0.0",
				},
			},
		},
	}
	machineRunning := v1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "running",
			Namespace:  "default",
			Finalizers: []string{v1beta1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				v1beta1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: v1beta1.MachineSpec{
			ProviderID: pointer.StringPtr("providerID"),
			ProviderSpec: v1beta1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "0.0.0.0",
				},
			},
			NodeRef: &corev1.ObjectReference{
				Name: "a node",
			},
		},
	}

	type expected struct {
		createCallCount int64
		existCallCount  int64
		updateCallCount int64
		deleteCallCount int64
		result          reconcile.Result
		error           bool
		phase           string
	}
	testCases := []struct {
		request     reconcile.Request
		existsValue bool
		expected    expected
	}{
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineProvisioning.Name, Namespace: machineProvisioning.Namespace}},
			existsValue: false,
			expected: expected{
				createCallCount: 1,
				existCallCount:  1,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           phaseProvisioning,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineProvisioned.Name, Namespace: machineProvisioned.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  1,
				updateCallCount: 1,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           phaseProvisioned,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeleting.Name, Namespace: machineDeleting.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  0,
				updateCallCount: 0,
				deleteCallCount: 1,
				result:          reconcile.Result{},
				error:           false,
				phase:           phaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineFailed.Name, Namespace: machineFailed.Namespace}},
			existsValue: false,
			expected: expected{
				createCallCount: 0,
				existCallCount:  1,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           phaseFailed, // A machine which does not exist but has providerID or addresses
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineRunning.Name, Namespace: machineRunning.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  1,
				updateCallCount: 1,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           phaseRunning,
			},
		},
	}

	for _, tc := range testCases {
		act := newTestActuator()
		act.ExistsValue = tc.existsValue
		v1beta1.AddToScheme(scheme.Scheme)
		r := &ReconcileMachine{
			Client: fake.NewFakeClientWithScheme(scheme.Scheme,
				&machineProvisioning,
				&machineProvisioned,
				&machineDeleting,
				&machineFailed,
				&machineRunning,
			),
			scheme:   scheme.Scheme,
			actuator: act,
		}

		result, err := r.Reconcile(tc.request)
		gotError := (err != nil)
		if tc.expected.error != gotError {
			var errorExpectation string
			if !tc.expected.error {
				errorExpectation = "no"
			}
			t.Errorf("Case: %s. Expected %s error, got: %v", tc.request.Name, errorExpectation, err)
		}

		if !reflect.DeepEqual(result, tc.expected.result) {
			t.Errorf("Case %s. Got: %v, expected %v", tc.request.Name, result, tc.expected.result)
		}

		if act.CreateCallCount != tc.expected.createCallCount {
			t.Errorf("Case %s. Got: %d createCallCount, expected %d", tc.request.Name, act.CreateCallCount, tc.expected.createCallCount)
		}

		if act.UpdateCallCount != tc.expected.updateCallCount {
			t.Errorf("Case %s. Got: %d updateCallCount, expected %d", tc.request.Name, act.UpdateCallCount, tc.expected.updateCallCount)
		}

		if act.ExistsCallCount != tc.expected.existCallCount {
			t.Errorf("Case %s. Got: %d existCallCount, expected %d", tc.request.Name, act.ExistsCallCount, tc.expected.existCallCount)
		}

		if act.DeleteCallCount != tc.expected.deleteCallCount {
			t.Errorf("Case %s. Got: %d deleteCallCount, expected %d", tc.request.Name, act.DeleteCallCount, tc.expected.deleteCallCount)
		}

		machine := &v1beta1.Machine{}
		if err := r.Client.Get(context.TODO(), tc.request.NamespacedName, machine); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if tc.expected.phase != stringPointerDeref(machine.Status.Phase) {
			t.Errorf("Case %s. Got: %v, expected: %v", tc.request.Name, stringPointerDeref(machine.Status.Phase), tc.expected.phase)
		}
	}
}

func TestSetPhase(t *testing.T) {
	name := "test"
	namespace := "test"
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      name,
	}

	machine := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Status: machinev1.MachineStatus{},
	}
	v1beta1.AddToScheme(scheme.Scheme)
	reconciler := &ReconcileMachine{
		Client: fake.NewFakeClientWithScheme(scheme.Scheme, machine),
		scheme: scheme.Scheme,
	}

	if err := reconciler.setPhase(machine, phaseRunning, ""); err != nil {
		t.Fatal(err)
	}
	// validate persisted object
	got := machinev1.Machine{}
	err := reconciler.Client.Get(context.TODO(), namespacedName, &got)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status.Phase == nil {
		t.Fatal("Got phase nil")
	}
	if *got.Status.Phase != phaseRunning {
		t.Errorf("Got: %v, expected: %v", *got.Status.Phase, phaseRunning)
	}
	lastUpdated := got.Status.LastUpdated
	if lastUpdated == nil {
		t.Errorf("Expected lastUpdated field to be updated")
	}
	// validate passed object
	if *machine.Status.Phase != phaseRunning {
		t.Errorf("Got: %v, expected: %v", *machine.Status.Phase, phaseRunning)
	}
	objectLastUpdated := machine.Status.LastUpdated
	if objectLastUpdated == nil {
		t.Errorf("Expected lastUpdated field to be updated")
	}

	// Set the same phase should not modify lastUpdated
	if err := reconciler.setPhase(machine, phaseRunning, ""); err != nil {
		t.Fatal(err)
	}
	// validate persisted object
	got = machinev1.Machine{}
	err = reconciler.Client.Get(context.TODO(), namespacedName, &got)
	if err != nil {
		t.Fatal(err)
	}
	if *lastUpdated != *got.Status.LastUpdated {
		t.Errorf("Expected: %v, got: %v", *lastUpdated, *got.Status.LastUpdated)
	}
	// validate passed object
	if *objectLastUpdated != *machine.Status.LastUpdated {
		t.Errorf("Expected: %v, got: %v", *objectLastUpdated, *machine.Status.LastUpdated)
	}

	// Set phaseFailed with an errorMessage should store the message
	expecterErrorMessage := "test"
	if err := reconciler.setPhase(machine, phaseFailed, expecterErrorMessage); err != nil {
		t.Fatal(err)
	}
	got = machinev1.Machine{}
	err = reconciler.Client.Get(context.TODO(), namespacedName, &got)
	if err != nil {
		t.Fatal(err)
	}
	// validate persisted object
	if expecterErrorMessage != *got.Status.ErrorMessage {
		t.Errorf("Expected: %v, got: %v", expecterErrorMessage, *got.Status.ErrorMessage)
	}
	// validate passed object
	if expecterErrorMessage != *machine.Status.ErrorMessage {
		t.Errorf("Expected: %v, got: %v", expecterErrorMessage, *machine.Status.ErrorMessage)
	}
}

func TestMachineIsProvisioned(t *testing.T) {
	name := "test"
	namespace := "test"
	providerID := "providerID"

	testCases := []struct {
		machine  *machinev1.Machine
		expected bool
	}{
		{
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Status: machinev1.MachineStatus{},
			},
			expected: false,
		},
		{
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Status: machinev1.MachineStatus{
					Addresses: []corev1.NodeAddress{
						{
							Type:    corev1.NodeInternalIP,
							Address: "0.0.0.0",
						},
					},
				},
			},
			expected: true,
		},
		{
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: namespace,
				},
				Spec: machinev1.MachineSpec{
					ProviderID: &providerID,
				},
				Status: machinev1.MachineStatus{},
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		if got := machineIsProvisioned(tc.machine); got != tc.expected {
			t.Errorf("Got: %v, expected: %v", got, tc.expected)
		}
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
		if got := stringPointerDeref(tc.stringPointer); got != tc.expected {
			t.Errorf("Got: %v, expected: %v", got, tc.expected)
		}
	}
}

func TestMachineIsFailed(t *testing.T) {
	testCases := []struct {
		machine  *machinev1.Machine
		expected bool
	}{
		{
			machine: &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "fromNilPhase",
					Namespace: "test",
				},
				Status: machinev1.MachineStatus{},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		if got := machineIsFailed(tc.machine); got {
			t.Errorf("Expected: %v, got: %v", got, tc.expected)
		}
	}
}
