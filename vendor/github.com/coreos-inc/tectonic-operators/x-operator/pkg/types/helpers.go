package types

import (
	"fmt"
	"reflect"

	appsv1beta2 "k8s.io/api/apps/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
	"github.com/coreos-inc/tectonic-operators/lib/manifest/marshal"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
)

// FindComponent finds component in upgrade definition that matches type of interface i, namespace and name.
func (ud *UpgradeDefinition) FindComponent(i interface{}, namespace, name string) (metav1.Object, bool) {
	return FindComponent(ud.Items, i, namespace, name)
}

// UpgradeSpecFromBytes converts the manifest data into upgrade spec. To unmarshal CRD instances you
// must first register the CRD with the marshal library using
// marshal.RegisterCustomResourceDefinition().
func UpgradeSpecFromBytes(b []byte) (UpgradeSpec, error) {
	obj, err := marshal.ObjectFromBytes(b)
	if err != nil {
		return UpgradeSpec{}, err
	}

	switch obj := obj.(type) {
	case *apiregistrationv1beta1.APIService,
		*rbacv1.ClusterRole,
		*rbacv1.ClusterRoleBinding,
		*v1.ConfigMap,
		*manifest.CustomResource,
		*v1beta1ext.CustomResourceDefinition,
		*appsv1beta2.DaemonSet,
		*appsv1beta2.Deployment,
		*extensionsv1beta1.Ingress,
		*netv1.NetworkPolicy,
		*rbacv1.Role,
		*rbacv1.RoleBinding,
		*v1.Secret,
		*v1.Service,
		*v1.ServiceAccount:
		upgradeStrategy, err := upgradeStrategyFromObject(obj)
		if err != nil {
			return UpgradeSpec{}, err
		}
		upgradeBehaviour, err := upgradeBehaviourFromObject(obj)
		if err != nil {
			return UpgradeSpec{}, err
		}
		return UpgradeSpec{
			Spec:             obj,
			UpgradeStrategy:  upgradeStrategy,
			UpgradeBehaviour: upgradeBehaviour,
		}, nil
	case *batchv1.Job:
		return UpgradeSpec{Spec: obj}, nil
	default:
		return UpgradeSpec{}, fmt.Errorf("unsupported object type: %T", obj)
	}
}

// upgradeStrategyFromObject reads out the "upgrade strategy" from the object.
func upgradeStrategyFromObject(obj metav1.Object) (constants.UpgradeStrategy, error) {
	anno := obj.GetAnnotations()
	val := anno[constants.XOperatorUpgradeStrategyAnnotationKey]
	switch val {
	case "":
		// default case when unset/empty is Patch
		return constants.UpgradeStrategyPatch, nil
	case constants.UpgradeStrategyReplace:
		return constants.UpgradeStrategyReplace, nil
	case constants.UpgradeStrategyPatch:
		return constants.UpgradeStrategyPatch, nil
	case constants.UpgradeStrategyDeleteAndRecreate:
		return constants.UpgradeStrategyDeleteAndRecreate, nil
	default:
		return "", fmt.Errorf("unknown UpgradeStrategy found: %s", val)
	}
}

// upgradeBehaviourFromObject reads out the "upgrade behavior" from the object.
func upgradeBehaviourFromObject(obj metav1.Object) (constants.UpgradeBehaviour, error) {
	anno := obj.GetAnnotations()
	val := anno[constants.XOperatorUpgradeBehaviourAnnotationKey]
	switch val {
	case "":
		// default case when unset/empty is CreateOrUpgrade.
		return constants.UpgradeBehaviourCreateOrUpgrade, nil
	case constants.UpgradeBehaviourCreateOrUpgrade:
		return constants.UpgradeBehaviourCreateOrUpgrade, nil
	case constants.UpgradeBehaviourUpgradeIfExists:
		return constants.UpgradeBehaviourUpgradeIfExists, nil
	default:
		return "", fmt.Errorf("unknown UpgradeBehaviour found: %s", val)
	}
}

// FindComponent finds component in specList that matches type of interface i, namespace and name.
func FindComponent(specList []UpgradeSpec, i interface{}, namespace, name string) (metav1.Object, bool) {
	t := reflect.TypeOf(i)

	for _, us := range specList {
		if reflect.TypeOf(us.Spec) == t && us.Spec.GetNamespace() == namespace && us.Spec.GetName() == name {
			return us.Spec, true
		}
	}
	return nil, false
}
