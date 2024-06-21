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

	configv1 "github.com/openshift/api/config/v1"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	machinev1resourcebuilder "github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder/machine/v1beta1"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"

	"github.com/openshift/cluster-control-plane-machine-set-operator/test/e2e/framework"
	corev1 "k8s.io/api/core/v1"
	apierrs "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

		k8sClient = mgr.GetClient()
		k = komega.New(k8sClient)

		By("Setting up feature gates")
		featureGateAccessor := featuregates.NewHardcodedFeatureGateAccess(
			[]configv1.FeatureGateName{
				"MachineAPIMigration",
			},
			[]configv1.FeatureGateName{})

		By("Setting up a new reconciler")
		reconciler := newReconciler(mgr, featureGateAccessor)

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
		Expect(cleanResources()).To(Succeed())

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

		By("Setting the AuthoritativeAPI to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMachineAPI
		})).Should(Succeed())

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

	It("Should set the Paused condition appropriately", func() {
		instance := machineSetBuilder.
			WithName("baz").
			WithLabels(map[string]string{"baz": "bar"}).
			Build()

		By("Creating the MachineSet")
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

func cleanResources() error {
	machineSets := &machinev1.MachineSetList{}
	if err := k8sClient.List(ctx, machineSets); err != nil {
		return err
	}
	for _, machineSet := range machineSets.Items {
		ms := machineSet
		if err := k8sClient.Delete(ctx, &ms); err != nil {
			return err
		}
	}

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

	clusterVersions := &configv1.ClusterVersionList{}
	if err := k8sClient.List(ctx, clusterVersions); err != nil {
		return err
	}
	for _, clusterVersion := range clusterVersions.Items {
		cv := clusterVersion
		if err := k8sClient.Delete(ctx, &cv); err != nil {
			return err
		}
	}

	featureGates := &configv1.FeatureGateList{}
	if err := k8sClient.List(ctx, featureGates); err != nil {
		return err
	}
	for _, gate := range featureGates.Items {
		fg := gate
		if err := k8sClient.Delete(ctx, &fg); err != nil {
			return err
		}
	}

	return nil
}

func objectKey(object metav1.Object) client.ObjectKey {
	return client.ObjectKey{
		Namespace: object.GetNamespace(),
		Name:      object.GetName(),
	}
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
