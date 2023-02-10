package main

import (
	"flag"
	"fmt"

	"github.com/openshift/machine-api-operator/pkg/version"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
)

var (
	versionCmd = &cobra.Command{
		Use:   "version",
		Short: "Print the version number of Machine API Operator",
		Long:  `All software has versions. This is Machine API Operator's.`,
		Run:   runVersionCmd,
	}
)

func init() {
	rootCmd.AddCommand(versionCmd)
}

func runVersionCmd(cmd *cobra.Command, args []string) {
	if err := flag.Set("logtostderr", "true"); err != nil {
		klog.Fatalf("failed to set logtostderr flag: %v", err)
	}
	flag.Parse()

	program := "MachineAPIOperator"
	version := "v" + version.Version.String()

	fmt.Println(program, version)
}
