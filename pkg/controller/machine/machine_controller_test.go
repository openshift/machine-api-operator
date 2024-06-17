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
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	utilnet "k8s.io/apimachinery/pkg/util/net"
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

		By("Setting up a new reconciler")
		act := newTestActuator()
		reconciler := newReconciler(mgr, act)

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
		Expect(cleanResources()).To(Succeed())

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

		By("Transitioning the AuthoritativeAPI though 'Migrating' to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMigrating
		})).Should(Succeed())

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
					if err := k8sClient.Get(ctx, objectKey(localInstance), localInstance); err != nil {
						return g.Expect(err).Should(WithTransform(isRetryableAPIError, BeTrue()), "expected temporary error while getting machine: %v", err)
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
					if err := k8sClient.Get(ctx, objectKey(localInstance), localInstance); err != nil {
						return g.Expect(err).Should(WithTransform(isRetryableAPIError, BeTrue()), "expected temporary error while getting machine: %v", err)
					}

					return g.Expect(localInstance.Status.AuthoritativeAPI).ToNot(Equal(machinev1.MachineAuthorityMachineAPI))
				})
		}()

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
	})
})

func objectKey(object metav1.Object) client.ObjectKey {
	return client.ObjectKey{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
}

func cleanResources() error {
	machines := &machinev1.MachineList{}
	if err := k8sClient.List(ctx, machines); err != nil {
		return err
	}
	for _, machine := range machines.Items {
		m := machine
		if err := k8sClient.Delete(ctx, &m); err != nil {
			return err
		}
	}

	return nil
}

// isRetryableAPIError returns whether an API error is retryable or not.
// inspired by: k8s.io/kubernetes/test/utils.
func isRetryableAPIError(err error) bool {
	// These errors may indicate a transient error that we can retry in tests.
	if apierrs.IsInternalError(err) || apierrs.IsTimeout(err) || apierrs.IsServerTimeout(err) ||
		apierrs.IsTooManyRequests(err) || utilnet.IsProbableEOF(err) || utilnet.IsConnectionReset(err) ||
		utilnet.IsHTTP2ConnectionLost(err) {
		return true
	}

	// If the error sends the Retry-After header, we respect it as an explicit confirmation we should retry.
	if _, shouldRetry := apierrs.SuggestsClientDelay(err); shouldRetry {
		return true
	}

	return false
}
