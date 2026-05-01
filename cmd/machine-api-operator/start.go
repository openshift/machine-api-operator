package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	coreclientsetv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	osconfigv1 "github.com/openshift/api/config/v1"
	utiltls "github.com/openshift/controller-runtime-common/pkg/tls"
	"github.com/openshift/library-go/pkg/operator/events"
	maometrics "github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/operator"
	pkgtls "github.com/openshift/machine-api-operator/pkg/tls"
	"github.com/openshift/machine-api-operator/pkg/util"
	"github.com/openshift/machine-api-operator/pkg/version"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

const (
	// defaultMetricsPort is the default port to expose metrics.
	defaultMetricsPort = 8443
	metricsCertDir     = "/etc/tls/private"
	metricsCertFile    = "tls.crt"
	metricsKeyFile     = "tls.key"
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
		kubeconfig      string
		imagesFile      string
		tlsMinVersion   string
		tlsCipherSuites []string
	}
)

func init() {
	rootCmd.AddCommand(startCmd)
	startCmd.PersistentFlags().StringVar(&startOpts.kubeconfig, "kubeconfig", "", "Kubeconfig file to access a remote cluster (testing only)")
	startCmd.PersistentFlags().StringVar(&startOpts.imagesFile, "images-json", "", "images.json file for MAO.")
	startCmd.PersistentFlags().StringVar(&startOpts.tlsMinVersion, "tls-min-version", "", "Minimum TLS version supported. When set with --tls-cipher-suites, overrides the cluster-wide TLS profile. Possible values: "+strings.Join(cliflag.TLSPossibleVersions(), ", "))
	startCmd.PersistentFlags().StringSliceVar(&startOpts.tlsCipherSuites, "tls-cipher-suites", nil, "Comma-separated list of cipher suites for the server. When set with --tls-min-version, overrides the cluster-wide TLS profile. Possible values: "+strings.Join(cliflag.TLSCipherPossibleValues(), ", "))

	klog.InitFlags(nil)
	flag.Parse()
	pflag.CommandLine.AddGoFlagSet(flag.CommandLine)
}

func runStartCmd(cmd *cobra.Command, args []string) error {
	if err := flag.Set("logtostderr", "true"); err != nil {
		return fmt.Errorf("failed to set logtostderr flag: %v", err)
	}
	// Opt into the fixed klog behavior so the --stderrthreshold flag is honored
	// even when --logtostderr is enabled. See https://github.com/kubernetes/klog/issues/432
	if err := flag.Set("legacy_stderr_threshold_behavior", "false"); err != nil {
		return fmt.Errorf("failed to set legacy_stderr_threshold_behavior flag: %v", err)
	}
	if err := flag.Set("stderrthreshold", "INFO"); err != nil {
		return fmt.Errorf("failed to set stderrthreshold flag: %v", err)
	}

	// To help debugging, immediately log version
	klog.Infof("Version: %+v", version.Version)

	if startOpts.imagesFile == "" {
		return errImagesJsonEmpty
	}
	if startOpts.tlsMinVersion != "" {
		if _, err := cliflag.TLSVersion(startOpts.tlsMinVersion); err != nil {
			return fmt.Errorf("invalid --tls-min-version value: %w", err)
		}
	}
	if len(startOpts.tlsCipherSuites) > 0 {
		if _, err := cliflag.TLSCipherSuites(startOpts.tlsCipherSuites); err != nil {
			return fmt.Errorf("invalid --tls-cipher-suites value: %w", err)
		}
	}

	cb, err := NewClientBuilder(startOpts.kubeconfig)
	if err != nil {
		return fmt.Errorf("error creating clients: %v", err)
	}
	stopCh := make(chan struct{})
	leaderElectionCtx, leaderElectionCancel := context.WithCancel(context.Background())
	var shutdownOnce sync.Once
	var shuttingDown atomic.Bool
	errCh := make(chan error, 1)
	reportError := func(err error) {
		if err == nil {
			return
		}
		select {
		case errCh <- err:
		default:
		}
	}
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
				tlsResult, err := pkgtls.ResolveTLSConfig(context.Background(), ctrlCtx.ClientBuilder.config, startOpts.tlsMinVersion, startOpts.tlsCipherSuites)
				if err != nil {
					reportError(fmt.Errorf("unable to resolve TLS configuration: %w", err))
					shutdown()
					return
				}
				if startOpts.tlsMinVersion == "" && len(startOpts.tlsCipherSuites) == 0 {
					if err := setupTLSProfileWatcher(ctrlCtx, tlsResult, shutdown); err != nil {
						reportError(fmt.Errorf("unable to set up TLS profile watcher: %w", err))
						shutdown()
						return
					}
				} else {
					klog.Info("TLS security profile watcher disabled because TLS is configured via CLI flags")
				}
				if err := startControllers(ctrlCtx); err != nil {
					reportError(err)
					shutdown()
					return
				}
				ctrlCtx.KubeNamespacedInformerFactory.Start(ctrlCtx.Stop)
				ctrlCtx.ConfigInformerFactory.Start(ctrlCtx.Stop)
				initMachineAPIInformers(ctrlCtx)
				if err := startMetricsCollectionAndServer(ctrlCtx, tlsResult, shutdown, reportError); err != nil {
					reportError(err)
					shutdown()
					return
				}
				close(ctrlCtx.InformersStarted)

				<-stopCh
			},
			OnStoppedLeading: func() {
				if shuttingDown.Load() {
					klog.Info("Leader election stopped due to shutdown")
					return
				}
				err := errors.New("leader election lost")
				klog.ErrorS(err, "Leader election lost")
				reportError(err)
				shutdown()
			},
		},
		ReleaseOnCancel: true,
	})

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
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

func startControllers(ctx *ControllerContext) error {
	kubeClient := ctx.ClientBuilder.KubeClientOrDie(componentName)
	eventRecorder, err := initEventRecorder(kubeClient)
	if err != nil {
		return fmt.Errorf("failed to create event recorder: %w", err)
	}
	recorder, err := initRecorder(kubeClient)
	if err != nil {
		return fmt.Errorf("failed to create recorder: %w", err)
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
		return fmt.Errorf("error creating operator: %w", err)
	}

	go optr.Run(1, ctx.Stop)
	return nil
}

func startMetricsCollectionAndServer(
	ctx *ControllerContext,
	tlsResult pkgtls.TLSConfigResult,
	shutdown func(),
	reportError func(error),
) error {
	machineInformer := ctx.MachineInformerFactory.Machine().V1beta1().Machines()
	machinesetInformer := ctx.MachineInformerFactory.Machine().V1beta1().MachineSets()
	machineMetricsCollector := maometrics.NewMachineCollector(
		machineInformer,
		machinesetInformer,
		componentNamespace)
	ctrlmetrics.Registry.MustRegister(machineMetricsCollector)
	metricsPort := defaultMetricsPort
	if port, ok := os.LookupEnv("METRICS_PORT"); ok {
		v, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("error parsing METRICS_PORT (%q) environment variable: %w", port, err)
		}
		metricsPort = v
	}
	klog.V(4).Info("Starting secure metrics server")
	metricsServer, err := newSecureMetricsServer(
		ctx,
		fmt.Sprintf(":%d", metricsPort),
		[]func(*tls.Config){tlsResult.TLSConfig},
	)
	if err != nil {
		return fmt.Errorf("unable to initialize secure metrics server: %w", err)
	}

	metricsServerCtx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Stop
		cancel()
	}()

	go func() {
		if err := metricsServer.Start(metricsServerCtx); err != nil {
			if errors.Is(err, context.Canceled) {
				klog.V(2).Info("Secure metrics server shutdown complete")
				return
			}
			reportError(fmt.Errorf("unable to start secure metrics server: %w", err))
			shutdown()
		}
	}()

	return nil
}

func newSecureMetricsServer(ctx *ControllerContext, metricsAddr string, tlsOpts []func(*tls.Config)) (metricsserver.Server, error) {
	httpClient, err := rest.HTTPClientFor(ctx.ClientBuilder.config)
	if err != nil {
		return nil, fmt.Errorf("unable to create HTTP client for metrics authn/authz: %w", err)
	}

	return metricsserver.NewServer(metricsserver.Options{
		BindAddress:    metricsAddr,
		SecureServing:  true,
		FilterProvider: filters.WithAuthenticationAndAuthorization,
		CertDir:        metricsCertDir,
		CertName:       metricsCertFile,
		KeyName:        metricsKeyFile,
		TLSOpts:        tlsOpts,
	}, ctx.ClientBuilder.config, httpClient)
}

func setupTLSProfileWatcher(ctx *ControllerContext, tlsResult pkgtls.TLSConfigResult, shutdown func()) error {
	initialProfile := tlsResult.TLSProfileSpec
	initialAdherencePolicy := tlsResult.TLSAdherencePolicy

	apiServerInformer := ctx.ConfigInformerFactory.Config().V1().APIServers().Informer()
	_, err := apiServerInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			handleTLSProfileEvent(obj, &initialProfile, &initialAdherencePolicy, shutdown)
		},
		UpdateFunc: func(_, newObj interface{}) {
			handleTLSProfileEvent(newObj, &initialProfile, &initialAdherencePolicy, shutdown)
		},
		DeleteFunc: func(obj interface{}) {
			handleTLSProfileEvent(obj, &initialProfile, &initialAdherencePolicy, shutdown)
		},
	})
	if err != nil {
		return fmt.Errorf("failed to add APIServer event handler: %w", err)
	}

	return nil
}

func handleTLSProfileEvent(
	obj interface{},
	initialProfile *osconfigv1.TLSProfileSpec,
	initialAdherencePolicy *osconfigv1.TLSAdherencePolicy,
	shutdown func(),
) {
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

	profileChanged := !reflect.DeepEqual(*initialProfile, currentProfile)
	adherencePolicyChanged := *initialAdherencePolicy != apiServer.Spec.TLSAdherence
	if !profileChanged && !adherencePolicyChanged {
		klog.V(2).Info("TLS settings unchanged")
		return
	}

	if profileChanged {
		klog.Infof("TLS security profile has changed, initiating a shutdown to pick up the new configuration: initialMinTLSVersion=%s currentMinTLSVersion=%s initialCiphers=%v currentCiphers=%v",
			initialProfile.MinTLSVersion,
			currentProfile.MinTLSVersion,
			initialProfile.Ciphers,
			currentProfile.Ciphers,
		)
		// Persist the new profile for future change detection.
		*initialProfile = currentProfile
	}
	if adherencePolicyChanged {
		klog.Infof("TLS adherence policy has changed, initiating a shutdown to pick up the new configuration: initialTLSAdherencePolicy=%s currentTLSAdherencePolicy=%s",
			*initialAdherencePolicy,
			apiServer.Spec.TLSAdherence,
		)
		// Persist the new policy for future change detection.
		*initialAdherencePolicy = apiServer.Spec.TLSAdherence
	}

	shutdown()
}
