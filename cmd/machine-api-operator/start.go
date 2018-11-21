package main

import (
	"flag"

	"github.com/golang/glog"
	"github.com/openshift/machine-api-operator/pkg/operator"
	"github.com/openshift/machine-api-operator/pkg/version"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/leaderelection"
)

var (
	startCmd = &cobra.Command{
		Use:   "start",
		Short: "Starts Machine API Operator",
		Long:  "",
		Run:   runStartCmd,
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
}

func runStartCmd(cmd *cobra.Command, args []string) {
	flag.Set("logtostderr", "true")
	flag.Parse()

	// To help debugging, immediately log version
	glog.Infof("Version: %+v", version.Version)

	if startOpts.imagesFile == "" {
		glog.Fatalf("--images-json should not be empty")
	}

	cb, err := NewClientBuilder(startOpts.kubeconfig)
	if err != nil {
		glog.Fatalf("error creating clients: %v", err)
	}
	stopCh := make(chan struct{})
	run := func(stop <-chan struct{}) {

		ctx := CreateControllerContext(cb, stopCh, componentNamespace)
		if err := startControllers(ctx); err != nil {
			glog.Fatalf("error starting controllers: %v", err)
		}

		ctx.KubeNamespacedInformerFactory.Start(ctx.Stop)
		close(ctx.InformersStarted)

		select {}
	}

	leaderelection.RunOrDie(leaderelection.LeaderElectionConfig{
		Lock:          CreateResourceLock(cb, componentNamespace, componentName),
		LeaseDuration: LeaseDuration,
		RenewDeadline: RenewDeadline,
		RetryPeriod:   RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				glog.Fatalf("leaderelection lost")
			},
		},
	})
	panic("unreachable")
}

func startControllers(ctx *ControllerContext) error {
	go operator.New(
		componentNamespace, componentName,
		startOpts.imagesFile,

		config,
		ctx.KubeNamespacedInformerFactory.Core().V1().ServiceAccounts(),
		ctx.KubeNamespacedInformerFactory.Apps().V1().Deployments(),
		ctx.KubeNamespacedInformerFactory.Rbac().V1().ClusterRoles(),
		ctx.KubeNamespacedInformerFactory.Rbac().V1().ClusterRoleBindings(),

		ctx.ClientBuilder.KubeClientOrDie(componentName),
		ctx.ClientBuilder.OpenshiftClientOrDie(componentName),
	).Run(2, ctx.Stop)

	return nil
}
