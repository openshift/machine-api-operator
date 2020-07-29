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

package cpmachinehooks

import (
	"context"
	"errors"
	"time"

	"github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	requeueAfter        = 30 * time.Second
	cpPreDrainHookKey   = "pre-terminate.delete.hook.machine.cluster.x-k8s.io/openshift-cp-machineset"
	cpPreDrainHookValue = "cpmachine-hooks-controller"
)

// Add adds reconciler to manager
func Add(mgr manager.Manager, opts manager.Options) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	r := &ReconcileMachine{
		Client:        mgr.GetClient(),
		eventRecorder: mgr.GetEventRecorderFor("cpmachine-hooks-controller"),
		config:        mgr.GetConfig(),
		scheme:        mgr.GetScheme(),
	}
	return r
}

func stringPointerDeref(stringPointer *string) string {
	if stringPointer != nil {
		return *stringPointer
	}
	return ""
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("cpmachine_hooks_controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to Machine
	return c.Watch(
		&source.Kind{Type: &v1beta1.Machine{}},
		&handler.EnqueueRequestForObject{},
	)
}

// ReconcileMachine reconciles a Machine object
type ReconcileMachine struct {
	client.Client
	config        *rest.Config
	scheme        *runtime.Scheme
	eventRecorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a Machine object and makes changes based on the state read
// and what is in the Machine.Spec
// +kubebuilder:rbac:groups=machine.openshift.io,resources=machines;machines/status,verbs=get;list;watch;create;update;patch;delete
func (r *ReconcileMachine) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	// TODO(mvladev): Can context be passed from Kubebuilder?
	ctx := context.TODO()

	// Fetch the Machine instance
	m := &v1beta1.Machine{}
	if err := r.Client.Get(ctx, request.NamespacedName, m); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return reconcile.Result{}, nil
		}

		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// Get owning machineset; If the user has modified labels or added
	// conflicting machinesets after our hooks have been added, we won't take
	// any action to remove our hooks.
	mss := r.getCPMachineSetsForMachine(m)
	if len(mss) == 0 {
		klog.V(4).Infof("Found no cp machine set for machine: %v", m.Name)
		return reconcile.Result{}, nil
	}
	if len(mss) > 1 {
		klog.Errorf("Found too many cp machinesets for machine: %v", m.Name)
		klog.Errorf("Machinesets found: %v", mss)
		return reconcile.Result{}, errors.New("Too many machinesets")
	}

	// At this point, we've checked to ensure this Machine is backed by a
	// Control Plane MachineSet.
	ms = mss[0]

	machineName := m.GetName()
	klog.Infof("%v: reconciling Machine", machineName)

	if m.ObjectMeta.DeletionTimestamp.IsZero() {
		// We want to ensure we wait until the machine has a valid NodeRef
		// so we can delete any failed machines without blocking on hooks.
		if machineHasNode(m) {
			// Add hooks
			if _, exists := m.ObjectMeta.Annotations[cpPreDrainHookKey]; !exists {
				annotations := m.ObjectMeta.GetAnnotations()
				if annotations == nil {
					annotations = make(map[string]string)
				}
				annotations[cpPreDrainHookKey] = cpPreDrainHookValue
				m.ObjectMeta.SetAnnotations(annotations)
				if err := r.Client.Update(context.Background(), m); err != nil {
					klog.Errorf("Failed to add/remove annotation %q: %v", m.Name, err)
					return reconcile.Result{}, err
				}
			}
		}
		return reconcile.Result{}, nil
	}

	// Machine has been marked for deletion, see if it has hooks
	annotations := m.ObjectMeta.GetAnnotations()
	if _, exists := annotations[cpPreDrainHookKey]; !exists {
		// Looks like it was already removed, nothing to do.
		return reconcile.Result{}, nil
	}

	// Determine if machineset has enough ready replicas
	// There's a potential race condition here, especially if the
	// machineset controller is not running for some reason.
	if ms.Status.Replicas == ms.Status.AvailableReplicas {
		// TODO: check etcd operator status and verify this node is not part of
		// quorum.
		// Remove pre-drain hook
		delete(annotations, cpPreDrainHookKey)
		m.ObjectMeta.SetAnnotations(annotations)
		if err := r.Client.Update(context.Background(), m); err != nil {
			klog.Errorf("Failed to add/remove annotation %q: %v", m.Name, err)
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	return reconcile.Result{RequeueAfter: requeueAfter}, errors.New("Waiting on replacement machine")
}

func (r *ReconcileMachine) getCPMachineSetsForMachine(m *v1beta1.Machine) []*v1beta1.MachineSet {
	if len(m.Labels) == 0 {
		klog.Warningf("No machine sets found for Machine %v because it has no labels", m.Name)
		return nil
	}

	msList := &v1beta1.MachineSetList{}
	err := r.Client.List(context.Background(), msList, client.InNamespace(m.Namespace))
	if err != nil {
		klog.Errorf("Failed to list machine sets, %v", err)
		return nil
	}

	var mss []*v1beta1.MachineSet
	for idx := range msList.Items {
		ms := &msList.Items[idx]
		// Determine if machineset has the appropriate labels
		if isCPMS(ms) && hasMatchingLabels(ms, m) {
			mss = append(mss, ms)
		}
	}

	return mss
}

func hasMatchingLabels(machineSet *v1beta1.MachineSet, machine *v1beta1.Machine) bool {
	selector, err := metav1.LabelSelectorAsSelector(&machineSet.Spec.Selector)
	if err != nil {
		klog.Warningf("unable to convert selector: %v", err)
		return false
	}

	// If a deployment with a nil or empty selector creeps in, it should match nothing, not everything.
	if selector.Empty() {
		klog.V(2).Infof("%v machineset has empty selector", machineSet.Name)
		return false
	}

	if !selector.Matches(labels.Set(machine.Labels)) {
		klog.V(4).Infof("%v machine has mismatch labels", machine.Name)
		return false
	}

	return true
}

func machineHasNode(machine *v1beta1.Machine) bool {
	return machine.Status.NodeRef != nil
}

func isCPMS(ms *v1beta1.MachineSet) bool {
	if ms.Spec.Template.ObjectMeta.Labels == nil {
		return false
	}
	val, ok := ms.Spec.Template.ObjectMeta.Labels["machine.openshift.io/cluster-api-machine-role"]
	if ok {
		if val == "master" {
			return true
		}
	}
	return false
}
