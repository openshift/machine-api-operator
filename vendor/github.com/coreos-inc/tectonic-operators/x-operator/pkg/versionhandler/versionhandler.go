package versionhandler

import (
	"fmt"

	"github.com/golang/glog"

	"github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	"github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
)

const (
	// BeforeUpdate is to register handlers before updating
	BeforeUpdate = "beforeUpdate"
	// AfterUpdate is to register handlers after updating
	AfterUpdate = "afterUpdate"
)

var (
	validTimings = map[string]bool{
		BeforeUpdate: true,
		AfterUpdate:  true,
	}
)

// HandlerFunc is the function to be run before/after a upgrade event
type HandlerFunc func(client client.Interface, version *types.AppVersion) error

// Handler keeps track of all HandlerFunc to run before/after an upgrade event
type Handler map[string][]HandlerFunc

// isValid returns if the timing string is a valid timing string
func isValid(timing string) bool {
	return validTimings[timing]
}

// New returns a new Handler
func New() Handler {
	return Handler(make(map[string][]HandlerFunc))
}

// Register will register a handler func with a timing
// Timings are defined above as const strings. They are used to filter when handlers should run, based on when the client calls run with a specific timing.
// ex: a handler is registered with the BeforeUpdate timing, then when the client calls Run(BeforeUpdate) that handler will be run
// NOTE: Handlers are run in the order they are registered.
func (h Handler) Register(timing string, handler HandlerFunc) {
	if !isValid(timing) {
		glog.Fatal("invalid timing for handler func: ", timing)
	}

	// Add the handler func to the timing list
	h[timing] = append(h[timing], handler)
}

// Run calls all handlers in the order they were registerd with the given timing string
// It halts and returns an error on the first handler to fail.
func (h Handler) Run(timing string, client client.Interface) error {
	if !isValid(timing) {
		return fmt.Errorf("invalid timing for run: %s", timing)
	}

	// No-op if there are no handlers registerd
	if len(h[timing]) == 0 {
		return nil
	}

	// Get the AppVersion from the cluster
	version, err := client.GetAppVersion(types.TectonicNamespace, types.AppVersionNameTectonicCluster)
	if err != nil {
		return fmt.Errorf("failed to get AppVersion: %v", err)
	}

	// Run all handlers
	handlers := h[timing]
	for _, f := range handlers {
		if err := f(client, version); err != nil {
			return fmt.Errorf("failed version handler: %v", err)
		}
	}

	return nil
}
