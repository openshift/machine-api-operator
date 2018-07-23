package xoperator

import (
	"time"

	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/versionhandler"
)

const (
	// updatePollInterval is the polling interval for checking the AppVersion for updates.
	updatePollInterval = 10 * time.Second
)

// xoperator stores the components that operator manages.
// It implements the XOperator interface.
type xoperator struct {
	client              opclient.Interface
	operatorName        string
	appVersionNamespace string
	appVersionName      string
	manifestDir         string
	enableReconcile     bool
	renderer            Renderer
	cache               *cache
	versionHandler      versionhandler.Handler
}
