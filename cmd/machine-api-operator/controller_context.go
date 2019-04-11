package main

import (
	"time"

	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
)

// ControllerContext stores all the informers for a variety of kubernetes objects.
type ControllerContext struct {
	ClientBuilder *ClientBuilder

	KubeNamespacedInformerFactory informers.SharedInformerFactory
	ConfigInformerFactory         configinformersv1.SharedInformerFactory

	AvailableResources map[schema.GroupVersionResource]bool

	Stop <-chan struct{}

	InformersStarted chan struct{}

	ResyncPeriod func() time.Duration
}

// CreateControllerContext creates the ControllerContext with the ClientBuilder.
func CreateControllerContext(cb *ClientBuilder, stop <-chan struct{}, targetNamespace string) *ControllerContext {
	kubeClient := cb.KubeClientOrDie("kube-shared-informer")
	configClient := cb.OpenshiftClientOrDie("config-shared-informer")

	kubeNamespacedSharedInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, resyncPeriod()(), informers.WithNamespace(targetNamespace))
	configSharedInformer := configinformersv1.NewSharedInformerFactoryWithOptions(configClient, resyncPeriod()())

	return &ControllerContext{
		ClientBuilder:                 cb,
		KubeNamespacedInformerFactory: kubeNamespacedSharedInformer,
		ConfigInformerFactory:         configSharedInformer,
		Stop:             stop,
		InformersStarted: make(chan struct{}),
		ResyncPeriod:     resyncPeriod(),
	}
}
