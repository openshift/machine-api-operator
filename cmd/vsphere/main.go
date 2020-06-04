package main

import (
	"flag"
	"fmt"
	"os"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	vsphereapis "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider"
	capimachine "github.com/openshift/machine-api-operator/pkg/controller/machine"
	machine "github.com/openshift/machine-api-operator/pkg/controller/vsphere"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/version"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

func main() {
	var printVersion bool
	flag.BoolVar(&printVersion, "version", false, "print version and exit")

	klog.InitFlags(nil)
	watchNamespace := flag.String("namespace", "", "Namespace that the controller watches to reconcile machine-api objects. If unspecified, the controller watches for machine-api objects across all namespaces.")
	metricsAddress := flag.String("metrics-bind-address", metrics.DefaultMachineMetricsAddress, "Address for hosting metrics")
	flag.Set("logtostderr", "true")
	flag.Parse()

	if printVersion {
		fmt.Println(version.String)
		os.Exit(0)
	}

	cfg := config.GetConfigOrDie()

	opts := manager.Options{
		MetricsBindAddress: *metricsAddress,
	}
	if *watchNamespace != "" {
		opts.Namespace = *watchNamespace
		klog.Infof("Watching machine-api objects only in namespace %q for reconciliation.", opts.Namespace)
	}

	// Setup a Manager
	mgr, err := manager.New(cfg, opts)
	if err != nil {
		klog.Fatalf("Failed to set up overall controller manager: %v", err)
	}

	// Initialize machine actuator.
	machineActuator := machine.NewActuator(machine.ActuatorParams{
		Client:        mgr.GetClient(),
		APIReader:     mgr.GetAPIReader(),
		EventRecorder: mgr.GetEventRecorderFor("vspherecontroller"),
	})

	if err := configv1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := vsphereapis.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	if err := v1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	capimachine.AddWithActuator(mgr, machineActuator)

	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		klog.Fatalf("Failed to run manager: %v", err)
	}
}
