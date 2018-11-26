package main

import (
	"flag"

	"github.com/golang/glog"
	"github.com/spf13/cobra"

	"github.com/openshift/cluster-version-operator/pkg/cvo"
)

var (
	renderCmd = &cobra.Command{
		Use:   "render",
		Short: "Renders the UpdatePayload to disk.",
		Long:  "",
		Run:   runRenderCmd,
	}

	renderOpts struct {
		outputDir string
	}
)

func init() {
	rootCmd.AddCommand(renderCmd)
	renderCmd.PersistentFlags().StringVar(&renderOpts.outputDir, "output-dir", "", "The output directory where the manifests will be rendered.")
}

func runRenderCmd(cmd *cobra.Command, args []string) {
	flag.Set("logtostderr", "true")
	flag.Parse()

	if renderOpts.outputDir == "" {
		glog.Fatalf("missing --output-dir flag, it is required")
	}
	if rootOpts.releaseImage == "" {
		glog.Fatalf("missing --release-image flag, it is required")
	}
	if err := cvo.Render(renderOpts.outputDir, rootOpts.releaseImage); err != nil {
		glog.Fatalf("Render command failed: %v", err)
	}
}
