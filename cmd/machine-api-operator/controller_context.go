package main

import (
	"time"

	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
)

// ControllerContext stores all the informers for a variety of kubernetes objects.
type ControllerContext struct {
	ClientBuilder *ClientBuilder

	KubeNamespacedInformerFactory   informers.SharedInformerFactory
	ConfigNamespacedInformerFactory configinformersv1.SharedInformerFactory

	AvailableResources map[schema.GroupVersionResource]bool

	Stop <-chan struct{}

	InformersStarted chan struct{}

	ResyncPeriod func() time.Duration
}

// CreateControllerContext creates the ControllerContext with the ClientBuilder.
func CreateControllerContext(cb *ClientBuilder, stop <-chan struct{}, targetNamespace string) *ControllerContext {
	kubeClient := cb.KubeClientOrDie("kube-shared-informer")
	openshiftClient := cb.OpenshiftClientOrDie("openshift-shared-informer")

	kubeNamespacedSharedInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, resyncPeriod()(), informers.WithNamespace(targetNamespace))
	configNamespacesShareInformer := configinformersv1.NewSharedInformerFactoryWithOptions(openshiftClient, resyncPeriod()(), configinformersv1.WithNamespace(metav1.NamespaceNone))

	return &ControllerContext{
		ClientBuilder:                   cb,
		KubeNamespacedInformerFactory:   kubeNamespacedSharedInformer,
		ConfigNamespacedInformerFactory: configNamespacesShareInformer,
		Stop:             stop,
		InformersStarted: make(chan struct{}),
		ResyncPeriod:     resyncPeriod(),
	}
}
