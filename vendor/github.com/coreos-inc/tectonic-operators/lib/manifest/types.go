// Package manifest contains functions and constants relevant to working with Kubernetes manifests.
package manifest

import (
	"fmt"

	appsv1beta2 "k8s.io/api/apps/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
)

// Convenience names for supported kinds.
const (
	KindAPIService               = "APIService"
	KindClusterRole              = "ClusterRole"
	KindClusterRoleBinding       = "ClusterRoleBinding"
	KindConfigMap                = "ConfigMap"
	KindCustomResourceDefinition = "CustomResourceDefinition"
	KindDaemonSet                = "DaemonSet"
	KindDeployment               = "Deployment"
	KindIngress                  = "Ingress"
	KindJob                      = "Job"
	KindNetworkPolicy            = "NetworkPolicy"
	KindPod                      = "Pod"
	KindPodDisruptionBudget      = "PodDisruptionBudget"
	KindPodSecurityPolicy        = "PodSecurityPolicy"
	KindRole                     = "Role"
	KindRoleBinding              = "RoleBinding"
	KindSecret                   = "Secret"
	KindService                  = "Service"
	KindServiceAccount           = "ServiceAccount"
)

// CustomResource is a convenience wrapper around unstructured for objects that are known to be
// CustomResources.
type CustomResource struct {
	*unstructured.Unstructured
}

// ComponentName resturns the name of the object used to set task status.
func ComponentName(obj metav1.Object) string {
	kind := ComponentKind(obj)
	if obj.GetNamespace() == "" {
		return fmt.Sprintf("%s/%s", kind, obj.GetName())
	}
	return fmt.Sprintf("%s/%s/%s", kind, obj.GetNamespace(), obj.GetName())
}

// ComponentKind returns the kind of the object.
func ComponentKind(obj metav1.Object) string {
	switch obj := obj.(type) {
	case *apiregistrationv1beta1.APIService:
		return KindAPIService
	case *rbacv1.ClusterRole:
		return KindClusterRole
	case *rbacv1.ClusterRoleBinding:
		return KindClusterRoleBinding
	case *v1.ConfigMap:
		return KindConfigMap
	case *CustomResource:
		return obj.GetKind()
	case *v1beta1ext.CustomResourceDefinition:
		return KindCustomResourceDefinition
	case *appsv1beta2.DaemonSet:
		return KindDaemonSet
	case *appsv1beta2.Deployment:
		return KindDeployment
	case *extensionsv1beta1.Ingress:
		return KindIngress
	case *netv1.NetworkPolicy:
		return KindNetworkPolicy
	case *batchv1.Job:
		return KindJob
	case *rbacv1.Role:
		return KindRole
	case *rbacv1.RoleBinding:
		return KindRoleBinding
	case *v1.Secret:
		return KindSecret
	case *v1.Service:
		return KindService
	case *v1.ServiceAccount:
		return KindServiceAccount
	default:
		return fmt.Sprintf("%T", obj)
	}
}
