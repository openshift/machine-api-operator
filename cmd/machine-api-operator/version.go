package main

import (
	"flag"
	"fmt"

	"github.com/openshift/machine-api-operator/pkg/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
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
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "openshift_cluster_openshift_apiserver_operator_build_info",
			Help: "A metric with a constant '1' value labeled by major, minor, git commit & git version from which OpenShift Service Serving Cert Signer was built.",
		},
		[]string{"Version"},
	)
	buildInfo.WithLabelValues(version.Version.String()).Set(1)

	prometheus.MustRegister(buildInfo)
}

func runVersionCmd(cmd *cobra.Command, args []string) {
	flag.Set("logtostderr", "true")
	flag.Parse()

	program := "MachineAPIOperator"
	version := "v" + version.Version.String()

	fmt.Println(program, version)
}
