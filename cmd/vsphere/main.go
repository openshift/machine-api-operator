package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"
	ipamv1beta1 "sigs.k8s.io/cluster-api/exp/ipam/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1 "github.com/openshift/api/config/v1"
	apifeatures "github.com/openshift/api/features"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/library-go/pkg/config/leaderelection"
	"github.com/openshift/library-go/pkg/features"
	capimachine "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere"
	machine "github.com/openshift/machine-api-operator/pkg/controller/vsphere"
	machinesetcontroller "github.com/openshift/machine-api-operator/pkg/controller/vsphere/machineset"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/util"
	"github.com/openshift/machine-api-operator/pkg/version"
)

const timeout = 10 * time.Minute

func main() {
	var printVersion bool
	flag.BoolVar(&printVersion, "version", false, "print version and exit")

	// Used to get the default values for leader election from library-go
	defaultLeaderElectionValues := leaderelection.LeaderElectionDefaulting(
		configv1.LeaderElection{},
		"", "",
	)

	// Set log for controller-runtime
	ctrl.SetLogger(klog.NewKlogr())

	klog.InitFlags(nil)
	if err := flag.Set("logtostderr", "true"); err != nil {
		klog.Fatalf("failed to set logtostderr flag: %v", err)
	}

	watchNamespace := flag.String(
		"namespace",
		"",
		"Namespace that the controller watches to reconcile machine-api objects. If unspecified, the controller watches for machine-api objects across all namespaces.",
	)

	leaderElectResourceNamespace := flag.String(
		"leader-elect-resource-namespace",
		"",
		"The namespace of resource object that is used for locking during leader election. If unspecified and running in cluster, defaults to the service account namespace for the controller. Required for leader-election outside of a cluster.",
	)

	leaderElect := flag.Bool(
		"leader-elect",
		false,
		"Start a leader election client and gain leadership before executing the main loop. Enable this when running replicated components for high availability.",
	)

	// Default values are printed for the user to see, but zero is set as the default to distinguish user intent from default value for topology aware leader election
	leaderElectLeaseDuration := flag.Duration(
		"leader-elect-lease-duration",
		0,
		fmt.Sprintf("The duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot. This is effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. This is only applicable if leader election is enabled. Default: (%s)", defaultLeaderElectionValues.LeaseDuration.Duration),
	)

	metricsAddress := flag.String(
		"metrics-bind-address",
		metrics.DefaultMachineMetricsAddress,
		"Address for hosting metrics",
	)

	healthAddr := flag.String(
		"health-addr",
		":9440",
		"The address for health checking.",
	)

	// Sets up feature gates
	defaultMutableGate := feature.DefaultMutableFeatureGate
	gateOpts, err := features.NewFeatureGateOptions(defaultMutableGate, apifeatures.SelfManaged, apifeatures.FeatureGateVSphereStaticIPs, apifeatures.FeatureGateMachineAPIMigration, apifeatures.FeatureGateVSphereHostVMGroupZonal, apifeatures.FeatureGateVSphereMultiDisk)
	if err != nil {
		klog.Fatalf("Error setting up feature gates: %v", err)
	}

	// Add the --feature-gates flag
	gateOpts.AddFlagsToGoFlagSet(nil)

	flag.Parse()

	if printVersion {
		fmt.Println(version.String)
		os.Exit(0)
	}

	cfg := config.GetConfigOrDie()
	syncPeriod := timeout

	le := util.GetLeaderElectionConfig(cfg, configv1.LeaderElection{
		Disable:       !*leaderElect,
		LeaseDuration: metav1.Duration{Duration: *leaderElectLeaseDuration},
	})

	opts := manager.Options{
		Metrics: server.Options{
			BindAddress: *metricsAddress,
		},
		HealthProbeBindAddress: *healthAddr,
		Cache: cache.Options{
			SyncPeriod: &syncPeriod,
		},
		LeaderElection:          *leaderElect,
		LeaderElectionNamespace: *leaderElectResourceNamespace,
		LeaderElectionID:        "cluster-api-provider-vsphere-leader",
		LeaseDuration:           &le.LeaseDuration.Duration,
		RetryPeriod:             &le.RetryPeriod.Duration,
		RenewDeadline:           &le.RenewDeadline.Duration,
	}

	if *watchNamespace != "" {
		opts.Cache.DefaultNamespaces = map[string]cache.Config{
			*watchNamespace: {},
		}
		klog.Infof("Watching machine-api objects only in namespace %q for reconciliation.", *watchNamespace)
	}

	// Sets feature gates from flags
	klog.Infof("Initializing feature gates: %s", strings.Join(defaultMutableGate.KnownFeatures(), ", "))
	warnings, err := gateOpts.ApplyTo(defaultMutableGate)
	if err != nil {
		klog.Fatalf("Error setting feature gates from flags: %v", err)
	}
	if len(warnings) > 0 {
		klog.Infof("Warnings setting feature gates from flags: %v", warnings)
	}

	klog.Infof("FeatureGateMachineAPIMigration initialised: %t", defaultMutableGate.Enabled(featuregate.Feature(apifeatures.FeatureGateMachineAPIMigration)))

	staticIPFeatureGateEnabled := defaultMutableGate.Enabled(featuregate.Feature(apifeatures.FeatureGateVSphereStaticIPs))
	klog.Infof("FeatureGateVSphereStaticIPs initialised: %t", staticIPFeatureGateEnabled)
	hostVMGroupZonalFeatureGateEnabled := defaultMutableGate.Enabled(featuregate.Feature(apifeatures.FeatureGateVSphereHostVMGroupZonal))
	klog.Infof("FeatureGateVSphereHostVMGroupZonal initialised %t", hostVMGroupZonalFeatureGateEnabled)
	multiDiskFeatureGateEnabled := defaultMutableGate.Enabled(featuregate.Feature(apifeatures.FeatureGateVSphereMultiDisk))
	klog.Infof("FeatureGateVSphereMultiDisk initialised: %t", multiDiskFeatureGateEnabled)

	// Setup a Manager
	mgr, err := manager.New(cfg, opts)
	if err != nil {
		klog.Fatalf("Failed to set up overall controller manager: %v", err)
	}

	// Create a taskIDCache for create task IDs in case they are lost due to
	// network error or stale cache.
	taskIDCache := make(map[string]string)

	// Initialize machine actuator.
	machineActuator := machine.NewActuator(machine.ActuatorParams{
		Client:                   mgr.GetClient(),
		APIReader:                mgr.GetAPIReader(),
		EventRecorder:            mgr.GetEventRecorderFor("vspherecontroller"),
		TaskIDCache:              taskIDCache,
		FeatureGates:             defaultMutableGate,
		OpenshiftConfigNamespace: vsphere.OpenshiftConfigNamespace,
	})

	if err := configv1.Install(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := machinev1.Install(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := machinev1.Install(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := ipamv1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatalf("unable to add ipamv1beta1 to scheme: %v", err)
	}

	if err := capimachine.AddWithActuator(mgr, machineActuator, defaultMutableGate); err != nil {
		klog.Fatal(err)
	}

	setupLog := ctrl.Log.WithName("setup")
	if err = (&machinesetcontroller.Reconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("MachineSet"),
	}).SetupWithManager(mgr, controller.Options{}); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "MachineSet")
		os.Exit(1)
	}

	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		klog.Fatal(err)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		klog.Fatal(err)
	}

	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Fatalf("Failed to run manager: %v", err)
	}
}
