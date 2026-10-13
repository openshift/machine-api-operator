/*
Copyright 2022 Red Hat, Inc.

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

package testutils

import (
	"context"
	"fmt"

	"github.com/onsi/gomega"

	corev1resourcebuilder "github.com/openshift/cluster-api-actuator-pkg/testutils/resourcebuilder/core/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest/komega"
)

// CleanupResources will clean up resources of the types of objects given in a particular namespace if given.
// The namespace will then be removed once it has been emptied.
// This utility is intended to be used in AfterEach blocks to clean up from a specific test.
// It calls various gomega assertions so will fail a test if there are any errors.
func CleanupResources(g gomega.Gomega, ctx context.Context, cfg *rest.Config, k8sClient client.Client, namespace string, objs ...client.Object) {
	for _, obj := range objs {
		cleanupResource(g, ctx, k8sClient, namespace, obj)
	}

	if namespace != "" {
		removeNamespace(g, ctx, cfg, k8sClient, namespace)
	}
}

// cleanupResource removes all of a particular resource within a namespace.
func cleanupResource(g gomega.Gomega, ctx context.Context, k8sClient client.Client, namespace string, obj client.Object) {
	removeFinalizersFromAll(g, ctx, k8sClient, namespace, obj)

	g.Eventually(func() (client.ObjectList, error) {
		if err := k8sClient.DeleteAllOf(ctx, obj, client.InNamespace(namespace)); err != nil {
			return nil, fmt.Errorf("error deleting resource list: %w", err)
		}

		listObj := newListFromObject(g, k8sClient, obj)

		return komega.ObjectList(listObj, client.InNamespace(namespace))()
	}).Should(gomega.HaveField("Items", gomega.HaveLen(0)))
}

// removeFinalizersFromAll removes any finalizers from all of the objects of the given object kind,
// in the namespace provided.
func removeFinalizersFromAll(g gomega.Gomega, ctx context.Context, k8sClient client.Client, namespace string, obj client.Object) {
	listObj := newListFromObject(g, k8sClient, obj)

	g.Expect(k8sClient.List(ctx, listObj, client.InNamespace(namespace))).Should(gomega.Succeed())

	listItems, err := apimeta.ExtractList(listObj)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	for _, item := range listItems {
		o, ok := item.(client.Object)
		g.Expect(ok).To(gomega.BeTrue())

		removeFinalizers(g, o)
	}
}

// removeFinalizers removes all finalizers from the object given.
// Finalizers must be removed one by one else the API server will reject the update.
func removeFinalizers(g gomega.Gomega, obj client.Object) {
	filter := func(finalizers []string, toRemove string) []string {
		out := []string{}

		for _, f := range finalizers {
			if f != toRemove {
				out = append(out, f)
			}
		}

		return out
	}

	for _, finalizer := range obj.GetFinalizers() {
		g.Eventually(komega.Update(obj, func() {
			obj.SetFinalizers(filter(obj.GetFinalizers(), finalizer))
		})).Should(gomega.Succeed())
	}
}

// newListFromObject converts an individual object type into a list object type to allow the
// the list to be checked for emptiness.
func newListFromObject(g gomega.Gomega, k8sClient client.Client, obj client.Object) client.ObjectList {
	objGVKs, _, err := k8sClient.Scheme().ObjectKinds(obj)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(objGVKs).To(gomega.HaveLen(1))

	listGVK := objGVKs[0]
	listGVK.Kind = fmt.Sprintf("%sList", listGVK.Kind)

	newObj, err := k8sClient.Scheme().New(listGVK)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	listObj, ok := newObj.(client.ObjectList)
	g.Expect(ok).To(gomega.BeTrue())

	return listObj
}

// removeNamespace handles the namespace finalization act that is normally performed by the garbage collector
// once it is happy that the namespace has no objects left within it.
func removeNamespace(g gomega.Gomega, ctx context.Context, cfg *rest.Config, k8sClient client.Client, namespace string) {
	coreClient, err := coreclient.NewForConfig(cfg)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	nsBuilder := corev1resourcebuilder.Namespace().WithName(namespace)

	// Delete the namespace
	ns := nsBuilder.Build()
	g.Expect(k8sClient.Delete(ctx, ns)).To(gomega.Succeed())

	// Remove the finalizer
	g.Eventually(func() error {
		if err := komega.Get(ns)(); err != nil {
			return fmt.Errorf("could not get namespace: %w", err)
		}
		ns.Spec.Finalizers = []corev1.FinalizerName{}

		_, err := coreClient.Namespaces().Finalize(ctx, ns, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("could not finalize namespace: %w", err)
		}

		return nil
	}).Should(gomega.Succeed())
}
