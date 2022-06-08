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
	"testing"
	"time"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var c client.Client

const timeout = time.Second * 5

func TestReconcile(t *testing.T) {
	instance := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: "default",
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "foo",
			},
		},
		Spec: machinev1.MachineSpec{
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
	}

	// Setup the Manager and Controller.  Wrap the Controller Reconcile function so it writes each request to a
	// channel when it is finished.
	mgr, err := manager.New(cfg, manager.Options{MetricsBindAddress: "0"})
	if err != nil {
		t.Fatalf("error creating new manager: %v", err)
	}
	c = mgr.GetClient()

	a := newTestActuator()
	recFn := newReconciler(mgr, a)
	if err := add(mgr, recFn, "dummy"); err != nil {
		t.Fatalf("error adding controller to manager: %v", err)
	}

	stop, errChan := StartTestManager(mgr, t)
	defer func() {
		stop()
		if err := <-errChan; err != nil {
			t.Fatalf("error starting test manager: %v", err)
		}
	}()

	// Create the Machine object and expect Reconcile and the actuator to be called
	if err := c.Create(context.TODO(), instance); err != nil {
		t.Fatalf("error creating instance: %v", err)
	}
	defer c.Delete(context.TODO(), instance)
	g := NewWithT(t)
	g.Eventually(func() (machinev1.MachineStatus, error) {
		machine := &machinev1.Machine{}
		namespacedName := client.ObjectKey{Namespace: instance.Namespace, Name: instance.Name}
		err := c.Get(ctx, namespacedName, machine)
		if err != nil {
			return machinev1.MachineStatus{}, err
		}
		return machine.Status, nil
	}, timeout).ShouldNot(Equal(machinev1.MachineStatus{}))

	// TODO: Verify that the actuator is called correctly on Create
}
