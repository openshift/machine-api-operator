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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	machinev1resourcebuilder "github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder/machine/v1beta1"
	"github.com/openshift/cluster-control-plane-machine-set-operator/test/e2e/framework"
	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ = Describe("Machine Reconciler", func() {
	var mgrCtxCancel context.CancelFunc
	var mgrStopped chan struct{}
	var k komega.Komega
	var namespace *corev1.Namespace
	var machineBuilder machinev1resourcebuilder.MachineBuilder

	BeforeEach(func() {
		By("Setting up a new manager")
		mgr, err := manager.New(cfg, manager.Options{
			Metrics: server.Options{
				BindAddress: "0",
			},
		})
		Expect(err).NotTo(HaveOccurred())

		k8sClient = mgr.GetClient()
		k = komega.New(k8sClient)

		By("Setting up feature gates")
		gate, err := testutils.NewDefaultMutableFeatureGate()
		Expect(err).NotTo(HaveOccurred())

		By("Setting up a new reconciler")
		act := newTestActuator()
		reconciler := newReconciler(mgr, act, gate)

		Expect(add(mgr, reconciler, "testing")).To(Succeed())

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
		namespace = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "machine-test"}}
		Expect(k8sClient.Create(ctx, namespace)).To(Succeed())

		By("Setting up the machine builder")
		machineBuilder = machinev1resourcebuilder.Machine().
			WithNamespace(namespace.ObjectMeta.Name).
			WithGenerateName("foo").
			WithProviderSpecBuilder(machinev1resourcebuilder.GCPProviderSpec())

	})

	AfterEach(func() {
		By("Deleting the machines")
		Expect(cleanResources(namespace.Name)).To(Succeed())

		By("Deleting the namespace")
		Expect(k8sClient.Delete(ctx, namespace)).To(Succeed())

		By("Closing the manager")
		mgrCtxCancel()
		Eventually(mgrStopped, timeout).WithTimeout(20 * time.Second).Should(BeClosed())
	})

	It("Should reconcile a Machine", func() {
		instance := machineBuilder.Build()

		By("Creating the Machine")
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("Setting the AuthoritativeAPI to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMachineAPI
		})).Should(Succeed())

		Eventually(k.Object(instance), timeout).Should(HaveField("Status", Not(Equal(machinev1.MachineStatus{}))))

		// // Expect platform status is not empty. This (check) means we've called the
		// // actuator. Given our test actuator doesn't set any status, we may need to expect to be called.

		// Eventually(k.Object(instance), timeout).Should(HaveField("Status.ProviderStatus", Not(Equal(runtime.RawExtension{}))))

	})

	It("Should set the Paused condition appropriately", func() {
		instance := machineBuilder.Build()

		By("Creating the Machine")
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("Setting the AuthoritativeAPI to ClusterAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityClusterAPI
		})).Should(Succeed())

		By("Verifying that the AuthoritativeAPI is set to Cluster API")
		Eventually(k.Object(instance), timeout).Should(HaveField("Status.AuthoritativeAPI", Equal(machinev1.MachineAuthorityClusterAPI)))

		By("Verifying the paused condition is approproately set to true")
		Eventually(k.Object(instance), timeout).Should(HaveField("Status.Conditions", ContainElement(SatisfyAll(
			HaveField("Type", Equal(PausedCondition)),
			HaveField("Status", Equal(corev1.ConditionTrue)),
		))))

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
						return g.Expect(err).Should(WithTransform(testutils.IsRetryableAPIError, BeTrue()), "expected temporary error while getting machine: %v", err)
					}

					return g.Expect(localInstance.Status.Conditions).Should(ContainElement(SatisfyAll(
						HaveField("Type", Equal(PausedCondition)),
						HaveField("Status", Equal(corev1.ConditionTrue)),
					)))

				},
				// Condition / until function: until we observe the MachineAuthority being MAPI
				func(_ context.Context, g framework.GomegaAssertions) bool {
					By("Checking that the AuthoritativeAPI is not MachineAPI")

					localInstance := instanceCopy.DeepCopy()
					if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(localInstance), localInstance); err != nil {
						return g.Expect(err).Should(WithTransform(testutils.IsRetryableAPIError, BeTrue()), "expected temporary error while getting machine: %v", err)
					}

					return g.Expect(localInstance.Status.AuthoritativeAPI).To(Equal(machinev1.MachineAuthorityMachineAPI))
				})
		}()

		By("Transitioning the AuthoritativeAPI though 'Migrating' to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMigrating
		})).Should(Succeed())

		By("Updating the AuthoritativeAPI from Migrating to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMachineAPI
		})).Should(Succeed())

		Eventually(k.Object(instance), timeout).Should(HaveField("Status.AuthoritativeAPI", Equal(machinev1.MachineAuthorityMachineAPI)))

		By("Verifying the paused condition is approproately set to false")
		Eventually(k.Object(instance), timeout).Should(HaveField("Status.Conditions", ContainElement(SatisfyAll(
			HaveField("Type", Equal(PausedCondition)),
			HaveField("Status", Equal(corev1.ConditionFalse)),
		))))

		By("Unsetting the AuthoritativeAPI field in the status")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = ""
		})).Should(Succeed())

		By("Verifying the paused condition is still approproately set to false")
		Eventually(k.Object(instance), timeout).Should(HaveField("Status.Conditions", ContainElement(SatisfyAll(
			HaveField("Type", Equal(PausedCondition)),
			HaveField("Status", Equal(corev1.ConditionFalse)),
		))))
	})
})

func cleanResources(namespace string) error {
	machine := &machinev1.Machine{}
	if err := k8sClient.DeleteAllOf(ctx, machine, client.InNamespace(namespace)); err != nil {
		return err
	}

	return nil
}
