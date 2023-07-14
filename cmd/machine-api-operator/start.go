package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"

	osconfigv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/operator"
	"github.com/openshift/machine-api-operator/pkg/util"
	"github.com/openshift/machine-api-operator/pkg/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	coreclientsetv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

const (
	// defaultMetricsPort is the default port to expose metrics.
	defaultMetricsPort = 8080
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

	le := util.GetLeaderElectionConfig(cb.config, osconfigv1.LeaderElection{})

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          CreateResourceLock(cb, componentNamespace, componentName),
		RenewDeadline: le.RenewDeadline.Duration,
		RetryPeriod:   le.RetryPeriod.Duration,
		LeaseDuration: le.LeaseDuration.Duration,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				ctrlCtx := CreateControllerContext(cb, stopCh, componentNamespace)
				startControllersOrDie(ctrlCtx)
				ctrlCtx.KubeNamespacedInformerFactory.Start(ctrlCtx.Stop)
				ctrlCtx.ConfigInformerFactory.Start(ctrlCtx.Stop)
				initMachineAPIInformers(ctrlCtx)
				startMetricsCollectionAndServer(ctrlCtx)
				close(ctrlCtx.InformersStarted)

				select {}
			},
			OnStoppedLeading: func() {
				klog.Fatalf("Leader election lost")
			},
		},
		ReleaseOnCancel: true,
	})
	panic("unreachable")
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
	recorder := events.NewKubeRecorder(kubeClient.CoreV1().Events(componentNamespace), "machineapioperator", controllerRef)
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
	go startHTTPMetricServer(fmt.Sprintf("localhost:%d", metricsPort))
}

func startHTTPMetricServer(metricsPort string) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	server := &http.Server{
		Addr:    metricsPort,
		Handler: mux,
	}
	klog.Fatal(server.ListenAndServe())
}
