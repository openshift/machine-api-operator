package operator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	osconfigv1 "github.com/openshift/api/config/v1"
	apifeatures "github.com/openshift/api/features"
	osoperatorv1 "github.com/openshift/api/operator/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	machineclientset "github.com/openshift/client-go/machine/clientset/versioned"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	admissioninformersv1 "k8s.io/client-go/informers/admissionregistration/v1"
	appsinformersv1 "k8s.io/client-go/informers/apps/v1"
	"k8s.io/client-go/kubernetes"
	admissionlisterv1 "k8s.io/client-go/listers/admissionregistration/v1"
	appslisterv1 "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// maxRetries is the number of times a key will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the times
	// a machineconfig pool is going to be requeued:
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries         = 15
	maoOwnedAnnotation = "machine.openshift.io/owned"

	releaseVersionEnvVariableName = "RELEASE_VERSION"
	releaseVersionUnknownValue    = "unknown"
)

// Operator defines machine api operator.
type Operator struct {
	namespace, name string

	imagesFile string
	config     string

	kubeClient    kubernetes.Interface
	osClient      osclientset.Interface
	machineClient machineclientset.Interface
	dynamicClient dynamic.Interface
	eventRecorder record.EventRecorder
	recorder      events.Recorder

	syncHandler func(ic string) (reconcile.Result, error)

	deployLister       appslisterv1.DeploymentLister
	deployListerSynced cache.InformerSynced

	daemonsetLister       appslisterv1.DaemonSetLister
	daemonsetListerSynced cache.InformerSynced

	proxyLister       configlistersv1.ProxyLister
	proxyListerSynced cache.InformerSynced

	cache                         resourceapply.ResourceCache
	validatingWebhookLister       admissionlisterv1.ValidatingWebhookConfigurationLister
	validatingWebhookListerSynced cache.InformerSynced
	mutatingWebhookLister         admissionlisterv1.MutatingWebhookConfigurationLister
	mutatingWebhookListerSynced   cache.InformerSynced

	featureGateAccessor featuregates.FeatureGateAccess

	// queue only ever has one item, but it has nice error handling backoff/retry semantics
	queue           workqueue.RateLimitingInterface
	operandVersions []osconfigv1.OperandVersion

	generations []osoperatorv1.GenerationStatus
}

// New returns a new machine config operator.
func New(
	namespace, name string,
	imagesFile string,

	config string,

	deployInformer appsinformersv1.DeploymentInformer,
	daemonsetInformer appsinformersv1.DaemonSetInformer,
	clusterOperatorInformer configinformersv1.ClusterOperatorInformer,
	featureGateInformer configinformersv1.FeatureGateInformer,
	clusterVersionInformer configinformersv1.ClusterVersionInformer,
	validatingWebhookInformer admissioninformersv1.ValidatingWebhookConfigurationInformer,
	mutatingWebhookInformer admissioninformersv1.MutatingWebhookConfigurationInformer,
	proxyInformer configinformersv1.ProxyInformer,
	kubeClient kubernetes.Interface,
	osClient osclientset.Interface,
	machineClient machineclientset.Interface,
	dynamicClient dynamic.Interface,

	eventRecorder record.EventRecorder,
	recorder events.Recorder,
) (*Operator, error) {
	// we must report the version from the release payload when we report available at that level
	// TODO we will report the version of the operands (so our machine api implementation version)
	operandVersions := []osconfigv1.OperandVersion{}
	releaseVersion := os.Getenv(releaseVersionEnvVariableName)
	if len(releaseVersion) > 0 {
		operandVersions = append(operandVersions, osconfigv1.OperandVersion{Name: "operator", Version: releaseVersion})
	} else {
		klog.Infof("%s environment variable is missing, defaulting to %q", releaseVersionEnvVariableName, releaseVersionUnknownValue)
		releaseVersion = releaseVersionUnknownValue
	}

	optr := &Operator{
		namespace:       namespace,
		name:            name,
		imagesFile:      imagesFile,
		kubeClient:      kubeClient,
		osClient:        osClient,
		machineClient:   machineClient,
		dynamicClient:   dynamicClient,
		eventRecorder:   eventRecorder,
		recorder:        recorder,
		queue:           workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineapioperator"),
		operandVersions: operandVersions,
	}

	_, err := deployInformer.Informer().AddEventHandler(optr.eventHandlerDeployments())
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to deployments informer: %v", err)
	}
	_, err = daemonsetInformer.Informer().AddEventHandler(optr.eventHandler())
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to daemonsets informer: %v", err)
	}
	_, err = validatingWebhookInformer.Informer().AddEventHandler(optr.eventHandlerSingleton(isMachineWebhook))
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to validatingwebhook informer: %v", err)
	}
	_, err = mutatingWebhookInformer.Informer().AddEventHandler(optr.eventHandlerSingleton(isMachineWebhook))
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to mutatingwebhook informer: %v", err)
	}
	_, err = clusterOperatorInformer.Informer().AddEventHandler(optr.eventHandlerSingleton(isSelfClusterOperator))
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to clusteroperator informer: %v", err)
	}

	desiredVersion := releaseVersion
	missingVersion := "0.0.1-snapshot"
	featureGateAccessor := featuregates.NewFeatureGateAccess(
		desiredVersion, missingVersion,
		clusterVersionInformer, featureGateInformer,
		recorder,
	)
	featureGateAccessor.SetChangeHandler(func(featureChange featuregates.FeatureChange) {
		if featureChange.Previous == nil {
			// When the initial featuregate is set, the previous version is nil.
			// Nothing to do in this case, it's handled by the 1st sync, which only runs after the initial feature set was received.
			return
		}

		klog.V(4).InfoS("FeatureGates changed", "enabled", featureChange.New.Enabled, "disabled", featureChange.New.Disabled)
		prevDisableMHC := featuregates.NewFeatureGate(featureChange.Previous.Enabled, featureChange.Previous.Disabled).
			Enabled(apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController)
		newDisableMHC := featuregates.NewFeatureGate(featureChange.New.Enabled, featureChange.New.Disabled).
			Enabled(apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController)

		if prevDisableMHC != newDisableMHC {
			klog.V(2).InfoS("Resync for modified feature gate",
				"FeatureGateMachineAPIOperatorDisableMachineHealthCheckController enabled", newDisableMHC,
			)
			workQueueKey := fmt.Sprintf("%s/%s", optr.namespace, optr.name)
			optr.queue.Add(workQueueKey)
		}
	})
	optr.featureGateAccessor = featureGateAccessor

	optr.config = config
	optr.syncHandler = optr.sync

	optr.deployLister = deployInformer.Lister()
	optr.deployListerSynced = deployInformer.Informer().HasSynced

	optr.daemonsetLister = daemonsetInformer.Lister()
	optr.daemonsetListerSynced = daemonsetInformer.Informer().HasSynced

	optr.proxyLister = proxyInformer.Lister()
	optr.proxyListerSynced = proxyInformer.Informer().HasSynced

	optr.cache = resourceapply.NewResourceCache()
	optr.validatingWebhookLister = validatingWebhookInformer.Lister()
	optr.validatingWebhookListerSynced = validatingWebhookInformer.Informer().HasSynced
	optr.mutatingWebhookLister = mutatingWebhookInformer.Lister()
	optr.mutatingWebhookListerSynced = mutatingWebhookInformer.Informer().HasSynced

	return optr, nil
}

// Run runs the machine config operator.
func (optr *Operator) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer optr.queue.ShutDown()

	klog.Info("Starting Machine API Operator")
	defer klog.Info("Shutting down Machine API Operator")

	if !cache.WaitForCacheSync(stopCh,
		optr.mutatingWebhookListerSynced,
		optr.validatingWebhookListerSynced,
		optr.deployListerSynced,
		optr.daemonsetListerSynced,
		optr.proxyListerSynced) {
		klog.Error("Failed to sync caches")
		return
	}
	klog.Info("Synced up caches")

	ctx, cancelFeatureGateAccessor := context.WithCancel(context.Background())
	defer cancelFeatureGateAccessor()
	go optr.featureGateAccessor.Run(ctx)
	klog.Info("Started feature gate accessor")
	select {
	case <-optr.featureGateAccessor.InitialFeatureGatesObserved():
		klog.V(4).Info("FeatureGates initialized")
	case <-time.After(1 * time.Minute):
		klog.Error(errors.New("timed out waiting for FeatureGate detection"), "unable to start operator")
		return
	}

	for i := 0; i < workers; i++ {
		go wait.Until(optr.worker, time.Second, stopCh)
	}

	<-stopCh
}

func logResource(obj interface{}) {
	metaObj, okObject := obj.(metav1.Object)
	if !okObject {
		klog.Errorf("Error assigning type to interface when logging")
	}
	klog.V(4).Infof("Resource type: %T", obj)
	klog.V(4).Infof("Resource: %v", metaObj.GetSelfLink())
}

func (optr *Operator) eventHandler() cache.ResourceEventHandler {
	workQueueKey := fmt.Sprintf("%s/%s", optr.namespace, optr.name)
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			klog.V(4).Infof("Event: Add")
			logResource(obj)
			optr.queue.Add(workQueueKey)
		},
		UpdateFunc: func(old, new interface{}) {
			klog.V(4).Infof("Event: Update")
			logResource(old)
			optr.queue.Add(workQueueKey)
		},
		DeleteFunc: func(obj interface{}) {
			klog.V(4).Infof("Event: Delete")
			logResource(obj)
			optr.queue.Add(workQueueKey)
		},
	}
}

// on deployments we only reconcile on update/delete events if its owned by mao
func (optr *Operator) eventHandlerDeployments() cache.ResourceEventHandler {
	workQueueKey := fmt.Sprintf("%s/%s", optr.namespace, optr.name)
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			klog.V(4).Infof("Event: Add")
			logResource(obj)
			optr.queue.Add(workQueueKey)
		},
		UpdateFunc: func(old, new interface{}) {
			klog.V(4).Infof("Event: Update")
			logResource(old)
			if owned, err := isOwned(old); !owned || err != nil {
				return
			}
			optr.queue.Add(workQueueKey)
		},
		DeleteFunc: func(obj interface{}) {
			klog.V(4).Infof("Event: Delete")
			logResource(obj)
			if owned, err := isOwned(obj); !owned || err != nil {
				return
			}
			optr.queue.Add(workQueueKey)
		},
	}
}

func isOwned(obj interface{}) (bool, error) {
	metaObj, okObject := obj.(metav1.Object)
	if !okObject {
		klog.Errorf("Error assigning metav1.Object type to object: %T", obj)
		return false, fmt.Errorf("error assigning metav1.Object type to object: %T", obj)
	}
	_, ok := metaObj.GetAnnotations()[maoOwnedAnnotation]
	return ok, nil
}

func (optr *Operator) eventHandlerSingleton(f func(interface{}) bool) cache.FilteringResourceEventHandler {
	workQueueKey := fmt.Sprintf("%s/%s", optr.namespace, optr.name)
	addToQueue := func(obj interface{}) {
		logResource(obj)
		optr.queue.Add(workQueueKey)
	}

	return cache.FilteringResourceEventHandler{
		FilterFunc: f,
		Handler: cache.ResourceEventHandlerFuncs{
			AddFunc:    addToQueue,
			DeleteFunc: addToQueue,
			UpdateFunc: func(old, new interface{}) {
				addToQueue(new)
			},
		},
	}
}

func isMachineWebhook(obj interface{}) bool {
	mutatingWebhook, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration)
	if ok {
		return mutatingWebhook.Name == "machine-api"
	}

	validatingWebhook, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
	if ok {
		return validatingWebhook.Name == "machine-api"
	}

	return false
}

func isSelfClusterOperator(obj interface{}) bool {
	clusterOperator, ok := obj.(*osconfigv1.ClusterOperator)
	if ok {
		return clusterOperator.Name == "machine-api"
	}

	return false
}

func (optr *Operator) worker() {
	for optr.processNextWorkItem() {
	}
}

func (optr *Operator) processNextWorkItem() bool {
	key, quit := optr.queue.Get()
	if quit {
		return false
	}
	defer optr.queue.Done(key)

	klog.V(4).Infof("Processing key %s", key)
	result, err := optr.syncHandler(key.(string))
	optr.handleSyncResult(result, err, key)

	return true
}

func (optr *Operator) handleSyncResult(result reconcile.Result, err error, key interface{}) {
	switch {
	case err != nil && optr.queue.NumRequeues(key) < maxRetries:
		klog.V(1).Infof("Error syncing operator %v: %v", key, err)
		optr.queue.AddRateLimited(key)
	case err != nil:
		// We've gone over max retries, don't try again
		utilruntime.HandleError(err)
		klog.V(1).Infof("Dropping operator %q out of the queue: %v", key, err)
		optr.queue.Forget(key)
	case result.RequeueAfter > 0:
		// The result.RequeueAfter request will be lost, if it is returned
		// along with a non-nil error. But this is intended as
		// We need to drive to stable reconcile loops before queuing due
		// to result.RequestAfter
		optr.queue.Forget(key)
		optr.queue.AddAfter(key, result.RequeueAfter)
	case result.Requeue:
		optr.queue.AddRateLimited(key)
	default:
		// Finally, if no error occurs we Forget this item so it does not
		// get queued again until another change happens.
		optr.queue.Forget(key)
	}
}

func (optr *Operator) sync(key string) (reconcile.Result, error) {
	startTime := time.Now()
	klog.V(4).Infof("Started syncing operator %q (%v)", key, startTime)
	defer func() {
		klog.V(4).Infof("Finished syncing operator %q (%v)", key, time.Since(startTime))
	}()

	operatorConfig, err := optr.maoConfigFromInfrastructure()
	if err != nil {
		klog.Errorf("Failed getting operator config: %v", err)
		return reconcile.Result{}, err
	}
	return optr.syncAll(operatorConfig)
}

func (optr *Operator) maoConfigFromInfrastructure() (*OperatorConfig, error) {
	infra, err := optr.osClient.ConfigV1().Infrastructures().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	provider, err := getProviderFromInfrastructure(infra)
	if err != nil {
		return nil, err
	}

	images, err := getImagesFromJSONFile(optr.imagesFile)
	if err != nil {
		return nil, err
	}

	providerControllerImage, err := getProviderControllerFromImages(provider, *images)
	if err != nil {
		return nil, err
	}

	terminationHandlerImage, err := getTerminationHandlerFromImages(provider, *images)
	if err != nil {
		return nil, err
	}

	machineAPIOperatorImage, err := getMachineAPIOperatorFromImages(*images)
	if err != nil {
		return nil, err
	}

	kubeRBACProxy, err := getKubeRBACProxyFromImages(*images)
	if err != nil {
		return nil, err
	}

	clusterWideProxy, err := optr.osClient.ConfigV1().Proxies().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	features := map[string]bool{}
	// in case the MHC controller is disabled, leave its image empty
	mhcImage := machineAPIOperatorImage
	featureGates, err := optr.featureGateAccessor.CurrentFeatureGates()
	if err != nil {
		return nil, err
	}

	for _, featureGateName := range featureGates.KnownFeatures() {
		if featureGates.Enabled(featureGateName) {
			features[string(featureGateName)] = true
		} else {
			features[string(featureGateName)] = false
		}
	}

	if featureGates.Enabled(apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController) {
		klog.V(2).Info("Disabling MHC controller")
		mhcImage = ""
	}
	if featureGates.Enabled(apifeatures.FeatureGateMachineAPIMigration) {
		klog.V(2).Info("Enabling MachineAPIMigration for provider controller")
	}

	return &OperatorConfig{
		TargetNamespace: optr.namespace,
		Proxy:           clusterWideProxy,
		Controllers: Controllers{
			Provider:           providerControllerImage,
			MachineSet:         machineAPIOperatorImage,
			NodeLink:           machineAPIOperatorImage,
			MachineHealthCheck: mhcImage,
			KubeRBACProxy:      kubeRBACProxy,
			TerminationHandler: terminationHandlerImage,
		},
		PlatformType: provider,
		Features:     features,
	}, nil
}
