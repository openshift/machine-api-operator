package xoperator

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ghodss/yaml"
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/versionhandler"
)

// Config defines the configuration options that are used for invoking xoperator.Run.
type Config struct {
	// Client is the operator client interface used to communicate with the cluster.
	Client opclient.Interface
	// LeaderElectionConfig contains optional leader election configuration options. If not set
	// (default values) then leader election is not used.
	LeaderElectionConfig
	// EnableReconcile enables reconciliation mode in the x-operator.
	EnableReconcile bool
	// OperatorName is the unique name for this operator.
	OperatorName        string
	AppVersionNamespace string
	// AppVersionName is name of the unique AppVersion CRD used by this operator.
	AppVersionName string
	// Renderer is the client rendering function. It is invoked upon each upgrade request.
	Renderer Renderer
	// BeforeHandlers are functions that run before updates are applied.
	BeforeHandlers []versionhandler.HandlerFunc
	// AfterHandlers are functions that run after updates are applied.
	AfterHandlers []versionhandler.HandlerFunc
}

// LeaderElectionConfig defines the configuration options for optional leader election when calling
// xoperator.Run.
type LeaderElectionConfig struct {
	// Kubeconfig is the path to the Kubeconfig that is used by leader election.
	Kubeconfig string
	// Namespace is the namespace the operator is running in.
	Namespace string
}

// Renderer is a function that is passed into the Config object and runs client rendering logic
// when the x-operator starts or becomes the leader.
type Renderer func() []types.UpgradeSpec

// Run starts an x-operator with the provided config. It does not return.
func Run(config Config) error {
	// Start the x-operator and worker function, optionally under leader election.
	glog.Infof("%s starting", config.OperatorName)

	appVersionNamespace := config.AppVersionNamespace
	if appVersionNamespace == "" {
		appVersionNamespace = optypes.TectonicNamespace
	}
	xo := &xoperator{
		client:              config.Client,
		operatorName:        config.OperatorName,
		appVersionNamespace: appVersionNamespace,
		appVersionName:      config.AppVersionName,
		enableReconcile:     config.EnableReconcile,
		renderer:            config.Renderer,
		versionHandler:      versionhandler.New(),
	}

	for _, h := range config.BeforeHandlers {
		xo.versionHandler.Register(versionhandler.BeforeUpdate, h)
	}
	for _, h := range config.AfterHandlers {
		xo.versionHandler.Register(versionhandler.AfterUpdate, h)
	}

	if config.LeaderElectionConfig.Namespace != "" {
		// Use Hostname as LockIdentity for leader election
		id, err := os.Hostname()
		if err != nil {
			return fmt.Errorf("unable to get hostname for identity: %v", err)
		}
		config.Client.RunLeaderElection(opclient.LeaderElectionConfig{
			Kubeconfig:         config.LeaderElectionConfig.Kubeconfig,
			ComponentName:      config.AppVersionName,
			ConfigMapName:      config.AppVersionName,
			ConfigMapNamespace: config.LeaderElectionConfig.Namespace,
			LeaseDuration:      90 * time.Second,
			RenewDeadline:      60 * time.Second,
			RetryPeriod:        30 * time.Second,
			LockIdentity:       id,
			OnStartedLeading: func(stop <-chan struct{}) {
				glog.Infof("started leading: running %s", config.OperatorName)
				wait.Until(xo.updateWorker, updatePollInterval, stop)
			},
			OnStoppedLeading: func() {
				glog.Infof("stopped leading: running %s", config.OperatorName)
			},
		})
	} else {
		wait.Until(xo.updateWorker, updatePollInterval, wait.NeverStop)
	}
	return nil
}

// Render renders the manifest into the given renderPath.
// The path must be a directory.
// If the path doesn't exist, a new directory will be created.
func Render(render Renderer, renderPath string) error {
	glog.Infof("Render mode, writing manifests to %q", renderPath)

	fi, err := os.Lstat(renderPath)
	if err == nil && !fi.IsDir() {
		return fmt.Errorf("path %q is not a dir", renderPath)
	}
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to lstat the path %q: %v", renderPath, err)
		}
		if err := os.MkdirAll(renderPath, 0755); err != nil {
			return fmt.Errorf("failed to mkdir for %q: %v", renderPath, err)
		}
	}

	for _, spec := range render() {
		ss := spec.Spec
		kind := manifest.ComponentKind(ss)

		// Also handles when namespace is an empty string.
		name := strings.TrimPrefix(fmt.Sprintf("%s-%s-%s.yaml", ss.GetNamespace(), ss.GetName(), strings.ToLower(kind)), "-")
		name = strings.Replace(name, ":", "-", -1)

		dst := filepath.Join(renderPath, name)
		_, err := os.Lstat(dst)
		if err == nil {
			return fmt.Errorf("path %q already exists", dst)
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("failed to lstat path %q: %v", dst, err)
		}

		data, err := yaml.Marshal(spec.Spec)
		if err != nil {
			return fmt.Errorf("failed to marshal %q: %v", spec.Spec.GetName(), err)
		}

		if err := ioutil.WriteFile(dst, data, 0644); err != nil {
			return fmt.Errorf("failed to write file %q: %v", name, err)
		}
	}

	return nil
}
