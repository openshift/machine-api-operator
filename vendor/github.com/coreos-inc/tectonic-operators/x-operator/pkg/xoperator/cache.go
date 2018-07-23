package xoperator

import (
	"time"

	"github.com/golang/glog"
	extensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/externalversions"
	v1beta1listers "k8s.io/apiextensions-apiserver/pkg/client/listers/apiextensions/v1beta1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	appsv1beta2informers "k8s.io/client-go/listers/apps/v1beta2"
	v1informers "k8s.io/client-go/listers/core/v1"
	extensionsv1beta1informers "k8s.io/client-go/listers/extensions/v1beta1"
	networkingv1informers "k8s.io/client-go/listers/networking/v1"
	rbacinformers "k8s.io/client-go/listers/rbac/v1"
	aggregatorinformers "k8s.io/kube-aggregator/pkg/client/informers/externalversions"
	apiregistrationv1beta1listers "k8s.io/kube-aggregator/pkg/client/listers/apiregistration/v1beta1"

	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
)

const (
	cacheSyncPollInterval = 100 * time.Millisecond
	cacheSyncDeadline     = 1 * time.Minute
	cacheResyncPeriod     = 10 * time.Second
)

// cache contains listers for all the components that are supported by x-operator.
type cache struct {
	apiServices                   apiregistrationv1beta1listers.APIServiceLister
	clusterRoles                  rbacinformers.ClusterRoleLister
	clusterRoleBindings           rbacinformers.ClusterRoleBindingLister
	configmaps                    v1informers.ConfigMapLister
	customResourceDefinitionKinds v1beta1listers.CustomResourceDefinitionLister
	daemonsets                    appsv1beta2informers.DaemonSetLister
	deployments                   appsv1beta2informers.DeploymentLister
	ingresss                      extensionsv1beta1informers.IngressLister
	networkPolicys                networkingv1informers.NetworkPolicyLister
	roles                         rbacinformers.RoleLister
	roleBindings                  rbacinformers.RoleBindingLister
	secrets                       v1informers.SecretLister
	services                      v1informers.ServiceLister
	serviceAccounts               v1informers.ServiceAccountLister
}

// setupCache initializes the cache object.
func setupCache(client opclient.Interface, stop <-chan struct{}) *cache {
	sharedInformer := informers.NewSharedInformerFactory(client.KubernetesInterface(), cacheResyncPeriod)
	cfInformer := sharedInformer.Core().V1().ConfigMaps()
	crInformer := sharedInformer.Rbac().V1().ClusterRoles()
	crbInformer := sharedInformer.Rbac().V1().ClusterRoleBindings()
	dInformer := sharedInformer.Apps().V1beta2().Deployments()
	dsInformer := sharedInformer.Apps().V1beta2().DaemonSets()
	igInformer := sharedInformer.Extensions().V1beta1().Ingresses()
	npInformer := sharedInformer.Networking().V1().NetworkPolicies()
	rInformer := sharedInformer.Rbac().V1().Roles()
	rbInformer := sharedInformer.Rbac().V1().RoleBindings()
	saInformer := sharedInformer.Core().V1().ServiceAccounts()
	seInformer := sharedInformer.Core().V1().Secrets()
	svcInformer := sharedInformer.Core().V1().Services()

	extensionsInformer := extensionsinformers.NewSharedInformerFactory(client.ApiextensionsV1beta1Interface(), cacheResyncPeriod)
	crdkInformer := extensionsInformer.Apiextensions().V1beta1().CustomResourceDefinitions()

	aggregatorsInformer := aggregatorinformers.NewSharedInformerFactory(client.KubeAggregatorInterface(), cacheResyncPeriod)
	apisvcInformer := aggregatorsInformer.Apiregistration().V1beta1().APIServices()

	c := &cache{
		apiServices:         apisvcInformer.Lister(),
		daemonsets:          dsInformer.Lister(),
		deployments:         dInformer.Lister(),
		configmaps:          cfInformer.Lister(),
		services:            svcInformer.Lister(),
		ingresss:            igInformer.Lister(),
		secrets:             seInformer.Lister(),
		serviceAccounts:     saInformer.Lister(),
		clusterRoleBindings: crbInformer.Lister(),
		clusterRoles:        crInformer.Lister(),
		roleBindings:        rbInformer.Lister(),
		networkPolicys:      npInformer.Lister(),
		roles:               rInformer.Lister(),
		customResourceDefinitionKinds: crdkInformer.Lister(),
	}

	go sharedInformer.Start(stop)
	go extensionsInformer.Start(stop)
	go aggregatorsInformer.Start(stop)

	waitForCacheSyncOrDie(
		apisvcInformer.Informer().HasSynced,
		cfInformer.Informer().HasSynced,
		crInformer.Informer().HasSynced,
		crbInformer.Informer().HasSynced,
		crdkInformer.Informer().HasSynced,
		dInformer.Informer().HasSynced,
		dsInformer.Informer().HasSynced,
		igInformer.Informer().HasSynced,
		npInformer.Informer().HasSynced,
		rInformer.Informer().HasSynced,
		rbInformer.Informer().HasSynced,
		saInformer.Informer().HasSynced,
		seInformer.Informer().HasSynced,
		svcInformer.Informer().HasSynced,
	)

	return c
}

func waitForCacheSyncOrDie(cacheSyncs ...func() bool) {
	err := wait.Poll(cacheSyncPollInterval, cacheSyncDeadline, func() (bool, error) {
		for _, syncFunc := range cacheSyncs {
			if !syncFunc() {
				return false, nil
			}
		}
		return true, nil
	})
	if err != nil {
		glog.Fatalf("timeout waiting for caches to sync: %v", err)
	}
}
