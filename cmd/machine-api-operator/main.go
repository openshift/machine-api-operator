package main

import (
	"flag"
	"os"

	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var componentNamespace = "openshift-machine-api"

const (
	componentName = "machine-api-operator"
)

var (
	rootCmd = &cobra.Command{
		Use:   componentName,
		Short: "Run Cluster API Controller",
		Long:  "",
	}
	config string
)

func init() {
	rootCmd.PersistentFlags().AddGoFlagSet(flag.CommandLine)
}

func main() {
	if namespace, ok := os.LookupEnv("COMPONENT_NAMESPACE"); ok {
		componentNamespace = namespace
	}
	if err := rootCmd.Execute(); err != nil {
		klog.Exitf("Error executing mao: %v", err)
	}
}
