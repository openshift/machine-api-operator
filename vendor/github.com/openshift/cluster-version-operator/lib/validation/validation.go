package validation

import (
	"net/url"
	"strings"

	"github.com/blang/semver"
	"github.com/google/uuid"

	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"

	configv1 "github.com/openshift/api/config/v1"
)

func ValidateClusterVersion(config *configv1.ClusterVersion) field.ErrorList {
	errs := apivalidation.ValidateObjectMeta(&config.ObjectMeta, false, apivalidation.NameIsDNS1035Label, nil)

	if len(config.Spec.Upstream) > 0 {
		if _, err := url.Parse(string(config.Spec.Upstream)); err != nil {
			errs = append(errs, field.Invalid(field.NewPath("spec", "upstream"), config.Spec.Upstream, "must be a valid URL or empty"))
		}
	}
	if len(config.Spec.ClusterID) > 0 {
		id, _ := uuid.Parse(string(config.Spec.ClusterID))
		switch {
		case id.Variant() != uuid.RFC4122:
			errs = append(errs, field.Invalid(field.NewPath("spec", "clusterID"), config.Spec.ClusterID, "must be an RFC4122-variant UUID"))
		case id.Version() != 4:
			errs = append(errs, field.Invalid(field.NewPath("spec", "clusterID"), config.Spec.ClusterID, "must be a version-4 UUID"))
		}
	}
	if u := config.Spec.DesiredUpdate; u != nil {
		switch {
		case len(u.Version) == 0 && len(u.Payload) == 0:
			errs = append(errs, field.Required(field.NewPath("spec", "desiredUpdate", "version"), "must specify version or payload"))
		case len(u.Version) > 0 && !validSemVer(u.Version):
			errs = append(errs, field.Invalid(field.NewPath("spec", "desiredUpdate", "version"), u.Version, "must be a semantic version (1.2.3[-...])"))
		}
	}
	return errs
}

func ClearInvalidFields(config *configv1.ClusterVersion, errs field.ErrorList) *configv1.ClusterVersion {
	if len(errs) == 0 {
		return config
	}
	copied := config.DeepCopy()
	for _, err := range errs {
		switch {
		case strings.HasPrefix(err.Field, "spec.desiredUpdate."):
			copied.Spec.DesiredUpdate = nil
		case err.Field == "spec.upstream":
			// TODO: invalid means, don't fetch updates
			copied.Spec.Upstream = ""
		case err.Field == "spec.clusterID":
			copied.Spec.ClusterID = ""
		}
	}
	return copied
}

func validSemVer(version string) bool {
	_, err := semver.Parse(version)
	return err == nil
}
