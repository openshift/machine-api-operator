package xoperator

import (
	"fmt"

	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/versionhandler"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/xoperator/components"
)

// updateWorker puts update work in the work queue if a new udate is triggered.
func (xo *xoperator) updateWorker() {
	if err := xo.checkPause(); err != nil {
		glog.Errorf("Error checking for pause status: %v", err)
		return
	}

	fs, err := xo.checkFailureStatus()
	if err != nil {
		glog.Errorf("Error checking failure status: %v", err)
		return
	}

	if fs != nil {
		glog.Infof("Non-empty failure status %q, not proceeding upgrade before the failure is cleared", fs)
		return
	}

	av, err := xo.client.GetAppVersion(xo.appVersionNamespace, xo.appVersionName)
	if err != nil {
		glog.Errorf("Error getting AppVersion %s: %v", xo.appVersionName, err)
		return
	}

	// New version: using the upgradereq and upgradecomp.
	req := av.UpgradeReq
	comp := av.UpgradeComp

	if req == comp && !xo.enableReconcile {
		glog.V(4).Infof("Upgrade not triggered, req: %v, comp: %v, skipping", req, comp)
		return
	}

	glog.Infof("Upgrade triggered, req: %v, comp: %v", req, comp)

	if !xo.enableReconcile {
		// If reconcile is enabled then the syncer will already be running.
		stop := make(chan struct{})
		defer close(stop)
		xo.cache = setupCache(xo.client, stop)
	}

	// Generate manifests and perform update.
	specList := xo.renderer()

	if err := xo.update(specList); err != nil {
		glog.Errorf("Error updating: %v", err)
		return
	}

	if err := xo.prune(specList); err != nil {
		glog.Errorf("Error pruning: %v", err)
		return
	}
}

// update tries to update the manifests to the desired versions.
func (xo *xoperator) update(specList []types.UpgradeSpec) error {
	glog.Infof("Start upgrading")

	if _, err := xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
		appVersion.Status.FailureStatus = nil
		appVersion.Status.TaskStatuses = nil
		return nil
	}); err != nil {
		return fmt.Errorf("error updating AppVersion: %v", err)
	}

	if err := xo.setInitialTaskStatuses(specList); err != nil {
		return err
	}

	// Run all registered BeforeUpdate version handlers
	if err := xo.versionHandler.Run(versionhandler.BeforeUpdate, xo.client); err != nil {
		return fmt.Errorf("one or more pre upgrade migrations failed: %v", err)
	}

	for _, us := range specList {
		if err := xo.checkPause(); err != nil {
			return fmt.Errorf("error checking for pause status: %v", err)
		}

		mig, ok := us.Spec.(*batchv1.Job)
		if ok {
			if err := xo.runMigration(mig); err != nil {
				return fmt.Errorf("error running migration %s/%s: %v", mig.GetNamespace(), mig.GetName(), err)
			}
			continue
		}

		// Get the modified manifest.
		modified := us.Spec

		// Get current manifest.
		current, err := getCurrentObject(xo.client, modified)
		if err != nil {
			if !errors.IsNotFound(err) {
				glog.Errorf("Failed to get current object %s/%s: %v", modified.GetNamespace(), modified.GetName(), err)
				return err
			}
			current = nil // Make it nil so in the code below we can distinguish whether the current exists or not.
			glog.Infof("Not current object %s/%s is running", modified.GetNamespace(), modified.GetName())
		}

		// Get the original from 'last-applied' of current, or from disk.
		original, err := getOriginalObjectFromLastApplied(current)
		if err != nil {
			glog.Errorf("Failed to read last-applied manifest: %v", err)
			return err
		}

		if err := setupLastAppliedAnnotation(original, current, modified); err != nil {
			glog.Errorf("Failed to setup last-applied annotation: %v", err)
			return err
		}

		if err := xo.updateComponent(original, modified, us.UpgradeStrategy, us.UpgradeBehaviour); err != nil {
			return err
		}
	}

	if _, err := xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
		switch {
		case appVersion.UpgradeReq > appVersion.UpgradeComp:
			appVersion.UpgradeComp++
		case appVersion.UpgradeReq < appVersion.UpgradeComp:
			appVersion.UpgradeComp--
		default:
			glog.Warningf("UpgradeComp %q already set to UpgradeReq %q", appVersion.UpgradeComp, appVersion.UpgradeReq)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("error updating update finished status: %v", err)
	}

	// Run all registered AfterUpdate version handlers
	if err := xo.versionHandler.Run(versionhandler.AfterUpdate, xo.client); err != nil {
		return fmt.Errorf("one or more post upgrade migrations failed: %v", err)
	}

	glog.Info("Update finished")
	return nil
}

// prune deletes any existing components that are not listed in specList.
func (xo *xoperator) prune(specList []types.UpgradeSpec) error {
	glog.Infof("Start pruning unwanted components")

	sel, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchLabels: map[string]string{
			constants.XOperatorManagedByLabelKey: xo.operatorName,
		},
	})
	if err != nil {
		return fmt.Errorf("error creating selector: %v", err)
	}

	var errs []error
	var cur []types.Component
	for _, obj := range components.SupportedObjects {
		cmp, err := xo.componentFromObject(obj)
		if err != nil {
			errs = append(errs, fmt.Errorf("error listing objects: %v", err))
			continue
		}
		cmps, err := cmp.List("", sel)
		if err != nil {
			errs = append(errs, fmt.Errorf("error listing objects: %v", err))
			continue
		}
		cur = append(cur, cmps...)
	}

	for _, cmp := range cur {
		obj := cmp.Definition()
		_, found := types.FindComponent(specList, obj, obj.GetNamespace(), obj.GetName())
		if !found {
			// this object is not present in current spec.
			// this object should be garbage collected ie. deleted.
			glog.Infof("Pruning %s", manifest.ComponentName(cmp))
			delPolicy := metav1.DeletePropagationForeground
			if err := cmp.Delete(&metav1.DeleteOptions{PropagationPolicy: &delPolicy}); err != nil {
				errs = append(errs, fmt.Errorf("error pruning %s: %v", types.Component(cmp), err))
			}
		}
	}

	glog.Infof("Prune finished")

	return utilerrors.NewAggregate(errs)
}
