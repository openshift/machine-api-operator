package main

import (
	"flag"
	"runtime"

	"github.com/openshift/machine-api-operator/pkg/controller"

	"github.com/openshift/machine-api-operator/pkg/controller/nodelink"

	mapiv1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	sdkVersion "github.com/operator-framework/operator-sdk/version"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/runtime/signals"
)

func printVersion() {
	klog.Infof("Go Version: %s", runtime.Version())
	klog.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)
	klog.Infof("operator-sdk Version: %v", sdkVersion.Version)
}

func main() {
	printVersion()

	watchNamespace := flag.String("namespace", "", "Namespace that the controller watches to reconcile machine-api objects. If unspecified, the controller watches for machine-api objects across all namespaces.")
	klog.InitFlags(nil)
	flag.Set("logtostderr", "true")
	flag.Parse()

	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		klog.Fatal(err)
	}

	opts := manager.Options{
		// Disable metrics serving
		MetricsBindAddress: "0",
	}
	if *watchNamespace != "" {
		opts.Namespace = *watchNamespace
		klog.Infof("Watching machine-api objects only in namespace %q for reconciliation.", opts.Namespace)
	}
	// Create a new Cmd to provide shared dependencies and start components
	mgr, err := manager.New(cfg, opts)
	if err != nil {
		klog.Fatal(err)
	}

	klog.Infof("Registering Components.")

	// Setup Scheme for all resources
	if err := mapiv1.AddToScheme(mgr.GetScheme()); err != nil {
		klog.Fatal(err)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr, opts, nodelink.Add); err != nil {
		klog.Fatal(err)
	}

	klog.Info("Starting the Cmd.")

	// Start the Cmd
	if err := mgr.Start(signals.SetupSignalHandler()); err != nil {
		klog.Fatal(err)
	}
}
