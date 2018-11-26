package cvo

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/blang/semver"
	"github.com/golang/glog"
	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	apiextclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	coreclientsetv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	configv1 "github.com/openshift/api/config/v1"
	clientset "github.com/openshift/client-go/config/clientset/versioned"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configlistersv1 "github.com/openshift/client-go/config/listers/config/v1"
	"github.com/openshift/cluster-version-operator/lib/resourceapply"
	"github.com/openshift/cluster-version-operator/lib/resourcemerge"
	"github.com/openshift/cluster-version-operator/lib/validation"
)

const (
	// maxRetries is the number of times a machineconfig pool will be retried before it is dropped out of the queue.
	// With the current rate-limiter in use (5ms*2^(maxRetries-1)) the following numbers represent the times
	// a machineconfig pool is going to be requeued:
	//
	// 5ms, 10ms, 20ms, 40ms, 80ms, 160ms, 320ms, 640ms, 1.3s, 2.6s, 5.1s, 10.2s, 20.4s, 41s, 82s
	maxRetries = 15
)

// ownerKind contains the schema.GroupVersionKind for type that owns objects managed by CVO.
var ownerKind = configv1.SchemeGroupVersion.WithKind("ClusterVersion")

// Operator defines cluster version operator. The CVO attempts to reconcile the appropriate payload
// onto the cluster, writing status to the ClusterVersion object as it goes. A background loop
// periodically checks for new updates from a server described by spec.upstream and spec.channel.
// The main CVO sync loop is the single writer of ClusterVersion status.
//
// The CVO updates multiple conditions, but synthesizes them into a summary message on the
// Progressing condition to answer the question of "what version is available on the cluster".
// When errors occur, the Failing condition of the status is updated with a detailed message and
// reason, and then the reason is used to summarize the error onto the Progressing condition's
// message for a simple overview.
//
// The CVO periodically syncs the whole payload to the cluster even if no version transition is
// detected in order to undo accidental actions.
//
// A release image is expected to contain a CVO binary, the manifests necessary to update the
// CVO, and the manifests of the other operators on the cluster. During an update the operator
// attempts to copy the contents of the image manifests into a temporary directory using a
// batch job and a shared host-path, then applies the CVO manifests using the payload image
// for the CVO deployment. The deployment is then expected to launch the new process, and the
// new operator picks up the lease and applies the rest of the payload.
type Operator struct {
	// nodename allows CVO to sync fetchPayload to same node as itself.
	nodename string
	// namespace and name are used to find the ClusterVersion, OperatorStatus.
	namespace, name string

	// releaseImage allows templating CVO deployment manifest.
	releaseImage string
	// releaseVersion is a string identifier for the current version, read
	// from the payload of the operator. It may be empty if no version exists, in
	// which case no available updates will be returned.
	releaseVersion string

	// restConfig is used to create resourcebuilder.
	restConfig *rest.Config

	client        clientset.Interface
	kubeClient    kubernetes.Interface
	apiExtClient  apiextclientset.Interface
	eventRecorder record.EventRecorder

	// updatePayloadHandler allows unit tests to inject arbitrary payload errors
	updatePayloadHandler func(config *configv1.ClusterVersion, payload *updatePayload) error

	// minimumUpdateCheckInterval is the minimum duration to check for updates from
	// the upstream.
	minimumUpdateCheckInterval time.Duration
	// payloadDir is intended for testing. If unset it will default to '/'
	payloadDir string
	// defaultUpstreamServer is intended for testing.
	defaultUpstreamServer string
	// syncBackoff allows the tests to use a quicker backoff
	syncBackoff wait.Backoff

	cvLister              configlistersv1.ClusterVersionLister
	cvListerSynced        cache.InformerSynced
	clusterOperatorLister configlistersv1.ClusterOperatorLister
	clusterOperatorSynced cache.InformerSynced

	// queue tracks applying updates to a cluster.
	queue workqueue.RateLimitingInterface
	// availableUpdatesQueue tracks checking for updates from the update server.
	availableUpdatesQueue workqueue.RateLimitingInterface

	// statusLock guards access to modifying available updates
	statusLock       sync.Mutex
	availableUpdates *availableUpdates

	// lastAtLock guards access to controller memory about the sync loop
	lastAtLock          sync.Mutex
	lastSyncAt          time.Time
	lastResourceVersion int64
}

// New returns a new cluster version operator.
func New(
	nodename string,
	namespace, name string,
	releaseImage string,
	overridePayloadDir string,
	minimumInterval time.Duration,
	cvInformer configinformersv1.ClusterVersionInformer,
	clusterOperatorInformer configinformersv1.ClusterOperatorInformer,
	restConfig *rest.Config,
	client clientset.Interface,
	kubeClient kubernetes.Interface,
	apiExtClient apiextclientset.Interface,
) *Operator {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(glog.Infof)
	eventBroadcaster.StartRecordingToSink(&coreclientsetv1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})

	optr := &Operator{
		nodename:     nodename,
		namespace:    namespace,
		name:         name,
		releaseImage: releaseImage,

		minimumUpdateCheckInterval: minimumInterval,
		payloadDir:                 overridePayloadDir,
		defaultUpstreamServer:      "http://localhost:8080/graph",
		syncBackoff: wait.Backoff{
			Duration: time.Second * 10,
			Factor:   1.3,
			Steps:    3,
		},

		restConfig:    restConfig,
		client:        client,
		kubeClient:    kubeClient,
		apiExtClient:  apiExtClient,
		eventRecorder: eventBroadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "clusterversionoperator"}),

		queue:                 workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "clusterversion"),
		availableUpdatesQueue: workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "availableupdates"),
	}

	optr.updatePayloadHandler = optr.syncUpdatePayload

	cvInformer.Informer().AddEventHandler(optr.eventHandler())
	clusterOperatorInformer.Informer().AddEventHandler(optr.eventHandler())

	optr.clusterOperatorLister = clusterOperatorInformer.Lister()
	optr.clusterOperatorSynced = clusterOperatorInformer.Informer().HasSynced

	optr.cvLister = cvInformer.Lister()
	optr.cvListerSynced = cvInformer.Informer().HasSynced

	if err := optr.registerMetrics(); err != nil {
		panic(err)
	}

	if meta, _, err := loadUpdatePayloadMetadata(optr.baseDirectory(), releaseImage); err != nil {
		glog.Warningf("The local payload is invalid - no current version can be determined from disk: %v", err)
	} else {
		// XXX: set this to the cincinnati version in preference
		if _, err := semver.Parse(meta.imageRef.Name); err != nil {
			glog.Warningf("The local payload name %q is not a valid semantic version - no current version will be reported: %v", meta.imageRef.Name, err)
		} else {
			optr.releaseVersion = meta.imageRef.Name
		}
	}

	return optr
}

// Run runs the cluster version operator.
func (optr *Operator) Run(workers int, stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer optr.queue.ShutDown()

	glog.Infof("Starting ClusterVersionOperator with minimum reconcile period %s", optr.minimumUpdateCheckInterval)
	defer glog.Info("Shutting down ClusterVersionOperator")

	if !cache.WaitForCacheSync(stopCh,
		optr.clusterOperatorSynced,
		optr.cvListerSynced,
	) {
		return
	}

	// trigger the first cluster version reconcile always
	optr.queue.Add(optr.queueKey())

	go wait.Until(func() { optr.worker(optr.queue, optr.sync) }, time.Second, stopCh)
	go wait.Until(func() { optr.worker(optr.availableUpdatesQueue, optr.availableUpdatesSync) }, time.Second, stopCh)

	<-stopCh
}

func (optr *Operator) queueKey() string {
	return fmt.Sprintf("%s/%s", optr.namespace, optr.name)
}

func (optr *Operator) eventHandler() cache.ResourceEventHandler {
	workQueueKey := optr.queueKey()
	return cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			optr.queue.Add(workQueueKey)
			optr.availableUpdatesQueue.Add(workQueueKey)
		},
		UpdateFunc: func(old, new interface{}) {
			optr.queue.Add(workQueueKey)
			optr.availableUpdatesQueue.Add(workQueueKey)
		},
		DeleteFunc: func(obj interface{}) {
			optr.queue.Add(workQueueKey)
		},
	}
}

func (optr *Operator) worker(queue workqueue.RateLimitingInterface, syncHandler func(string) error) {
	for processNextWorkItem(queue, syncHandler, optr.syncFailingStatus) {
	}
}

type syncFailingStatusFunc func(config *configv1.ClusterVersion, err error) error

func processNextWorkItem(queue workqueue.RateLimitingInterface, syncHandler func(string) error, syncFailingStatus syncFailingStatusFunc) bool {
	key, quit := queue.Get()
	if quit {
		return false
	}
	defer queue.Done(key)

	err := syncHandler(key.(string))
	handleErr(queue, err, key, syncFailingStatus)
	return true
}

func handleErr(queue workqueue.RateLimitingInterface, err error, key interface{}, syncFailingStatus syncFailingStatusFunc) {
	if err == nil {
		queue.Forget(key)
		return
	}

	if queue.NumRequeues(key) < maxRetries {
		glog.V(2).Infof("Error syncing operator %v: %v", key, err)
		queue.AddRateLimited(key)
		return
	}

	err = syncFailingStatus(nil, err)
	utilruntime.HandleError(err)
	glog.V(2).Infof("Dropping operator %q out of the queue %v: %v", key, queue, err)
	queue.Forget(key)
}

func (optr *Operator) sync(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing cluster version %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing cluster version %q (%v)", key, time.Since(startTime))
	}()

	// ensure the cluster version exists, that the object is valid, and that
	// all initial conditions are set.
	original, changed, err := optr.getOrCreateClusterVersion()
	if err != nil {
		return err
	}
	if changed {
		glog.V(4).Infof("Cluster version changed, waiting for newer event")
		return nil
	}

	glog.V(3).Infof("ClusterVersion: %#v", original)

	// when we're up to date, limit how frequently we check the payload
	availableAndUpdated := original.Status.Generation == original.Generation &&
		resourcemerge.IsOperatorStatusConditionTrue(original.Status.Conditions, configv1.OperatorAvailable)
	hasRecentlySynced := availableAndUpdated && optr.hasRecentlySynced()
	if hasRecentlySynced {
		glog.V(4).Infof("Cluster version has been recently synced and no new changes detected")
		return nil
	}

	optr.setLastSyncAt(time.Time{})

	// read the payload
	payload, err := optr.loadUpdatePayload(original)
	if err != nil {
		// the payload is invalid, try and update the status to indicate that
		if sErr := optr.syncPayloadFailingStatus(original, err); sErr != nil {
			glog.V(2).Infof("Unable to write status when payload was invalid: %v", sErr)
		}
		return err
	}

	update := configv1.Update{
		Version: payload.releaseVersion,
		Payload: payload.releaseImage,
	}

	// if the current payload is already live, we are reconciling, not updating,
	// and we won't set the progressing status.
	if availableAndUpdated && payload.manifestHash == original.Status.VersionHash {
		glog.V(2).Infof("Reconciling cluster to version %s and image %s (hash=%s)", update.Version, update.Payload, payload.manifestHash)
	} else {
		glog.V(2).Infof("Updating the cluster to version %s and image %s (hash=%s)", update.Version, update.Payload, payload.manifestHash)
		if err := optr.syncProgressingStatus(original); err != nil {
			return err
		}
	}

	if err := optr.updatePayloadHandler(original, payload); err != nil {
		if applyErr := optr.syncUpdateFailingStatus(original, err); applyErr != nil {
			glog.V(2).Infof("Unable to write status when sync error occurred: %v", applyErr)
		}
		return err
	}

	glog.V(2).Infof("Payload for cluster version %s synced", update.Version)

	// update the status to indicate we have synced
	optr.setLastSyncAt(time.Now())
	return optr.syncAvailableStatus(original, update, payload.manifestHash)
}

// availableUpdatesSync is triggered on cluster version change (and periodic requeues) to
// sync available updates. It only modifies cluster version.
func (optr *Operator) availableUpdatesSync(key string) error {
	startTime := time.Now()
	glog.V(4).Infof("Started syncing available updates %q (%v)", key, startTime)
	defer func() {
		glog.V(4).Infof("Finished syncing available updates %q (%v)", key, time.Since(startTime))
	}()

	config, err := optr.cvLister.Get(optr.name)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if errs := validation.ValidateClusterVersion(config); len(errs) > 0 {
		return nil
	}

	return optr.syncAvailableUpdates(config)
}

// hasRecentlySynced returns true if the most recent sync was newer than the
// minimum check interval.
func (optr *Operator) hasRecentlySynced() bool {
	if optr.minimumUpdateCheckInterval == 0 {
		return false
	}
	optr.lastAtLock.Lock()
	defer optr.lastAtLock.Unlock()
	return optr.lastSyncAt.After(time.Now().Add(-optr.minimumUpdateCheckInterval))
}

// setLastSyncAt sets the time the operator was last synced at.
func (optr *Operator) setLastSyncAt(t time.Time) {
	optr.lastAtLock.Lock()
	defer optr.lastAtLock.Unlock()
	optr.lastSyncAt = t
}

// isOlderThanLastUpdate returns true if the cluster version is older than
// the last update we saw.
func (optr *Operator) isOlderThanLastUpdate(config *configv1.ClusterVersion) bool {
	i, err := strconv.ParseInt(config.ResourceVersion, 10, 64)
	if err != nil {
		return false
	}
	optr.lastAtLock.Lock()
	defer optr.lastAtLock.Unlock()
	return i < optr.lastResourceVersion
}

// rememberLastUpdate records the most recent resource version we
// have seen from the server for cluster versions.
func (optr *Operator) rememberLastUpdate(config *configv1.ClusterVersion) {
	if config == nil {
		return
	}
	i, err := strconv.ParseInt(config.ResourceVersion, 10, 64)
	if err != nil {
		return
	}
	optr.lastAtLock.Lock()
	defer optr.lastAtLock.Unlock()
	optr.lastResourceVersion = i
}

func (optr *Operator) getOrCreateClusterVersion() (*configv1.ClusterVersion, bool, error) {
	obj, err := optr.cvLister.Get(optr.name)
	if err == nil {
		// if we are waiting to see a newer cached version, just exit
		if optr.isOlderThanLastUpdate(obj) {
			return nil, true, nil
		}

		// ensure that the object we do have is valid
		errs := validation.ValidateClusterVersion(obj)
		changed, err := optr.syncInitialObjectStatus(obj, errs)
		if err != nil {
			return nil, false, err
		}

		// for fields that have meaning that are incomplete, clear them
		// prevents us from loading clearly malformed payloads
		obj = validation.ClearInvalidFields(obj, errs)

		return obj, changed, nil
	}

	if !apierrors.IsNotFound(err) {
		return nil, false, err
	}

	var upstream configv1.URL
	if len(optr.defaultUpstreamServer) > 0 {
		u := configv1.URL(optr.defaultUpstreamServer)
		upstream = u
	}
	id, _ := uuid.NewRandom()

	// XXX: generate ClusterVersion from options calculated above.
	config := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: optr.name,
		},
		Spec: configv1.ClusterVersionSpec{
			Upstream:  upstream,
			Channel:   "fast",
			ClusterID: configv1.ClusterID(id.String()),
		},
	}

	actual, _, err := resourceapply.ApplyClusterVersionFromCache(optr.cvLister, optr.client.ConfigV1(), config)
	if apierrors.IsAlreadyExists(err) {
		return nil, true, nil
	}
	return actual, true, err
}

// versionString returns a string describing the current version.
func (optr *Operator) currentVersionString(config *configv1.ClusterVersion) string {
	if s := config.Status.Current.Version; len(s) > 0 {
		return s
	}
	if s := config.Status.Current.Payload; len(s) > 0 {
		return s
	}
	if s := optr.releaseVersion; len(s) > 0 {
		return s
	}
	if s := optr.releaseImage; len(s) > 0 {
		return s
	}
	return "<unknown>"
}

// versionString returns a string describing the desired version.
func (optr *Operator) desiredVersionString(config *configv1.ClusterVersion) string {
	var s string
	if v := config.Spec.DesiredUpdate; v != nil {
		if len(v.Payload) > 0 {
			s = v.Payload
		}
		if len(v.Version) > 0 {
			s = v.Version
		}
	}
	if len(s) == 0 {
		s = optr.currentVersionString(config)
	}
	return s
}

// currentVersion returns an update object describing the current known cluster version.
func (optr *Operator) currentVersion() configv1.Update {
	return configv1.Update{
		Version: optr.releaseVersion,
		Payload: optr.releaseImage,
	}
}
