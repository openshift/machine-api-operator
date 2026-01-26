package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	coreclientsetv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	osconfigv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	utiltls "github.com/openshift/controller-runtime-common/pkg/tls"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/operator"
	"github.com/openshift/machine-api-operator/pkg/util"
	"github.com/openshift/machine-api-operator/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// defaultMetricsPort is the default port to expose metrics.
	defaultMetricsPort = 8443
	metricsCertFile    = "/etc/tls/private/tls.crt"
	metricsKeyFile     = "/etc/tls/private/tls.key"
)

var (
	// errImagesJsonEmpty is and Error when --images-json option is empty
	errImagesJsonEmpty = errors.New("--images-json should not be empty")
)

var (
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "Starts Machine API Operator",
		Long:  "",
		RunE:  runStartCmd,
	}

	startOpts struct {
		kubeconfig string
		imagesFile string
	}
)

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.PersistentFlags().StringVar(&startOpts.kubeconfig, "kubeconfig", "", "Kubeconfig file to access a remote cluster (testing only)")
	startCmd.PersistentFlags().StringVar(&startOpts.imagesFile, "images-json", "", "images.json file for MAO.")

	klog.InitFlags(nil)
	flag.Parse()
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
}

func runStartCmd(cmd *cobra.Command, args []string) error {
	if err := flag.Set("logtostderr", "true"); err != nil {
		return fmt.Errorf("failed to set logtostderr flag: %v", err)
	}

	// To help debugging, immediately log version
	klog.Infof("Version: %+v", version.Version)

	if startOpts.imagesFile == "" {
		return errImagesJsonEmpty
	}

	cb, err := NewClientBuilder(startOpts.kubeconfig)
	if err != nil {
		return fmt.Errorf("error creating clients: %v", err)
	}
	stopCh := make(chan struct{})
	leaderElectionCtx, leaderElectionCancel := context.WithCancel(context.Background())
	var shutdownOnce sync.Once
	var shuttingDown atomic.Bool
	shutdown := func() {
		shutdownOnce.Do(func() {
			shuttingDown.Store(true)
			close(stopCh)
			leaderElectionCancel()
		})
	}

	le := util.GetLeaderElectionConfig(cb.config, osconfigv1.LeaderElection{})

	leaderelection.RunOrDie(leaderElectionCtx, leaderelection.LeaderElectionConfig{
		Lock:          CreateResourceLock(cb, componentNamespace, componentName),
		RenewDeadline: le.RenewDeadline.Duration,
		RetryPeriod:   le.RetryPeriod.Duration,
		LeaseDuration: le.LeaseDuration.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				ctrlCtx := CreateControllerContext(cb, stopCh, componentNamespace)
				if err := setupTLSProfileWatcher(ctrlCtx, shutdown); err != nil {
					klog.Fatalf("Unable to set up TLS profile watcher: %v", err)
				}
				startControllersOrDie(ctrlCtx)
				ctrlCtx.KubeNamespacedInformerFactory.Start(ctrlCtx.Stop)
				ctrlCtx.ConfigInformerFactory.Start(ctrlCtx.Stop)
				initMachineAPIInformers(ctrlCtx)
				startMetricsCollectionAndServer(ctrlCtx)
				close(ctrlCtx.InformersStarted)

				<-stopCh
			},
			OnStoppedLeading: func() {
				if shuttingDown.Load() {
					klog.Info("Leader election stopped due to shutdown")
					return
				}
				klog.Fatalf("Leader election lost")
			},
		},
		ReleaseOnCancel: true,
	})
	return nil
}

func initMachineAPIInformers(ctx *ControllerContext) {
	mInformer := ctx.MachineInformerFactory.Machine().V1beta1().Machines().Informer()
	msInformer := ctx.MachineInformerFactory.Machine().V1beta1().MachineSets().Informer()
	ctx.MachineInformerFactory.Start(ctx.Stop)
	if !cache.WaitForCacheSync(ctx.Stop,
		mInformer.HasSynced,
		msInformer.HasSynced) {
		klog.Fatal("Failed to sync caches for Machine api informers")
	}
	klog.Info("Synced up machine api informer caches")
}

func initEventRecorder(kubeClient kubernetes.Interface) (record.EventRecorder, error) {
	eventRecorderScheme := runtime.NewScheme()
	if err := osconfigv1.Install(eventRecorderScheme); err != nil {
		return nil, fmt.Errorf("failed to create event recorder scheme: %v", err)
	}
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&coreclientsetv1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	return eventBroadcaster.NewRecorder(eventRecorderScheme, v1.EventSource{Component: "machineapioperator"}), nil
}

func initRecorder(kubeClient kubernetes.Interface) (events.Recorder, error) {
	controllerRef, err := events.GetControllerReferenceForCurrentPod(context.Background(), kubeClient, componentNamespace, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create controller ref for recorder: %v", err)
	}
	recorder := events.NewKubeRecorder(kubeClient.CoreV1().Events(componentNamespace), "machineapioperator", controllerRef, clock.RealClock{})
	return recorder, nil
}

func startControllersOrDie(ctx *ControllerContext) {
	kubeClient := ctx.ClientBuilder.KubeClientOrDie(componentName)
	eventRecorder, err := initEventRecorder(kubeClient)
	if err != nil {
		klog.Fatalf("failed to create event recorder: %v", err)
	}
	recorder, err := initRecorder(kubeClient)
	if err != nil {
		klog.Fatalf("failed to create recorder: %v", err)
	}
	optr, err := operator.New(
		componentNamespace, componentName,
		startOpts.imagesFile,
		config,
		ctx.KubeNamespacedInformerFactory.Apps().V1().Deployments(),
		ctx.KubeNamespacedInformerFactory.Apps().V1().DaemonSets(),
		ctx.ConfigInformerFactory.Config().V1().ClusterOperators(),
		ctx.ConfigInformerFactory.Config().V1().FeatureGates(),
		ctx.ConfigInformerFactory.Config().V1().ClusterVersions(),
		ctx.KubeNamespacedInformerFactory.Admissionregistration().V1().ValidatingWebhookConfigurations(),
		ctx.KubeNamespacedInformerFactory.Admissionregistration().V1().MutatingWebhookConfigurations(),
		ctx.ConfigInformerFactory.Config().V1().Proxies(),
		ctx.ClientBuilder.KubeClientOrDie(componentName),
		ctx.ClientBuilder.OpenshiftClientOrDie(componentName),
		ctx.ClientBuilder.MachineClientOrDie(componentName),
		ctx.ClientBuilder.DynamicClientOrDie(componentName),
		eventRecorder,
		recorder,
	)
	if err != nil {
		panic(fmt.Errorf("error creating operator: %v", err))
	}

	go optr.Run(1, ctx.Stop)
}

func startMetricsCollectionAndServer(ctx *ControllerContext) {
	machineInformer := ctx.MachineInformerFactory.Machine().V1beta1().Machines()
	machinesetInformer := ctx.MachineInformerFactory.Machine().V1beta1().MachineSets()
	machineMetricsCollector := metrics.NewMachineCollector(
		machineInformer,
		machinesetInformer,
		componentNamespace)
	prometheus.MustRegister(machineMetricsCollector)
	metricsPort := defaultMetricsPort
	if port, ok := os.LookupEnv("METRICS_PORT"); ok {
		v, err := strconv.Atoi(port)
		if err != nil {
			klog.Fatalf("Error parsing METRICS_PORT (%q) environment variable: %v", port, err)
		}
		metricsPort = v
	}
	klog.V(4).Info("Starting server to serve prometheus metrics")
	tlsConfig, err := metricsTLSConfig(ctx)
	if err != nil {
		klog.Fatalf("Unable to configure metrics TLS: %v", err)
	}
	go startHTTPSMetricServer(fmt.Sprintf(":%d", metricsPort), tlsConfig)
}

func metricsTLSConfig(ctx *ControllerContext) (*tls.Config, error) {
	scheme := runtime.NewScheme()
	if err := osconfigv1.Install(scheme); err != nil {
		return nil, fmt.Errorf("unable to add config.openshift.io scheme: %w", err)
	}

	k8sClient, err := client.New(ctx.ClientBuilder.config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("unable to create Kubernetes client: %w", err)
	}

	tlsSecurityProfileSpec, err := utiltls.FetchAPIServerTLSProfile(context.Background(), k8sClient)
	if err != nil {
		return nil, fmt.Errorf("unable to get TLS profile from API server: %w", err)
	}

	tlsConfigFn, unsupportedCiphers := utiltls.NewTLSConfigFromProfile(tlsSecurityProfileSpec)
	if len(unsupportedCiphers) > 0 {
		klog.Infof("TLS configuration contains unsupported ciphers that will be ignored: %v", unsupportedCiphers)
	}

	tlsConfig := &tls.Config{}
	tlsConfigFn(tlsConfig)

	return tlsConfig, nil
}

func setupTLSProfileWatcher(ctx *ControllerContext, shutdown func()) error {
	configClient := ctx.ClientBuilder.OpenshiftClientOrDie("tls-profile-watcher")
	initialProfile, err := fetchAPIServerTLSProfileSpec(context.Background(), configClient)
	if err != nil {
		return err
	}

	apiServerInformer := ctx.ConfigInformerFactory.Config().V1().APIServers().Informer()
	_, err = apiServerInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleTLSProfileEvent(obj, initialProfile, shutdown)
		},
		UpdateFunc: func(_, newObj interface{}) {
			handleTLSProfileEvent(newObj, initialProfile, shutdown)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add APIServer event handler: %w", err)
	}

	return nil
}

func fetchAPIServerTLSProfileSpec(ctx context.Context, configClient osclientset.Interface) (osconfigv1.TLSProfileSpec, error) {
	apiServer, err := configClient.ConfigV1().APIServers().Get(ctx, utiltls.APIServerName, metav1.GetOptions{})
	if err != nil {
		return osconfigv1.TLSProfileSpec{}, fmt.Errorf("failed to get APIServer %q: %w", utiltls.APIServerName, err)
	}

	profile, err := utiltls.GetTLSProfileSpec(apiServer.Spec.TLSSecurityProfile)
	if err != nil {
		return osconfigv1.TLSProfileSpec{}, fmt.Errorf("failed to get TLS profile from APIServer %q: %w", utiltls.APIServerName, err)
	}

	return profile, nil
}

func handleTLSProfileEvent(obj interface{}, initialProfile osconfigv1.TLSProfileSpec, shutdown func()) {
	apiServer, ok := obj.(*osconfigv1.APIServer)
	if !ok {
		return
	}
	if apiServer.Name != utiltls.APIServerName {
		return
	}

	currentProfile, err := utiltls.GetTLSProfileSpec(apiServer.Spec.TLSSecurityProfile)
	if err != nil {
		klog.Errorf("Failed to get TLS profile from APIServer %q: %v", apiServer.Name, err)
		return
	}

	if reflect.DeepEqual(initialProfile, currentProfile) {
		klog.V(2).Info("TLS security profile unchanged")
		return
	}

	klog.Infof("TLS security profile has changed, initiating a shutdown to pick up the new configuration: initialMinTLSVersion=%s currentMinTLSVersion=%s initialCiphers=%v currentCiphers=%v",
		initialProfile.MinTLSVersion,
		currentProfile.MinTLSVersion,
		initialProfile.Ciphers,
		currentProfile.Ciphers,
	)
	shutdown()
}

func startHTTPSMetricServer(metricsAddr string, tlsConfig *tls.Config) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:      metricsAddr,
		Handler:   mux,
		TLSConfig: tlsConfig,
	}
	klog.Fatal(server.ListenAndServeTLS(metricsCertFile, metricsKeyFile))
}
