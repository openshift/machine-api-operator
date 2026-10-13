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
	"github.com/openshift/machine-api-operator/pkg/controller/machine"

	testutils "github.com/openshift/cluster-api-actuator-pkg/testutils"
	"github.com/openshift/cluster-control-plane-machine-set-operator/test/e2e/framework"
	"github.com/openshift/machine-api-operator/pkg/util/testing"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("MachineSet Reconciler", func() {
	var mgrCtxCancel context.CancelFunc
	var mgrStopped chan struct{}
	var k komega.Komega
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
		k = komega.New(k8sClient)

		By("Setting up feature gates")
		gate, err := testing.NewDefaultMutableFeatureGate()
		Expect(err).NotTo(HaveOccurred())

		By("Setting up a new reconciler")
		reconciler := newReconciler(mgr, gate)

		Expect(addWithOpts(mgr, controller.Options{
			Reconciler:         reconciler,
			SkipNameValidation: ptr.To(true),
		}, reconciler.MachineToMachineSets)).To(Succeed())

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
			WithGenerateName("foo").
			WithReplicas(replicas).
			WithMachineTemplateLabels(map[string]string{"foo": "bar"})
	})

	AfterEach(func() {
		By("Closing the manager")
		mgrCtxCancel()
		Eventually(mgrStopped, timeout).WithTimeout(20 * time.Second).Should(BeClosed())

		By("Cleaning up test resources")
		testutils.CleanupResources(Default, ctx, cfg, k8sClient, namespace.GetName(),
			&machinev1.Machine{},
			&machinev1.MachineSet{},
		)
	})

	It("Should reconcile a MachineSet", func() {
		instance := machineSetBuilder.WithAuthoritativeAPI(machinev1.MachineAuthorityMachineAPI).Build()

		By("Creating the MachineSet")
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("Verying the MachineSet has been reconciled (status set)")
		Eventually(k.Object(instance), timeout).Should(HaveField("Status", Not(Equal(machinev1.MachineStatus{}))))

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

	Describe("Paused Condition", func() {
		var instance *machinev1.MachineSet
		BeforeEach(func() {
			instance = machineSetBuilder.
				WithGenerateName("baz-").
				WithMachineTemplateLabels(map[string]string{"baz": "bar"}).
				WithAuthoritativeAPI(machinev1.MachineAuthorityClusterAPI).
				Build()
		})
		It("Should set the Paused condition appropriately", func() {
			By("Creating the MachineSet (spec.authoritativeAPI=ClusterAPI)")
			Expect(k8sClient.Create(ctx, instance)).To(Succeed())

			By("Verifying the status.authoritativeAPI has been set to ClusterAPI and paused condition is appropriately set to true")
			Eventually(k.Object(instance), timeout).Should(SatisfyAll(
				HaveField("Status.Conditions", ContainElement(SatisfyAll(
					HaveField("Type", Equal(machine.PausedCondition)),
					HaveField("Status", Equal(corev1.ConditionTrue)),
					HaveField("Message", Equal("The AuthoritativeAPI status is set to 'ClusterAPI'")),
				))),
				HaveField("Status.AuthoritativeAPI", Equal(machinev1.MachineAuthorityClusterAPI)),
			))

			// The condition should remain true whilst transitioning through 'Migrating'
			// Run this in a goroutine so we don't block

			// Copy the instance before starting the goroutine, to avoid data races
			instanceCopy := instance.DeepCopy()
			go func() {
				defer GinkgoRecover()
				framework.RunCheckUntil(
					ctx,
					// Check that we consistently have the Paused condition true
					func(_ context.Context, g framework.GomegaAssertions) bool {
						By("Checking that the paused condition is consistently true whilst migrating to MachineAPI")

						localInstance := instanceCopy.DeepCopy()
						if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(localInstance), localInstance); err != nil {
							return g.Expect(err).Should(WithTransform(testing.IsRetryableAPIError, BeTrue()), "expected temporary error while getting machine: %v", err)
						}

						return g.Expect(localInstance.Status.Conditions).Should(ContainElement(SatisfyAll(
							HaveField("Type", Equal(machine.PausedCondition)),
							HaveField("Status", Equal(corev1.ConditionTrue)),
						)))

					},
					// Condition / until function: until we observe the MachineAuthority being MAPI
					func(_ context.Context, g framework.GomegaAssertions) bool {
						By("Checking that status.authoritativeAPI is not MachineAPI")

						localInstance := instanceCopy.DeepCopy()
						if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(localInstance), localInstance); err != nil {
							return g.Expect(err).Should(WithTransform(testing.IsRetryableAPIError, BeTrue()), "expected temporary error while getting machine: %v", err)
						}

						return g.Expect(localInstance.Status.AuthoritativeAPI).To(Equal(machinev1.MachineAuthorityMachineAPI))
					})
			}()

			By("Changing status.authoritativeAPI from ClusterAPI to Migrating")
			Eventually(k.UpdateStatus(instance, func() {
				instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMigrating
			})).Should(Succeed())

			By("Verifying the paused condition is appropriately set to true while in Migrating")
			Eventually(k.Object(instance), timeout).Should(SatisfyAll(
				HaveField("Status.Conditions", ContainElement(SatisfyAll(
					HaveField("Type", Equal(machine.PausedCondition)),
					HaveField("Status", Equal(corev1.ConditionTrue)),
					HaveField("Message", Equal("The AuthoritativeAPI status is set to 'Migrating'")),
				))),
				HaveField("Status.AuthoritativeAPI", Equal(machinev1.MachineAuthorityMigrating)),
			))

			By("Changing status.authoritativeAPI from Migrating to MachineAPI")
			Eventually(k.UpdateStatus(instance, func() {
				instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMachineAPI
			})).Should(Succeed())

			By("Verifying the paused condition is appropriately set to false when in MachineAPI")
			Eventually(k.Object(instance), timeout).Should(SatisfyAll(
				HaveField("Status.Conditions", ContainElement(SatisfyAll(
					HaveField("Type", Equal(machine.PausedCondition)),
					HaveField("Status", Equal(corev1.ConditionFalse)),
					HaveField("Message", Equal("The AuthoritativeAPI status is set to 'MachineAPI'")),
				))),
				HaveField("Status.AuthoritativeAPI", Equal(machinev1.MachineAuthorityMachineAPI)),
			))
		})
	})
})
