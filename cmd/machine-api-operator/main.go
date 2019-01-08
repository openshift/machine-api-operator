package main

import (
	"flag"
	"net/http"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	componentName      = "machine-api-operator"
	componentNamespace = "openshift-cluster-api"
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
	if err := rootCmd.Execute(); err != nil {
		glog.Exitf("Error executing mao: %v", err)
	}

	r := prometheus.NewRegistry()
	r.MustRegister(
		prometheus.NewGoCollector(),
		prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(r, promhttp.HandlerOpts{}))
	go http.ListenAndServe(":8080", mux)
}
