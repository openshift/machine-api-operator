package components

import (
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	"k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
)

// SupportedObjects is list of objects that are supported.
var SupportedObjects = []metav1.Object{
	&apiregistrationv1beta1.APIService{},
	&rbacv1.ClusterRole{},
	&rbacv1.ClusterRoleBinding{},
	&v1.ConfigMap{},
	&v1beta1ext.CustomResourceDefinition{},
	&appsv1beta2.DaemonSet{},
	&appsv1beta2.Deployment{},
	&extensionsv1beta1.Ingress{},
	&netv1.NetworkPolicy{},
	&rbacv1.Role{},
	&rbacv1.RoleBinding{},
	&v1.Secret{},
	&v1.Service{},
	&v1.ServiceAccount{},
}
