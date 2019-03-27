package main

import (
	"flag"
	"net/http"
	"os"

	"github.com/golang/glog"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
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
