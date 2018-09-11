package common

import (
	"time"

	apiextinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	clusterapiinformers "sigs.k8s.io/cluster-api/pkg/client/informers_generated/externalversions"
)

// ControllerContext stores all the informers for a variety of kubernetes objects.
type ControllerContext struct {
	ClientBuilder *ClientBuilder

	NamespacedInformerFactory     clusterapiinformers.SharedInformerFactory
	KubeInformerFactory           informers.SharedInformerFactory
	KubeNamespacedInformerFactory informers.SharedInformerFactory
	APIExtInformerFactory         apiextinformers.SharedInformerFactory

	AvailableResources map[schema.GroupVersionResource]bool

	Stop <-chan struct{}

	InformersStarted chan struct{}

	KubeInformersStarted chan struct{}

	ResyncPeriod func() time.Duration
}

// CreateControllerContext creates the ControllerContext with the ClientBuilder.
func CreateControllerContext(cb *ClientBuilder, stop <-chan struct{}, targetNamespace string) *ControllerContext {
	clusterAPIClient := cb.ClusterAPIClientOrDie("cluster-api-shared-informer")
	kubeClient := cb.KubeClientOrDie("kube-shared-informer")
	apiExtClient := cb.APIExtClientOrDie("apiext-shared-informer")

	sharedNamespacedInformers := clusterapiinformers.NewFilteredSharedInformerFactory(clusterAPIClient, resyncPeriod()(), targetNamespace, nil)

	kubeSharedInformer := informers.NewSharedInformerFactory(kubeClient, resyncPeriod()())
	kubeNamespacedSharedInformer := informers.NewFilteredSharedInformerFactory(kubeClient, resyncPeriod()(), targetNamespace, nil)
	apiExtSharedInformer := apiextinformers.NewSharedInformerFactory(apiExtClient, resyncPeriod()())

	return &ControllerContext{
		ClientBuilder:                 cb,
		NamespacedInformerFactory:     sharedNamespacedInformers,
		KubeInformerFactory:           kubeSharedInformer,
		KubeNamespacedInformerFactory: kubeNamespacedSharedInformer,
		APIExtInformerFactory:         apiExtSharedInformer,
		Stop:                 stop,
		InformersStarted:     make(chan struct{}),
		KubeInformersStarted: make(chan struct{}),
		ResyncPeriod:         resyncPeriod(),
	}
}
