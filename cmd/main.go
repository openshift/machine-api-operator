package main

import (
	"flag"

	"github.com/golang/glog"

	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/coreos-inc/tectonic-operators/operator/machine-api/pkg/render"
	machineAPI "github.com/coreos-inc/tectonic-operators/operator/machine-api/pkg/types"
	xotypes "github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/xoperator"
)

var (
	kubeconfig  string
	manifestDir string
	configPath  string
)

func init() {
	flag.Set("logtostderr", "true")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Kubeconfig file to access a remote cluster. Warning: For testing only, do not use in production.")
	flag.StringVar(&manifestDir, "manifest-dir", "/manifests", "Path to dir with manifest templates.")
	flag.StringVar(&configPath, "config", "/etc/cluster-config/machine-api", "Cluster config file from which to obtain configuration options")
	flag.Parse()
}

func main() {
	if err := xoperator.Run(xoperator.Config{
		Client: opclient.NewClient(kubeconfig),
		LeaderElectionConfig: xoperator.LeaderElectionConfig{
			Kubeconfig: kubeconfig,
			Namespace:  optypes.TectonicNamespace,
		},
		OperatorName:   machineAPI.MachineAPIOperatorName,
		AppVersionName: machineAPI.MachineAPIVersionName,
		Renderer:       rendererFromFile,
	}); err != nil {
		glog.Fatalf("Failed to run machine-api operator: %v", err)
	}
}

// rendererFromFile reads the config object on demand from the path and then passes it to the
// renderer.
func rendererFromFile() []xotypes.UpgradeSpec {
	config, err := render.Config(configPath)
	if err != nil {
		glog.Exitf("Error reading machine-api config: %v", err)
	}
	return render.MakeRenderer(config, manifestDir)()
}
