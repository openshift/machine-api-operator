package cvo

import (
	"fmt"
	"strings"

	"github.com/golang/glog"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/cluster-version-operator/lib"
	"github.com/openshift/cluster-version-operator/lib/resourcebuilder"
	"github.com/openshift/cluster-version-operator/pkg/cvo/internal"
)

const (
	// RequeueOnErrorAnnotationKey is key for annotation on a manifests object that instructs CVO to requeue on specific errors.
	// The value is comma separated list of causes that forces requeue.
	RequeueOnErrorAnnotationKey = "v1.cluster-version-operator.operators.openshift.io/requeue-on-error"

	// RequeueOnErrorCauseNoMatch is used when no match is found for object in api.
	// This maps to https://godoc.org/k8s.io/apimachinery/pkg/api/meta#NoKindMatchError and https://godoc.org/k8s.io/apimachinery/pkg/api/meta#NoResourceMatchError .
	// https://godoc.org/k8s.io/apimachinery/pkg/api/meta#IsNoMatchError is used as a check.
	RequeueOnErrorCauseNoMatch = "NoMatch"
)

// This is used to map the know causes to their check.
var requeueOnErrorCauseToCheck = map[string]func(error) bool{
	RequeueOnErrorCauseNoMatch: meta.IsNoMatchError,
}

// loadUpdatePayload reads the payload from disk or remote, as necessary.
func (optr *Operator) loadUpdatePayload(config *configv1.ClusterVersion) (*updatePayload, error) {
	payloadDir, err := optr.updatePayloadDir(config)
	if err != nil {
		return nil, err
	}
	releaseImage := optr.releaseImage
	if config.Spec.DesiredUpdate != nil {
		releaseImage = config.Spec.DesiredUpdate.Payload
	}
	return loadUpdatePayload(payloadDir, releaseImage)
}

// syncUpdatePayload applies the manifests in the payload to the cluster.
func (optr *Operator) syncUpdatePayload(config *configv1.ClusterVersion, payload *updatePayload) error {
	version := payload.releaseVersion
	if len(version) == 0 {
		version = payload.releaseImage
	}

	total := len(payload.manifests)
	done := 0
	var tasks []*syncTask
	for i := range payload.manifests {
		tasks = append(tasks, &syncTask{
			index:    i + 1,
			total:    total,
			manifest: &payload.manifests[i],
			backoff:  optr.syncBackoff,
		})
	}

	for i := 0; i < len(tasks); i++ {
		task := tasks[i]
		setAppliedAndPending(version, total, done)
		glog.V(4).Infof("Running sync for %s", task)
		glog.V(6).Infof("Manifest: %s", string(task.manifest.Raw))

		ov, ok := getOverrideForManifest(config.Spec.Overrides, task.manifest)
		if ok && ov.Unmanaged {
			glog.V(4).Infof("Skipping %s as unmanaged", task)
			continue
		}

		if err := task.Run(version, optr.restConfig); err != nil {
			cause := errors.Cause(err)
			if task.requeued == 0 && shouldRequeueOnErr(cause, task.manifest) {
				task.requeued++
				tasks = append(tasks, task)
				continue
			}
			return err
		}
		done++
		glog.V(4).Infof("Done syncing for %s", task)
	}
	setAppliedAndPending(version, total, done)
	return nil
}

type syncTask struct {
	index    int
	total    int
	manifest *lib.Manifest
	requeued int
	backoff  wait.Backoff
}

func (st *syncTask) String() string {
	ns := st.manifest.Object().GetNamespace()
	if len(ns) == 0 {
		return fmt.Sprintf("%s %q (%s, %d of %d)", strings.ToLower(st.manifest.GVK.Kind), st.manifest.Object().GetName(), st.manifest.GVK.GroupVersion().String(), st.index, st.total)
	}
	return fmt.Sprintf("%s \"%s/%s\" (%s, %d of %d)", strings.ToLower(st.manifest.GVK.Kind), ns, st.manifest.Object().GetName(), st.manifest.GVK.GroupVersion().String(), st.index, st.total)
}

func (st *syncTask) Run(version string, rc *rest.Config) error {
	var lastErr error
	if err := wait.ExponentialBackoff(st.backoff, func() (bool, error) {
		// build resource builder for manifest
		var b resourcebuilder.Interface
		var err error
		if resourcebuilder.Mapper.Exists(st.manifest.GVK) {
			b, err = resourcebuilder.New(resourcebuilder.Mapper, rc, *st.manifest)
		} else {
			b, err = internal.NewGenericBuilder(rc, *st.manifest)
		}
		if err != nil {
			utilruntime.HandleError(errors.Wrapf(err, "error creating resourcebuilder for %s", st))
			lastErr = err
			metricPayloadErrors.WithLabelValues(version).Inc()
			return false, nil
		}
		// run builder for the manifest
		if err := b.Do(); err != nil {
			utilruntime.HandleError(errors.Wrapf(err, "error running apply for %s", st))
			lastErr = err
			metricPayloadErrors.WithLabelValues(version).Inc()
			return false, nil
		}
		return true, nil
	}); err != nil {
		reason, cause := reasonForPayloadSyncError(lastErr)
		if len(cause) > 0 {
			cause = ": " + cause
		}
		return &updateError{
			cause:   lastErr,
			Reason:  reason,
			Message: fmt.Sprintf("Could not update %s%s", st, cause),
		}
	}
	return nil
}

func shouldRequeueOnErr(err error, manifest *lib.Manifest) bool {
	ok, errs := hasRequeueOnErrorAnnotation(manifest.Object().GetAnnotations())
	if !ok {
		return false
	}
	cause := errors.Cause(err)

	should := false
	for _, e := range errs {
		if ef, ok := requeueOnErrorCauseToCheck[e]; ok {
			if ef(cause) {
				should = true
				break
			}
		}
	}
	return should
}

func hasRequeueOnErrorAnnotation(annos map[string]string) (bool, []string) {
	if annos == nil {
		return false, nil
	}
	errs, ok := annos[RequeueOnErrorAnnotationKey]
	if !ok {
		return false, nil
	}
	return ok, strings.Split(errs, ",")
}

func setAppliedAndPending(version string, total, done int) {
	metricPayload.WithLabelValues(version, "pending").Set(float64(total - done))
	metricPayload.WithLabelValues(version, "applied").Set(float64(done))
}

type updateError struct {
	cause   error
	Reason  string
	Message string
}

func (e *updateError) Error() string {
	return e.Message
}

func (e *updateError) Cause() error {
	return e.cause
}

// reasonForUpdateError provides a succint explanation of a known error type for use in a human readable
// message during update. Since all objects in the payload should be successfully applied, messages
// should direct the reader (likely a cluster administrator) to a possible cause in their own config.
func reasonForPayloadSyncError(err error) (string, string) {
	err = errors.Cause(err)
	switch {
	case apierrors.IsNotFound(err), apierrors.IsAlreadyExists(err):
		return "UpdatePayloadResourceNotFound", "resource may have been deleted"
	case apierrors.IsConflict(err):
		return "UpdatePayloadResourceConflict", "someone else is updating this resource"
	case apierrors.IsTimeout(err), apierrors.IsServiceUnavailable(err), apierrors.IsUnexpectedServerError(err):
		return "UpdatePayloadClusterDown", "the server is down or not responding"
	case apierrors.IsInternalError(err):
		return "UpdatePayloadClusterError", "the server is reporting an internal error"
	case apierrors.IsInvalid(err):
		return "UpdatePayloadResourceInvalid", "the object is invalid, possibly due to local cluster configuration"
	case apierrors.IsUnauthorized(err):
		return "UpdatePayloadClusterUnauthorized", "could not authenticate to the server"
	case apierrors.IsForbidden(err):
		return "UpdatePayloadResourceForbidden", "the server has forbidden updates to this resource"
	case apierrors.IsServerTimeout(err), apierrors.IsTooManyRequests(err):
		return "UpdatePayloadClusterOverloaded", "the server is overloaded and is not accepting updates"
	case meta.IsNoMatchError(err):
		return "UpdatePayloadResourceTypeMissing", "the server does not recognize this resource, check extension API servers"
	default:
		return "UpdatePayloadFailed", ""
	}
}

func summaryForReason(reason string) string {
	switch reason {

	// likely temporary errors
	case "UpdatePayloadResourceNotFound", "UpdatePayloadResourceConflict":
		return "some resources could not be updated"
	case "UpdatePayloadClusterDown":
		return "the control plane is down or not responding"
	case "UpdatePayloadClusterError":
		return "the control plane is reporting an internal error"
	case "UpdatePayloadClusterOverloaded":
		return "the control plane is overloaded and is not accepting updates"
	case "UpdatePayloadClusterUnauthorized":
		return "could not authenticate to the server"
	case "UpdatePayloadRetrievalFailed":
		return "could not download the update"

	// likely a policy or other configuration error due to end user action
	case "UpdatePayloadResourceForbidden":
		return "the server is rejecting updates"

	// the payload may not be correct, or the cluster may be in an unexpected
	// state
	case "UpdatePayloadResourceTypeMissing":
		return "a required extension is not available to update"
	case "UpdatePayloadResourceInvalid":
		return "some cluster configuration is invalid"
	case "UpdatePayloadIntegrity":
		return "the contents of the update are invalid"
	}
	if strings.HasPrefix(reason, "UpdatePayload") {
		return "the update could not be applied"
	}
	return "an unknown error has occurred"
}

// getOverrideForManifest returns the override and true when override exists for manifest.
func getOverrideForManifest(overrides []configv1.ComponentOverride, manifest *lib.Manifest) (configv1.ComponentOverride, bool) {
	for idx, ov := range overrides {
		kind, namespace, name := manifest.GVK.Kind, manifest.Object().GetNamespace(), manifest.Object().GetName()
		if ov.Kind == kind &&
			(namespace == "" || ov.Namespace == namespace) && // cluster-scoped objects don't have namespace.
			ov.Name == name {
			return overrides[idx], true
		}
	}
	return configv1.ComponentOverride{}, false
}

func ownerRefModifier(config *configv1.ClusterVersion) resourcebuilder.MetaV1ObjectModifierFunc {
	oref := metav1.NewControllerRef(config, ownerKind)
	return func(obj metav1.Object) {
		obj.SetOwnerReferences([]metav1.OwnerReference{*oref})
	}
}
