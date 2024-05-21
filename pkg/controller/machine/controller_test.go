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
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	_ reconcile.Reconciler = &ReconcileMachine{}
)

func TestReconcileRequest(t *testing.T) {
	machineNoPhase := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "emptyPhase",
			Namespace:  "default",
			Finalizers: []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
		},
	}
	machineProvisioning := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "create",
			Namespace:  "default",
			Finalizers: []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			Phase:            ptr.To[string](machinev1.PhaseProvisioning),
		},
	}
	machineProvisioned := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "update",
			Namespace:  "default",
			Finalizers: []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "0.0.0.0",
				},
			},
		},
	}
	time := metav1.Now()
	machineDeleting := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete",
			Namespace:         "default",
			Finalizers:        []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			DeletionTimestamp: &time,
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
		},
	}
	machineDeletingPreDrainHook := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete-predrain",
			Namespace:         "default",
			Finalizers:        []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			DeletionTimestamp: &time,
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			LifecycleHooks: machinev1.LifecycleHooks{
				PreDrain: []machinev1.LifecycleHook{
					{
						Name:  "protect-from-drain",
						Owner: "machine-api-tests",
					},
				},
			},
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			NodeRef: &corev1.ObjectReference{
				Name: "a node",
			},
		},
	}
	machineDeletingPreDrainHookWithoutNode := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete-predrain-without-node",
			Namespace:         "default",
			Finalizers:        []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			DeletionTimestamp: &time,
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			LifecycleHooks: machinev1.LifecycleHooks{
				PreDrain: []machinev1.LifecycleHook{
					{
						Name:  "protect-from-drain",
						Owner: "machine-api-tests",
					},
				},
			},
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
		},
	}
	machineDeletingPreTerminateHook := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete-preterminate",
			Namespace:         "default",
			Finalizers:        []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			DeletionTimestamp: &time,
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			LifecycleHooks: machinev1.LifecycleHooks{
				PreTerminate: []machinev1.LifecycleHook{
					{
						Name:  "protect-from-terminate",
						Owner: "machine-api-tests",
					},
				},
			},
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
		},
	}
	machineFailed := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "failed",
			Namespace:  "default",
			Finalizers: []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			ProviderID:       ptr.To[string]("providerID"),
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "0.0.0.0",
				},
			},
		},
	}
	machineRunning := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "running",
			Namespace:  "default",
			Finalizers: []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			ProviderID:       ptr.To[string]("providerID"),
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
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
	machineDeletingAlreadyDrained := machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "machine.openshift.io/v1beta1",
			Kind:       "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:              "delete-drained",
			Namespace:         "default",
			Finalizers:        []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			DeletionTimestamp: &time,
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			LifecycleHooks: machinev1.LifecycleHooks{
				PreDrain: []machinev1.LifecycleHook{
					{
						Name:  "protect-from-drain",
						Owner: "machine-api-tests",
					},
				},
			},
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			AuthoritativeAPI: machinev1.MachineAuthorityMachineAPI,
			Conditions:       []machinev1.Condition{*conditions.TrueCondition(machinev1.MachineDrained)},
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
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineNoPhase.Name, Namespace: machineNoPhase.Namespace}},
			existsValue: false,
			expected: expected{
				createCallCount: 0,
				existCallCount:  1,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseProvisioning,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineProvisioning.Name, Namespace: machineProvisioning.Namespace}},
			existsValue: false,
			expected: expected{
				createCallCount: 1,
				existCallCount:  1,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{RequeueAfter: requeueAfter},
				error:           false,
				phase:           machinev1.PhaseProvisioning,
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
				result:          reconcile.Result{RequeueAfter: requeueAfter},
				error:           false,
				phase:           machinev1.PhaseProvisioned,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeleting.Name, Namespace: machineDeleting.Namespace}},
			existsValue: false,
			expected: expected{
				createCallCount: 0,
				existCallCount:  0,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeleting.Name, Namespace: machineDeleting.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  0,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeletingPreDrainHook.Name, Namespace: machineDeletingPreDrainHook.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  0,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeletingPreDrainHookWithoutNode.Name, Namespace: machineDeletingPreDrainHookWithoutNode.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  0,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeletingPreTerminateHook.Name, Namespace: machineDeletingPreTerminateHook.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  0,
				updateCallCount: 0,
				deleteCallCount: 0,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeletingAlreadyDrained.Name, Namespace: machineDeletingAlreadyDrained.Namespace}},
			existsValue: true,
			expected: expected{
				createCallCount: 0,
				existCallCount:  1,
				updateCallCount: 0,
				deleteCallCount: 1,
				result:          reconcile.Result{RequeueAfter: requeueAfter},
				error:           false,
				phase:           machinev1.PhaseDeleting,
			},
		},
		{
			request:     reconcile.Request{NamespacedName: types.NamespacedName{Name: machineDeletingAlreadyDrained.Name, Namespace: machineDeletingAlreadyDrained.Namespace}},
			existsValue: false,
			expected: expected{
				createCallCount: 0,
				existCallCount:  1,
				updateCallCount: 0,
				deleteCallCount: 1,
				result:          reconcile.Result{},
				error:           false,
				phase:           machinev1.PhaseDeleting,
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
				phase:           machinev1.PhaseFailed, // A machine which does not exist but has providerID or addresses
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
				phase:           machinev1.PhaseRunning,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.request.Name, func(t *testing.T) {
			act := newTestActuator()
			act.ExistsValue = tc.existsValue
			r := &ReconcileMachine{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(
					&machineNoPhase,
					&machineProvisioning,
					&machineProvisioned,
					&machineDeleting,
					&machineDeletingPreDrainHook,
					&machineDeletingPreDrainHookWithoutNode,
					&machineDeletingPreTerminateHook,
					&machineFailed,
					&machineRunning,
					&machineDeletingAlreadyDrained,
				).WithStatusSubresource(&machinev1.Machine{}).Build(),
				scheme:   scheme.Scheme,
				actuator: act,
			}

			result, err := r.Reconcile(ctx, tc.request)
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

			machine := &machinev1.Machine{}
			if err := r.Client.Get(context.TODO(), tc.request.NamespacedName, machine); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expected.phase != ptr.Deref(machine.Status.Phase, "") {
				t.Errorf("Case %s. Got: %v, expected: %v", tc.request.Name, ptr.Deref(machine.Status.Phase, ""), tc.expected.phase)
			}
		})
	}
}

func TestUpdateStatus(t *testing.T) {
	cleanupFn := StartEnvTest(t)
	defer cleanupFn(t)

	drainableTrue := conditions.TrueCondition(machinev1.MachineDrainable)
	terminableTrue := conditions.TrueCondition(machinev1.MachineTerminable)
	defaultLifecycleConditions := []machinev1.Condition{*drainableTrue, *terminableTrue}

	testCases := []struct {
		name                   string
		phase                  string
		err                    error
		annotations            map[string]string
		existingProviderStatus string
		expectedProviderStatus string
		conditions             []machinev1.Condition
		originalConditions     []machinev1.Condition
		updated                bool
	}{
		{
			name:        "when the status is not changed",
			phase:       machinev1.PhaseRunning,
			err:         nil,
			annotations: nil,
			conditions:  defaultLifecycleConditions,
		},
		{
			name:  "when updating the phase to Failed",
			phase: machinev1.PhaseFailed,
			err:   errors.New("test"),
			annotations: map[string]string{
				MachineInstanceStateAnnotationName: unknownInstanceState,
			},
			updated:    true,
			conditions: defaultLifecycleConditions,
		},
		{
			name:  "when updating the phase to Failed with instanceState Set",
			phase: machinev1.PhaseFailed,
			err:   errors.New("test"),
			annotations: map[string]string{
				MachineInstanceStateAnnotationName: unknownInstanceState,
			},
			existingProviderStatus: `{"instanceState":"Running"}`,
			expectedProviderStatus: `{"instanceState":"Unknown"}`,
			updated:                true,
			conditions:             defaultLifecycleConditions,
		},
		{
			name:  "when updating the phase to Failed with vmState Set",
			phase: machinev1.PhaseFailed,
			err:   errors.New("test"),
			annotations: map[string]string{
				MachineInstanceStateAnnotationName: unknownInstanceState,
			},
			existingProviderStatus: `{"vmState":"Running"}`,
			expectedProviderStatus: `{"vmState":"Unknown"}`,
			updated:                true,
			conditions:             defaultLifecycleConditions,
		},
		{
			name:  "when updating the phase to Failed with state Set",
			phase: machinev1.PhaseFailed,
			err:   errors.New("test"),
			annotations: map[string]string{
				MachineInstanceStateAnnotationName: unknownInstanceState,
			},
			existingProviderStatus: `{"state":"Running"}`,
			expectedProviderStatus: `{"state":"Running"}`,
			updated:                true,
			conditions:             defaultLifecycleConditions,
		},
		{
			name:        "when adding a condition",
			phase:       machinev1.PhaseRunning,
			err:         nil,
			annotations: nil,
			conditions: []machinev1.Condition{
				*conditions.TrueCondition(machinev1.InstanceExistsCondition),
				*drainableTrue,
				*terminableTrue,
			},
			updated: true,
		},
		{
			name:        "when updating a condition",
			phase:       machinev1.PhaseRunning,
			err:         nil,
			annotations: nil,
			conditions: []machinev1.Condition{
				*conditions.FalseCondition(machinev1.InstanceExistsCondition, machinev1.InstanceMissingReason, machinev1.ConditionSeverityWarning, "message"),
				*drainableTrue,
				*terminableTrue,
			},
			originalConditions: []machinev1.Condition{
				*conditions.TrueCondition(machinev1.InstanceExistsCondition),
			},
			updated: true,
		},
		{
			name:        "when the conditions do not change",
			phase:       machinev1.PhaseRunning,
			err:         nil,
			annotations: nil,
			conditions: []machinev1.Condition{
				*conditions.TrueCondition(machinev1.InstanceExistsCondition),
				*drainableTrue,
				*terminableTrue,
			},
			originalConditions: []machinev1.Condition{
				*conditions.TrueCondition(machinev1.InstanceExistsCondition),
			},
			updated: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			k8sClient, err := client.New(cfg, client.Options{})
			g.Expect(err).ToNot(HaveOccurred())
			reconciler := &ReconcileMachine{
				Client: k8sClient,
				scheme: scheme.Scheme,
			}

			// Set up the test namespace
			name := "test"
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: name,
				},
			}
			g.Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

			// Set up the test machine
			machine := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: name,
					Namespace:    namespace.Name,
				},
			}

			g.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
			defer func() {
				if err := k8sClient.Delete(ctx, machine); err != nil {
					t.Fatalf("error deleting machine: %v", err)
				}
			}()

			if tc.existingProviderStatus != "" {
				machine.Status.ProviderStatus = &runtime.RawExtension{
					Raw: []byte(tc.existingProviderStatus),
				}
			}

			g.Expect(k8sClient.Status().Update(ctx, machine)).To(Succeed())

			namespacedName := types.NamespacedName{
				Namespace: machine.Namespace,
				Name:      machine.Name,
			}

			for _, cond := range tc.originalConditions {
				c := cond
				conditions.Set(machine, &c)
			}

			// Set the phase to Running initially
			g.Expect(reconciler.updateStatus(context.TODO(), machine, machinev1.PhaseRunning, nil, []machinev1.Condition{})).To(Succeed())
			// validate persisted object
			got := machinev1.Machine{}
			g.Expect(reconciler.Client.Get(context.TODO(), namespacedName, &got)).To(Succeed())
			g.Expect(got.Status.Phase).ToNot(BeNil())
			g.Expect(*got.Status.Phase).To(Equal(machinev1.PhaseRunning))
			lastUpdated := got.Status.LastUpdated
			gotConditions := got.Status.Conditions
			g.Expect(lastUpdated).ToNot(BeNil())
			// validate passed object
			g.Expect(machine.Status.Phase).ToNot(BeNil())
			g.Expect(*machine.Status.Phase).To(Equal(machinev1.PhaseRunning))
			objectLastUpdated := machine.Status.LastUpdated
			g.Expect(objectLastUpdated).ToNot(BeNil())

			// Set the time func so that we can check lastUpdated is set correctly
			reconciler.nowFunc = func() time.Time {
				return time.Now().Add(5 * time.Second)
			}

			// Modify the phase and conditions and verify the result
			for _, cond := range tc.conditions {
				c := cond
				conditions.Set(machine, &c)
			}
			g.Expect(reconciler.updateStatus(context.TODO(), machine, tc.phase, tc.err, gotConditions)).To(Succeed())
			// validate the persisted object
			got = machinev1.Machine{}
			g.Expect(reconciler.Client.Get(context.TODO(), namespacedName, &got)).To(Succeed())

			if tc.updated {
				g.Expect(got.Status.LastUpdated.UnixNano()).ToNot(Equal(lastUpdated.UnixNano()))
				g.Expect(machine.Status.LastUpdated.UnixNano()).ToNot(Equal(objectLastUpdated.UnixNano()))
			} else {
				g.Expect(got.Status.LastUpdated.UnixNano()).To(Equal(lastUpdated.UnixNano()))
				g.Expect(machine.Status.LastUpdated.UnixNano()).To(Equal(objectLastUpdated.UnixNano()))
			}

			if tc.err != nil {
				g.Expect(got.Status.ErrorMessage).ToNot(BeNil())
				g.Expect(*got.Status.ErrorMessage).To(Equal(tc.err.Error()))
				g.Expect(machine.Status.ErrorMessage).ToNot(BeNil())
				g.Expect(*machine.Status.ErrorMessage).To(Equal(tc.err.Error()))
			}

			g.Expect(*got.Status.Phase).To(Equal(tc.phase))
			g.Expect(*machine.Status.Phase).To(Equal(tc.phase))

			g.Expect(got.Status.Conditions).To(conditions.MatchConditions(tc.conditions))
			g.Expect(machine.Status.Conditions).To(conditions.MatchConditions(tc.conditions))

			g.Expect(got.GetAnnotations()).To(Equal(tc.annotations))
			g.Expect(machine.GetAnnotations()).To(Equal(tc.annotations))

			if tc.existingProviderStatus != "" {
				g.Expect(got.Status.ProviderStatus).ToNot(BeNil())
				g.Expect(got.Status.ProviderStatus.Raw).To(BeEquivalentTo(tc.expectedProviderStatus))
			}
		})
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

func TestNodeIsUnreachable(t *testing.T) {
	testCases := []struct {
		name     string
		node     *corev1.Node
		expected bool
	}{
		{
			name: "Node should be unreachable",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Node",
					Namespace: "test",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionUnknown,
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Node should not be unreachable",
			node: &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Node",
					Namespace: "test",
				},
				Status: corev1.NodeStatus{
					Conditions: []corev1.NodeCondition{
						{
							Type:   corev1.NodeReady,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := nodeIsUnreachable(tc.node); actual != tc.expected {
				t.Errorf("Expected: %v, got: %v", actual, tc.expected)
			}
		})
	}
}

func TestIsInvalidMachineConfigurationError(t *testing.T) {
	invalidMachineConfigurationError := InvalidMachineConfiguration("invalidConfiguration")
	createError := CreateMachine("createFailed")

	testCases := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "With an InvalidMachineConfigurationError",
			err:      invalidMachineConfigurationError,
			expected: true,
		},
		{
			name:     "With a CreateError",
			err:      createError,
			expected: false,
		},
		{
			name:     "With a wrapped InvalidMachineConfigurationError",
			err:      fmt.Errorf("Wrap: %w", invalidMachineConfigurationError),
			expected: true,
		},
		{
			name:     "With a wrapped CreateError",
			err:      fmt.Errorf("Wrap: %w", createError),
			expected: false,
		},
		{
			name:     "With a double wrapped InvalidMachineConfigurationError",
			err:      fmt.Errorf("Wrap: %w", fmt.Errorf("Wrap: %w", invalidMachineConfigurationError)),
			expected: true,
		},
		{
			name:     "With a double wrapped CreateError",
			err:      fmt.Errorf("Wrap: %w", fmt.Errorf("Wrap: %w", createError)),
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if actual := isInvalidMachineConfigurationError(tc.err); actual != tc.expected {
				t.Errorf("Case: %s, got: %v, expected: %v", tc.name, actual, tc.expected)
			}
		})
	}
}

func TestDelayIfRequeueAfterError(t *testing.T) {
	requeueAfter30s := &RequeueAfterError{RequeueAfter: 30 * time.Second}
	requeueAfter1m := &RequeueAfterError{RequeueAfter: time.Minute}
	createError := CreateMachine("createFailed")
	wrappedCreateError := fmt.Errorf("Wrap: %w", createError)
	doubleWrappedCreateError := fmt.Errorf("Wrap: %w", fmt.Errorf("Wrap: %w", createError))

	testCases := []struct {
		name           string
		err            error
		expectedErr    error
		expectedResult reconcile.Result
	}{
		{
			name:           "with a RequeAfterError (30s)",
			err:            requeueAfter30s,
			expectedErr:    nil,
			expectedResult: reconcile.Result{Requeue: true, RequeueAfter: 30 * time.Second},
		},
		{
			name:           "with a RequeAfterError (1m)",
			err:            requeueAfter1m,
			expectedErr:    nil,
			expectedResult: reconcile.Result{Requeue: true, RequeueAfter: time.Minute},
		},
		{
			name:           "with a CreateError",
			err:            createError,
			expectedErr:    createError,
			expectedResult: reconcile.Result{},
		},
		{
			name:           "with a wrapped RequeAfterError (30s)",
			err:            fmt.Errorf("Wrap: %w", requeueAfter30s),
			expectedErr:    nil,
			expectedResult: reconcile.Result{Requeue: true, RequeueAfter: 30 * time.Second},
		},
		{
			name:           "with a wrapped RequeAfterError (1m)",
			err:            fmt.Errorf("Wrap: %w", requeueAfter1m),
			expectedErr:    nil,
			expectedResult: reconcile.Result{Requeue: true, RequeueAfter: time.Minute},
		},
		{
			name:           "with a wrapped CreateError",
			err:            wrappedCreateError,
			expectedErr:    wrappedCreateError,
			expectedResult: reconcile.Result{},
		},
		{
			name:           "with a double wrapped RequeAfterError (30s)",
			err:            fmt.Errorf("Wrap: %w", fmt.Errorf("Wrap: %w", requeueAfter30s)),
			expectedErr:    nil,
			expectedResult: reconcile.Result{Requeue: true, RequeueAfter: 30 * time.Second},
		},
		{
			name:           "with a double wrapped RequeAfterError (1m)",
			err:            fmt.Errorf("Wrap: %w", fmt.Errorf("Wrap: %w", requeueAfter1m)),
			expectedErr:    nil,
			expectedResult: reconcile.Result{Requeue: true, RequeueAfter: time.Minute},
		},
		{
			name:           "with a double wrapped CreateError",
			err:            doubleWrappedCreateError,
			expectedErr:    doubleWrappedCreateError,
			expectedResult: reconcile.Result{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := delayIfRequeueAfterError(tc.err)
			if err != tc.expectedErr {
				t.Errorf("Case: %s, got: %v, expected: %v", tc.name, err, tc.expectedErr)
			}
			if result != tc.expectedResult {
				t.Errorf("Case: %s, got: %v, expected: %v", tc.name, result, tc.expectedResult)
			}
		})
	}
}

func TestSetLifecycleHookConditions(t *testing.T) {
	drainableTrue := conditions.TrueCondition(machinev1.MachineDrainable)
	terminableTrue := conditions.TrueCondition(machinev1.MachineTerminable)
	unrelatedCondition := conditions.FalseCondition(machinev1.MachineCreated, "", machinev1.ConditionSeverityNone, "")

	preDrainHook := machinev1.LifecycleHook{
		Name:  "pre-drain",
		Owner: "pre-drain-owner",
	}
	drainableFalse := conditions.FalseCondition(machinev1.MachineDrainable, machinev1.MachineHookPresent, machinev1.ConditionSeverityWarning, "Drain operation currently blocked by: [{Name:pre-drain Owner:pre-drain-owner}]")

	otherPreDrainHook := machinev1.LifecycleHook{
		Name:  "other-pre-drain",
		Owner: "other-pre-drain-owner",
	}
	drainableFalseWithOther := conditions.FalseCondition(machinev1.MachineDrainable, machinev1.MachineHookPresent, machinev1.ConditionSeverityWarning, "Drain operation currently blocked by: [{Name:pre-drain Owner:pre-drain-owner} {Name:other-pre-drain Owner:other-pre-drain-owner}]")

	preTerminateHook := machinev1.LifecycleHook{
		Name:  "pre-terminate",
		Owner: "pre-terminate-owner",
	}
	terminableFalse := conditions.FalseCondition(machinev1.MachineTerminable, machinev1.MachineHookPresent, machinev1.ConditionSeverityWarning, "Terminate operation currently blocked by: [{Name:pre-terminate Owner:pre-terminate-owner}]")

	testCases := []struct {
		name               string
		existingConditions []machinev1.Condition
		lifecycleHooks     machinev1.LifecycleHooks
		expectedConditions []machinev1.Condition
	}{
		{
			name: "with a fresh machine",
			expectedConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableTrue,
			},
		},
		{
			name: "with an unrelated condition",
			existingConditions: []machinev1.Condition{
				*unrelatedCondition,
			},
			expectedConditions: []machinev1.Condition{
				*unrelatedCondition,
				*drainableTrue,
				*terminableTrue,
			},
		},
		{
			name: "with a pre-drain hook",
			existingConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableTrue,
			},
			lifecycleHooks: machinev1.LifecycleHooks{
				PreDrain: []machinev1.LifecycleHook{preDrainHook},
			},
			expectedConditions: []machinev1.Condition{
				*drainableFalse,
				*terminableTrue,
			},
		},
		{
			name: "with a pre-terminate hook",
			existingConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableTrue,
			},
			lifecycleHooks: machinev1.LifecycleHooks{
				PreTerminate: []machinev1.LifecycleHook{preTerminateHook},
			},
			expectedConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableFalse,
			},
		},
		{
			name: "with a both a pre-drain and pre-terminate hook",
			existingConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableTrue,
			},
			lifecycleHooks: machinev1.LifecycleHooks{
				PreDrain:     []machinev1.LifecycleHook{preDrainHook},
				PreTerminate: []machinev1.LifecycleHook{preTerminateHook},
			},
			expectedConditions: []machinev1.Condition{
				*drainableFalse,
				*terminableFalse,
			},
		},
		{
			name: "with multiple pre-drain hooks",
			existingConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableTrue,
			},
			lifecycleHooks: machinev1.LifecycleHooks{
				PreDrain: []machinev1.LifecycleHook{preDrainHook, otherPreDrainHook},
			},
			expectedConditions: []machinev1.Condition{
				*drainableFalseWithOther,
				*terminableTrue,
			},
		},
		{
			name: "with hooks are removed",
			existingConditions: []machinev1.Condition{
				*drainableFalse,
				*terminableFalse,
			},
			expectedConditions: []machinev1.Condition{
				*drainableTrue,
				*terminableTrue,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			machine := &machinev1.Machine{
				Spec: machinev1.MachineSpec{
					LifecycleHooks: tc.lifecycleHooks,
				},
				Status: machinev1.MachineStatus{
					Conditions: tc.existingConditions,
				},
			}

			setLifecycleHookConditions(machine)
			g.Expect(machine.Status.Conditions).To(conditions.MatchConditions(tc.expectedConditions))
		})
	}
}
