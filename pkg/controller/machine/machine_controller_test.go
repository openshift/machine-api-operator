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
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("MachineSet Reconciler", func() {
	var mgrCtxCancel context.CancelFunc
	var mgrStopped chan struct{}
	var k komega.Komega
	var namespace *corev1.Namespace
	var reconciler reconcile.Reconciler

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
		reconciler = newReconciler(mgr, act)

		err = add(mgr, reconciler, "testing")
		Expect(err).NotTo(HaveOccurred())

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

	It("Should reconcile a Machine", func() {
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

		By("Creating the Machine")
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("Setting the AuthoritativeAPI to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMachineAPI
		})).Should(Succeed())

		Eventually(func() (machinev1.MachineStatus, error) {
			machine := &machinev1.Machine{}
			err := k8sClient.Get(ctx, objectKey(instance), machine)
			if err != nil {
				return machinev1.MachineStatus{}, err
			}
			return machine.Status, nil
		}, timeout).ShouldNot(Equal(machinev1.MachineStatus{}))
		// TODO: Verify that the actuator is called correctly on Create
		// Expect platform status is not empty. This (check) means we've caLLED the actuator
	})

	It("Should set the Paused condition appropriately", func() {
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

		By("Creating the Machine")
		Expect(k8sClient.Create(ctx, instance)).To(Succeed())

		By("Setting the AuthoritativeAPI to ClusterAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityClusterAPI
		})).Should(Succeed())

		By("Verifying that the AuthoritativeAPI is set to Cluster API")
		Eventually(func() (machinev1.MachineAuthority, error) {
			if err := k8sClient.Get(ctx, objectKey(instance), instance); err != nil {
				return "", err
			}
			return instance.Status.AuthoritativeAPI, nil
		}, timeout).Should(Equal(machinev1.MachineAuthorityClusterAPI))

		By("Verifying the paused condition is approproately set to true")
		Eventually(func() bool {
			if err := k8sClient.Get(ctx, objectKey(instance), instance); err != nil {
				return false
			}

			// The condition is set to true
			return conditions.IsTrue(instance, PausedCondition)
		}, timeout).Should(BeTrue())

		By("Transitioning the AuthoritativeAPI though 'Migrating' to MachineAPI")
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMigrating
		})).Should(Succeed())
		Eventually(k.UpdateStatus(instance, func() {
			instance.Status.AuthoritativeAPI = machinev1.MachineAuthorityMachineAPI
		})).Should(Succeed())

		By("Verifying the paused condition is approproately set to false")
		Eventually(func() bool {
			if err := k8sClient.Get(ctx, objectKey(instance), instance); err != nil {
				return false
			}

			// The condition is set to true
			return conditions.IsFalse(instance, PausedCondition)
		}, timeout).Should(BeTrue())
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
