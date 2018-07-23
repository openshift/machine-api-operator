package xoperator

import (
	"fmt"
	"sync"
	"time"

	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
)

// checkPause blocks until the upgrade is not paused.
func (xo *xoperator) checkPause() error {
	var once sync.Once
	err := wait.PollInfinite(time.Second, func() (bool, error) {
		vu, err := xo.client.GetAppVersion(xo.appVersionNamespace, xo.appVersionName)
		if err != nil {
			glog.Errorf("error checking for pause status: %v", err)
			return false, err
		}
		// Update our status to match the spec.
		if vu.Spec.Paused != vu.Status.Paused {
			var err error
			if vu, err = xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
				appVersion.Status.Paused = appVersion.Spec.Paused
				return nil
			}); err != nil {
				return false, fmt.Errorf("error while updating pause status: %v", err)
			}
		}
		// Stay in wait loop if we're paused.
		if vu.Status.Paused {
			once.Do(func() { glog.Info("version operator paused") })
			return false, nil
		}
		return true, nil
	})
	return err
}

// setInitialTaskStatuses sets the initial tasks status list for all the upgradable components.
func (xo *xoperator) setInitialTaskStatuses(items []types.UpgradeSpec) error {
	var taskStatuses []optypes.TaskStatus
	for _, item := range items {
		taskStatuses = append(taskStatuses, optypes.TaskStatus{
			Name:  manifest.ComponentName(item.Spec),
			State: optypes.TaskStateNotStarted,
		})
	}
	return xo.setTaskStatuses(taskStatuses)
}

// setTaskStatus updates the task status.
func (xo *xoperator) setTaskStatus(ts optypes.TaskStatus) (*optypes.AppVersion, error) {
	return xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
		var found bool
		for i, v := range appVersion.Status.TaskStatuses {
			if v.Name == ts.Name {
				appVersion.Status.TaskStatuses[i] = ts
				found = true
				break
			}
		}
		if !found {
			appVersion.Status.TaskStatuses = append(appVersion.Status.TaskStatuses, ts)
		}
		return nil
	})
}

// setTaskStatuses updates the task status list in one shot.
func (xo *xoperator) setTaskStatuses(ts []optypes.TaskStatus) error {
	_, err := xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
		appVersion.Status.TaskStatuses = ts
		return nil
	})
	return err
}

// checkFailureStatus returns the failure status.
func (xo *xoperator) checkFailureStatus() (*optypes.FailureStatus, error) {
	av, err := xo.client.GetAppVersion(xo.appVersionNamespace, xo.appVersionName)
	if err != nil {
		return nil, err
	}
	return av.Status.FailureStatus, nil
}

func (xo *xoperator) setFailureStatus(reason string) error {
	if _, err := xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
		appVersion.Status.FailureStatus = &optypes.FailureStatus{
			Reason: reason,
			Type:   optypes.FailureTypeUpdatesNotPossible,
		}
		return nil
	}); err != nil {
		return fmt.Errorf("error updating AppVersion failure status: %v", err)
	}
	return nil
}
