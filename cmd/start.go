package main

import (
	"flag"

	"github.com/golang/glog"
	"github.com/openshift/machine-api-operator/cmd/common"
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
		glog.Fatal("--images-json cannot be empty")
	}

	cb, err := common.NewClientBuilder(startOpts.kubeconfig)
	if err != nil {
		glog.Fatalf("error creating clients: %v", err)
	}
	stopCh := make(chan struct{})
	run := func(stop <-chan struct{}) {

		ctx := common.CreateControllerContext(cb, stopCh, componentNamespace)
		if err := startControllers(ctx); err != nil {
			glog.Fatalf("error starting controllers: %v", err)
		}

		ctx.NamespacedInformerFactory.Start(ctx.Stop)
		ctx.KubeInformerFactory.Start(ctx.Stop)
		ctx.KubeNamespacedInformerFactory.Start(ctx.Stop)
		ctx.APIExtInformerFactory.Start(ctx.Stop)
		close(ctx.KubeInformersStarted)

		select {}
	}

	leaderelection.RunOrDie(leaderelection.LeaderElectionConfig{
		Lock:          common.CreateResourceLock(cb, componentNamespace, componentName),
		LeaseDuration: common.LeaseDuration,
		RenewDeadline: common.RenewDeadline,
		RetryPeriod:   common.RetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				glog.Fatalf("leaderelection lost")
			},
		},
	})
	panic("unreachable")
}

func startControllers(ctx *common.ControllerContext) error {
	go operator.New(
		componentNamespace, componentName,
		startOpts.imagesFile,

		config,
		ctx.NamespacedInformerFactory.Cluster().V1alpha1().MachineSets(),
		ctx.KubeInformerFactory.Core().V1().ConfigMaps(),
		ctx.KubeNamespacedInformerFactory.Core().V1().ServiceAccounts(),
		ctx.APIExtInformerFactory.Apiextensions().V1beta1().CustomResourceDefinitions(),
		ctx.KubeNamespacedInformerFactory.Apps().V1().Deployments(),
		ctx.KubeNamespacedInformerFactory.Rbac().V1().ClusterRoles(),
		ctx.KubeNamespacedInformerFactory.Rbac().V1().ClusterRoleBindings(),

		ctx.ClientBuilder.KubeClientOrDie(componentName),
		ctx.ClientBuilder.APIExtClientOrDie(componentName),
		ctx.ClientBuilder.APIRegistrationClientOrDie(componentName),
		ctx.ClientBuilder.ClusterversionClientOrDie(componentName),
		ctx.ClientBuilder.ClusterAPIClientOrDie(componentName),
	).Run(2, ctx.Stop)

	return nil
}
