package main

import (
	"flag"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

const (
	componentName      = "version"
	componentNamespace = "openshift-cluster-version"
)

var (
	rootCmd = &cobra.Command{
		Use:   componentName,
		Short: "Run Cluster Version Controller",
		Long:  "",
	}

	rootOpts struct {
		releaseImage string
	}
)

func init() {
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
	rootCmd.PersistentFlags().StringVar(&rootOpts.releaseImage, "release-image", "", "The Openshift release image url.")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Exitf("Error executing mcc: %v", err)
	}
}
