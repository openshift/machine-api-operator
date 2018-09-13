package main

import (
	"flag"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

const (
	componentName      = "machine-api-operator"
	componentNamespace = "openshift-api-operator"
)

var (
	rootCmd = &cobra.Command{
		Use:   componentName,
		Short: "Run Machine API Controller",
		Long:  "",
	}

	rootOpts struct {
		manifestDir string
		config      string
	}
)

func init() {
	rootCmd.PersistentFlags().StringVar(&rootOpts.manifestDir, "manifest-dir", "/manifests", "Path to dir with manifest templates.")
	rootCmd.PersistentFlags().StringVar(&rootOpts.config, "config", "/etc/mao-config/config", "Cluster config file from which to obtain configuration options")
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		glog.Exitf("Error executing mao: %v", err)
	}
}
