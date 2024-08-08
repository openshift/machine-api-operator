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

package machineset

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinev1resourcebuilder "github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder/machine/v1beta1"
	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("MachineSet Reconciler", func() {
	var mgrCtxCancel context.CancelFunc
	var mgrStopped chan struct{}
	var namespace *corev1.Namespace
	var machineSetBuilder machinev1resourcebuilder.MachineSetBuilder
	var replicas int32 = int32(2)

	BeforeEach(func() {
		By("Setting up a new manager")
		mgr, err := manager.New(cfg, manager.Options{
			Metrics: server.Options{
				BindAddress: "0",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		k8sClient = mgr.GetClient()

		By("Setting up feature gates")
		gate, err := testutils.NewDefaultMutableFeatureGate()
		Expect(err).NotTo(HaveOccurred())

		By("Setting up a new reconciler")
		reconciler := newReconciler(mgr, gate)

		Expect(add(mgr, reconciler, reconciler.MachineToMachineSets)).To(Succeed())

		var mgrCtx context.Context
		mgrCtx, mgrCtxCancel = context.WithCancel(ctx)
		mgrStopped = make(chan struct{})

		By("Starting the manager")
		go func() {
			defer GinkgoRecover()
			defer close(mgrStopped)

			Expect(mgr.Start(mgrCtx)).To(Succeed())
		}()

		By("Creating the namespace")
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "ms-test"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		By("Setting up the machine set builder")
		machineSetBuilder = machinev1resourcebuilder.MachineSet().
			WithNamespace(namespace.ObjectMeta.Name).
			WithName("foo").
			WithReplicas(replicas).
			WithLabels(map[string]string{"foo": "bar"})

	})

	AfterEach(func() {
		By("Deleting the machinesets")
		Expect(cleanResources(namespace.Name)).To(Succeed())

		By("Deleting the namespace")
		Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())

		By("Closing the manager")
		mgrCtxCancel()
		Eventually(mgrStopped, timeout).WithTimeout(20 * time.Second).Should(BeClosed())
	})

	It("Should reconcile a MachineSet", func() {
		instance := machineSetBuilder.Build()

		By("Creating the MachineSet")
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		machines := &machinev1.MachineList{}
		By("Verifying that we have 2 replicas")
		Eventually(func() (int, error) {
			if err := k8sClient.List(ctx, machines, client.InNamespace(namespace.Name)); err != nil {
				return 0, err
			}
			return len(machines.Items), nil
		}, timeout).Should(BeEquivalentTo(replicas))

		By("Trying to delete 1 machine and check the MachineSet scales back up")
		machineToBeDeleted := machines.Items[0]
		Expect(k8sClient.Delete(ctx, &machineToBeDeleted)).To(Succeed())

		By("Verifying that we have 2 replicas")
		Eventually(func() (int, error) {
			ready := 0
			if err := k8sClient.List(ctx, machines, client.InNamespace(namespace.Name)); err != nil {
				return 0, err
			}
			for _, m := range machines.Items {
				if !m.DeletionTimestamp.IsZero() {
					continue
				}
				ready++
			}
			return ready, nil
		}, timeout*3).Should(BeEquivalentTo(replicas))
	})
})

func cleanResources(namespace string) error {
	machineSet := &machinev1.MachineSet{}
	if err := k8sClient.DeleteAllOf(ctx, machineSet, client.InNamespace(namespace)); err != nil {
		return err
	}

	machine := &machinev1.Machine{}
	if err := k8sClient.DeleteAllOf(ctx, machine, client.InNamespace(namespace)); err != nil {
		return err
	}

	return nil
}
