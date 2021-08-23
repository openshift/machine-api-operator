/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"flag"
	"log"
	"time"

	"github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/controller"
	"github.com/openshift/machine-api-operator/pkg/controller/machineset"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/util"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	defaultWebhookPort    = 8443
	defaultWebhookCertdir = "/etc/machine-api-operator/tls"
)

func main() {
	flag.Set("logtostderr", "true")
	klog.InitFlags(nil)
	watchNamespace := flag.String("namespace", "",
		"Namespace that the controller watches to reconcile cluster-api objects. If unspecified, the controller watches for cluster-api objects across all namespaces.")
	metricsAddress := flag.String("metrics-bind-address", metrics.DefaultMachineSetMetricsAddress, "Address for hosting metrics")

	webhookEnabled := flag.Bool("webhook-enabled", true,
		"Webhook server, enabled by default. When enabled, the manager will run a webhook server.")

	webhookPort := flag.Int("webhook-port", defaultWebhookPort,
		"Webhook Server port, only used when webhook-enabled is true.")

	webhookCertdir := flag.String("webhook-cert-dir", defaultWebhookCertdir,
		"Webhook cert dir, only used when webhook-enabled is true.")

	healthAddr := flag.String(
		"health-addr",
		":9441",
		"The address for health checking.",
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

	leaderElectLeaseDuration := flag.Duration(
		"leader-elect-lease-duration",
		util.LeaseDuration,
		"The duration that non-leader candidates will wait after observing a leadership renewal until attempting to acquire leadership of a led but unrenewed leader slot. This is effectively the maximum duration that a leader can be stopped before it is replaced by another candidate. This is only applicable if leader election is enabled.",
	)

	flag.Parse()
	if *watchNamespace != "" {
		log.Printf("Watching cluster-api objects only in namespace %q for reconciliation.", *watchNamespace)
	}

	log.Printf("Registering Components.")
	// Get a config to talk to the apiserver
	cfg, err := config.GetConfig()
	if err != nil {
		log.Fatal(err)
	}

	// Create a new Cmd to provide shared dependencies and start components
	syncPeriod := 10 * time.Minute
	opts := manager.Options{
		MetricsBindAddress:      *metricsAddress,
		SyncPeriod:              &syncPeriod,
		Namespace:               *watchNamespace,
		HealthProbeBindAddress:  *healthAddr,
		LeaderElection:          *leaderElect,
		LeaderElectionNamespace: *leaderElectResourceNamespace,
		LeaderElectionID:        "cluster-api-provider-machineset-leader",
		LeaseDuration:           leaderElectLeaseDuration,
		RetryPeriod:             util.TimeDuration(util.RetryPeriod),
		RenewDeadline:           util.TimeDuration(util.RenewDeadline),
	}

	mgr, err := manager.New(cfg, opts)
	if err != nil {
		log.Fatal(err)
	}

	// Enable defaulting and validating webhooks
	machineDefaulter, err := v1beta1.NewMachineDefaulter()
	if err != nil {
		log.Fatal(err)
	}

	machineValidator, err := v1beta1.NewMachineValidator(mgr.GetClient())
	if err != nil {
		log.Fatal(err)
	}

	machineSetDefaulter, err := v1beta1.NewMachineSetDefaulter()
	if err != nil {
		log.Fatal(err)
	}

	machineSetValidator, err := v1beta1.NewMachineSetValidator(mgr.GetClient())
	if err != nil {
		log.Fatal(err)
	}

	if *webhookEnabled {
		mgr.GetWebhookServer().Port = *webhookPort
		mgr.GetWebhookServer().CertDir = *webhookCertdir
		mgr.GetWebhookServer().Register(v1beta1.DefaultMachineMutatingHookPath, &webhook.Admission{Handler: machineDefaulter})
		mgr.GetWebhookServer().Register(v1beta1.DefaultMachineValidatingHookPath, &webhook.Admission{Handler: machineValidator})
		mgr.GetWebhookServer().Register(v1beta1.DefaultMachineSetMutatingHookPath, &webhook.Admission{Handler: machineSetDefaulter})
		mgr.GetWebhookServer().Register(v1beta1.DefaultMachineSetValidatingHookPath, &webhook.Admission{Handler: machineSetValidator})
	}

	log.Printf("Registering Components.")

	// Setup Scheme for all resources
	if err := v1beta1.AddToScheme(mgr.GetScheme()); err != nil {
		log.Fatal(err)
	}

	// Setup all Controllers
	if err := controller.AddToManager(mgr, opts, machineset.Add); err != nil {
		log.Fatal(err)
	}

	if err := mgr.AddReadyzCheck("ping", healthz.Ping); err != nil {
		klog.Fatal(err)
	}

	if err := mgr.AddHealthzCheck("ping", healthz.Ping); err != nil {
		klog.Fatal(err)
	}

	log.Printf("Starting the Cmd.")

	// Start the Cmd
	log.Fatal(mgr.Start(signals.SetupSignalHandler()))
}
