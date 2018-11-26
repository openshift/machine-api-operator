package cvo

import (
	"bytes"
	"fmt"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	configv1 "github.com/openshift/api/config/v1"
	configclientv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/cluster-version-operator/lib/resourcemerge"
)

// ClusterVersionInvalid indicates that the cluster version has an error that prevents the server from
// taking action. The cluster version operator will only reconcile the current state as long as this
// condition is set.
const ClusterVersionInvalid configv1.ClusterStatusConditionType = "Invalid"

// syncInitialObjectStatus ensures that every known condition is either set or primed. errs is a list of
// known validation errors for the cluster version.
func (optr *Operator) syncInitialObjectStatus(original *configv1.ClusterVersion, errs field.ErrorList) (bool, error) {
	config := original.DeepCopy()

	config.Status.Current = optr.currentVersion()
	if updated := optr.getAvailableUpdates().NeedsUpdate(config); updated != nil {
		config = updated
	}

	now := metav1.Now()

	// ensure the initial state of all conditions is set
	if resourcemerge.FindOperatorStatusCondition(config.Status.Conditions, configv1.OperatorAvailable) == nil {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorAvailable, Status: configv1.ConditionFalse, LastTransitionTime: now})
	}
	if resourcemerge.FindOperatorStatusCondition(config.Status.Conditions, configv1.OperatorFailing) == nil {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorFailing, Status: configv1.ConditionFalse, LastTransitionTime: now})
	}
	// We default towards progressing because we know we will at least need to sync
	if resourcemerge.FindOperatorStatusCondition(config.Status.Conditions, configv1.OperatorProgressing) == nil {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			Message:            fmt.Sprintf("Initializing, will work towards %s", optr.desiredVersionString(config)),
			LastTransitionTime: now,
		})
	}
	// other conditions
	if resourcemerge.FindOperatorStatusCondition(config.Status.Conditions, configv1.RetrievedUpdates) == nil {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.RetrievedUpdates, Status: configv1.ConditionFalse, LastTransitionTime: now})
	}

	// update the invalid condition
	if len(errs) > 0 {
		buf := &bytes.Buffer{}
		if len(errs) == 1 {
			fmt.Fprintf(buf, "The cluster version is invalid: %s", errs[0].Error())
		} else {
			fmt.Fprintf(buf, "The cluster version is invalid:\n")
			for _, err := range errs {
				fmt.Fprintf(buf, "* %s\n", err.Error())
			}
		}
		reason := "InvalidClusterVersion"

		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               ClusterVersionInvalid,
			Status:             configv1.ConditionTrue,
			Reason:             reason,
			Message:            buf.String(),
			LastTransitionTime: now,
		})

		if !resourcemerge.IsOperatorStatusConditionTrue(config.Status.Conditions, configv1.OperatorFailing) {
			resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
				Type:               configv1.OperatorProgressing,
				Status:             configv1.ConditionFalse,
				Reason:             reason,
				Message:            fmt.Sprintf("Stopped at %s: the cluster version is invalid", optr.desiredVersionString(config)),
				LastTransitionTime: now,
			})
		}

	} else {
		resourcemerge.RemoveOperatorStatusCondition(&config.Status.Conditions, ClusterVersionInvalid)
	}

	updated, err := applyClusterVersionStatus(optr.client.ConfigV1(), config, original)
	optr.rememberLastUpdate(updated)
	return updated != nil && updated.ResourceVersion != original.ResourceVersion, err
}

func (optr *Operator) syncProgressingStatus(config *configv1.ClusterVersion) error {
	original := config.DeepCopy()

	config.Status.Generation = config.Generation
	config.Status.Current = optr.currentVersion()

	now := metav1.Now()

	// clear the available condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorAvailable, Status: configv1.ConditionFalse, LastTransitionTime: now})

	// preserve the most recent failing condition
	if resourcemerge.IsOperatorStatusConditionNotIn(config.Status.Conditions, configv1.OperatorFailing, configv1.ConditionTrue) {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorFailing, Status: configv1.ConditionFalse, LastTransitionTime: now})
	}

	// set progressing with an accurate summary message
	if c := resourcemerge.FindOperatorStatusCondition(config.Status.Conditions, configv1.OperatorFailing); c != nil && c.Status == configv1.ConditionTrue {
		reason := c.Reason
		msg := summaryForReason(reason)
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			Reason:             reason,
			Message:            fmt.Sprintf("Unable to apply %s: %s", optr.desiredVersionString(config), msg),
			LastTransitionTime: now,
		})
	} else if resourcemerge.IsOperatorStatusConditionTrue(config.Status.Conditions, ClusterVersionInvalid) {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			Message:            fmt.Sprintf("Reconciling %s: the cluster version is invalid", optr.desiredVersionString(config)),
			LastTransitionTime: now,
		})
	} else {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			Message:            fmt.Sprintf("Working towards %s", optr.desiredVersionString(config)),
			LastTransitionTime: now,
		})
	}

	updated, err := applyClusterVersionStatus(optr.client.ConfigV1(), config, original)
	optr.rememberLastUpdate(updated)
	return err
}

func (optr *Operator) syncAvailableStatus(config *configv1.ClusterVersion, current configv1.Update, versionHash string) error {
	original := config.DeepCopy()

	config.Status.Current = current
	config.Status.VersionHash = versionHash
	config.Status.Generation = config.Generation

	now := metav1.Now()
	version := optr.currentVersionString(config)

	// set the available condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
		Type:    configv1.OperatorAvailable,
		Status:  configv1.ConditionTrue,
		Message: fmt.Sprintf("Done applying %s", version),

		LastTransitionTime: now,
	})

	// clear the failure condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorFailing, Status: configv1.ConditionFalse, LastTransitionTime: now})

	// clear the progressing condition
	if resourcemerge.IsOperatorStatusConditionTrue(config.Status.Conditions, ClusterVersionInvalid) {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionFalse,
			Reason:             "InvalidClusterVersion",
			Message:            fmt.Sprintf("Stopped at %s: the cluster version is invalid", optr.desiredVersionString(config)),
			LastTransitionTime: now,
		})
	} else {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:    configv1.OperatorProgressing,
			Status:  configv1.ConditionFalse,
			Message: fmt.Sprintf("Cluster version is %s", version),

			LastTransitionTime: now,
		})
	}

	updated, err := applyClusterVersionStatus(optr.client.ConfigV1(), config, original)
	optr.rememberLastUpdate(updated)
	return err
}

func (optr *Operator) syncPayloadFailingStatus(original *configv1.ClusterVersion, err error) error {
	config := original.DeepCopy()

	config.Status.Generation = config.Generation
	config.Status.Current = optr.currentVersion()

	now := metav1.Now()
	var reason string
	msg := "an error occurred"
	if uErr, ok := err.(*updateError); ok {
		reason = uErr.Reason
		msg = summaryForReason(reason)
	}

	// leave the available condition alone

	// set the failing condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
		Type:               configv1.OperatorFailing,
		Status:             configv1.ConditionTrue,
		Reason:             reason,
		Message:            err.Error(),
		LastTransitionTime: now,
	})

	// update the progressing condition message to indicate there is an error
	if resourcemerge.IsOperatorStatusConditionTrue(config.Status.Conditions, configv1.OperatorProgressing) {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			Reason:             reason,
			Message:            fmt.Sprintf("Unable to apply %s: %s", optr.desiredVersionString(config), msg),
			LastTransitionTime: now,
		})
	} else {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionFalse,
			Reason:             reason,
			Message:            fmt.Sprintf("Error while reconciling %s: %s", optr.desiredVersionString(config), msg),
			LastTransitionTime: now,
		})
	}

	updated, err := applyClusterVersionStatus(optr.client.ConfigV1(), config, original)
	optr.rememberLastUpdate(updated)
	return err
}

func (optr *Operator) syncUpdateFailingStatus(original *configv1.ClusterVersion, err error) error {
	config := original.DeepCopy()

	config.Status.Generation = config.Generation
	config.Status.Current = optr.currentVersion()

	now := metav1.Now()
	var reason string
	msg := "an error occurred"
	if uErr, ok := err.(*updateError); ok {
		reason = uErr.Reason
		msg = summaryForReason(reason)
	}

	// clear the available condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorAvailable, Status: configv1.ConditionFalse, LastTransitionTime: now})

	// set the failing condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
		Type:               configv1.OperatorFailing,
		Status:             configv1.ConditionTrue,
		Reason:             reason,
		Message:            err.Error(),
		LastTransitionTime: now,
	})

	// update the progressing condition message to indicate there is an error
	if resourcemerge.IsOperatorStatusConditionTrue(config.Status.Conditions, configv1.OperatorProgressing) {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionTrue,
			Reason:             reason,
			Message:            fmt.Sprintf("Unable to apply %s: %s", optr.desiredVersionString(config), msg),
			LastTransitionTime: now,
		})
	} else {
		resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
			Type:               configv1.OperatorProgressing,
			Status:             configv1.ConditionFalse,
			Reason:             reason,
			Message:            fmt.Sprintf("Error while reconciling %s: %s", optr.desiredVersionString(config), msg),
			LastTransitionTime: now,
		})
	}

	updated, err := applyClusterVersionStatus(optr.client.ConfigV1(), config, original)
	optr.rememberLastUpdate(updated)
	return err
}

// syncDegradedStatus handles generic errors in the cluster version. It tries to preserve
// all status fields that it can by using the provided config or loading the latest version
// from the cache (instead of clearing the status).
// if ierr is nil, return nil
// if ierr is not nil, update OperatorStatus as Failing and return ierr
func (optr *Operator) syncFailingStatus(config *configv1.ClusterVersion, ierr error) error {
	if ierr == nil {
		return nil
	}

	// try to reuse the most recent status if available
	if config == nil {
		config, _ = optr.cvLister.Get(optr.name)
	}
	if config == nil {
		config = &configv1.ClusterVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name: optr.name,
			},
		}
	}

	original := config.DeepCopy()

	config.Status.Current = optr.currentVersion()

	now := metav1.Now()
	msg := fmt.Sprintf("Error ensuring the cluster version is up to date: %v", ierr)

	// clear the available condition
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{Type: configv1.OperatorAvailable, Status: configv1.ConditionFalse, LastTransitionTime: now})

	// reset the failing message
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
		Type:               configv1.OperatorFailing,
		Status:             configv1.ConditionTrue,
		Message:            ierr.Error(),
		LastTransitionTime: now,
	})

	// preserve the status of the existing progressing condition
	progressingStatus := configv1.ConditionFalse
	if resourcemerge.IsOperatorStatusConditionTrue(config.Status.Conditions, configv1.OperatorProgressing) {
		progressingStatus = configv1.ConditionTrue
	}
	resourcemerge.SetOperatorStatusCondition(&config.Status.Conditions, configv1.ClusterOperatorStatusCondition{
		Type:               configv1.OperatorProgressing,
		Status:             progressingStatus,
		Message:            msg,
		LastTransitionTime: now,
	})

	updated, err := applyClusterVersionStatus(optr.client.ConfigV1(), config, original)
	optr.rememberLastUpdate(updated)
	if err != nil {
		return err
	}
	return ierr
}

// applyClusterVersionStatus attempts to overwrite the status subresource of required. If
// original is provided it is compared to required and no update will be made if the
// object does not change. The method will retry a conflict by retrieving the latest live
// version and updating the metadata of required. required is modified if the object on
// the server is newer.
func applyClusterVersionStatus(client configclientv1.ClusterVersionsGetter, required, original *configv1.ClusterVersion) (*configv1.ClusterVersion, error) {
	if original != nil && equality.Semantic.DeepEqual(&original.Status, &required.Status) {
		return required, nil
	}
	actual, err := client.ClusterVersions().UpdateStatus(required)
	if apierrors.IsConflict(err) {
		existing, cErr := client.ClusterVersions().Get(required.Name, metav1.GetOptions{})
		if err != nil {
			return nil, cErr
		}
		if existing.UID != required.UID {
			return nil, fmt.Errorf("cluster version was deleted and recreated, cannot update status")
		}
		if equality.Semantic.DeepEqual(&existing.Status, &required.Status) {
			return existing, nil
		}
		required.ObjectMeta = existing.ObjectMeta
		actual, err = client.ClusterVersions().UpdateStatus(required)
	}
	if err != nil {
		return nil, err
	}
	required.ObjectMeta = actual.ObjectMeta
	return actual, nil
}
