package xoperator

import (
	"fmt"

	"github.com/golang/glog"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/versionhandler"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/xoperator/components"
)

// WatchDirectory runs an XOperator that parses and stores the manifest files from manifestDir. It
// dies when error occurs in parsing files.
func WatchDirectory(opClient opclient.Interface, operatorName, appVersionName, manifestDir string, enableReconcile bool, stop <-chan struct{}) {
	xo := &xoperator{
		client:              opClient,
		operatorName:        operatorName,
		appVersionNamespace: optypes.TectonicNamespace,
		appVersionName:      appVersionName,
		manifestDir:         manifestDir,
		enableReconcile:     enableReconcile,
		versionHandler:      versionhandler.New(),
	}

	glog.V(2).Info("Starting update worker.")
	defer glog.V(2).Info("Shutting down update worker.")

	if xo.enableReconcile {
		glog.V(2).Info("Reconcile mode is enabled, running informers indefinitely.")
		xo.cache = setupCache(xo.client, stop)
	}

	wait.Until(xo.directoryUpdateWorker, updatePollInterval, stop)
}

// directoryUpdateWorker puts update work in the work queue if a new udate is triggered.
func (xo *xoperator) directoryUpdateWorker() {
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

	// directory version: tying operators to specific version numbers
	curV := av.Status.CurrentVersion
	targetV := av.Spec.DesiredVersion

	var targetUpgradeDef *types.UpgradeDefinition
	var currentUpgradeDef *types.UpgradeDefinition

	empty, err := xo.isManifestDirEmpty()
	if err != nil {
		glog.Errorf("Failed to read manifest dir: %v, will retry", err)
		return
	}
	if empty {
		glog.V(2).Infof("No manifests in manifest dir, will retry")
		return
	}

	currentUpgradeDef, err = ParseManifestsForVersion(xo.manifestDir, curV)
	if err != nil {
		reason := fmt.Sprintf("error parsing current manifests: %v", err)
		xo.setFailureStatus(reason)
		glog.Errorf(reason)
		return
	}

	targetUpgradeDef, err = ParseManifestsForVersion(xo.manifestDir, targetV)
	if err != nil {
		reason := fmt.Sprintf("error parsing target manifests: %v", err)
		xo.setFailureStatus(reason)
		glog.Errorf(reason)
		return
	}

	xo.directorySync(&types.UpgradeDefinitionPair{
		Current: currentUpgradeDef,
		Target:  targetUpgradeDef,
	})
}

// directorySync calls directoryHandleSync() to do the reconciliation or updates.
// It also writes failure status when error happens.
func (xo *xoperator) directorySync(pair *types.UpgradeDefinitionPair) {
	if err := xo.directoryHandleSync(pair); err != nil {
		reason := fmt.Sprintf("error syncing: %v", err)
		glog.Errorf(reason)

		if err := xo.setFailureStatus(reason); err != nil {
			glog.Errorf("error setting failure status: %v", err)
		}
	}
}

// directoryHandleSync dispatches the update or reconcile work.
func (xo *xoperator) directoryHandleSync(pair *types.UpgradeDefinitionPair) error {
	current := pair.Current
	target := pair.Target

	if current == nil || current.Version != target.Version || xo.enableReconcile {
		if !xo.enableReconcile {
			// If reconcile is enabled then the syncer will already be running.
			stop := make(chan struct{})
			defer close(stop)
			xo.cache = setupCache(xo.client, stop)
		}

		if err := xo.directoryUpdate(current, target); err != nil {
			return fmt.Errorf("error updating: %v", err)
		}
		if err := xo.directoryPrune(target); err != nil {
			return fmt.Errorf("error pruning: %v", err)
		}
	}

	return nil
}

// directoryUpdate tries to update the manifests from 'cdef'(current version) to 'tdef' (target
// version).
func (xo *xoperator) directoryUpdate(cdef, tdef *types.UpgradeDefinition) error {
	glog.Infof("Start updating to target: %s", tdef.Version)

	targetV := tdef.Version
	if _, err := xo.client.AtomicUpdateAppVersion(xo.appVersionNamespace, xo.appVersionName, func(appVersion *optypes.AppVersion) error {
		appVersion.Status.TargetVersion = targetV
		appVersion.Status.FailureStatus = nil
		appVersion.Status.TaskStatuses = nil
		return nil
	}); err != nil {
		return fmt.Errorf("error updating AppVersion target version: %v", err)
	}

	if err := xo.setInitialTaskStatuses(tdef.Items); err != nil {
		return err
	}

	// Run all registered BeforeUpdate version handlers
	if err := xo.versionHandler.Run(versionhandler.BeforeUpdate, xo.client); err != nil {
		return fmt.Errorf("one or more pre upgrade migrations failed: %v", err)
	}

	for _, us := range tdef.Items {
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
		original, err := getOriginalObject(cdef, current, modified)
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
		appVersion.Status.CurrentVersion = appVersion.Status.TargetVersion
		appVersion.Status.TargetVersion = ""
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

// directoryPrune deletes any existing components that are not listed in the def.
func (xo *xoperator) directoryPrune(def *types.UpgradeDefinition) error {
	glog.Infof("Start pruning unwanted components")

	av, err := xo.client.GetAppVersion(xo.appVersionNamespace, xo.appVersionName)
	if err != nil {
		return fmt.Errorf("error getting AppVersion %s: %v", xo.appVersionName, err)
	}
	currV := av.Status.CurrentVersion
	if currV != def.Version {
		glog.Infof("Skipping pruning %q: old version", def.Version)
		return nil
	}

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
		_, found := def.FindComponent(obj, obj.GetNamespace(), obj.GetName())
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
	return utilerrors.NewAggregate(errs)
}
