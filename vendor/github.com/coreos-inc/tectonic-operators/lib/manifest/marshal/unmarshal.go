// Package marshal provides utilities for marshaling and unmarshaling Kubernetes objects.
package marshal

import (
	"fmt"

	appsv1beta2 "k8s.io/api/apps/v1beta2"
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/api/core/v1"
	extensionsv1beta1 "k8s.io/api/extensions/v1beta1"
	netv1 "k8s.io/api/networking/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	rbacv1 "k8s.io/api/rbac/v1"
	v1beta1ext "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"

	"github.com/coreos-inc/tectonic-operators/lib/manifest"
)

var (
	supportedCRDs = map[schema.GroupVersionKind]interface{}{}
)

// ObjectFromBytes unmarshals bytes to k8s objects. By default CustomResource instances are not
// supported. To add supported CustomResourceDefinitions use the RegisterCustomResourceDefinition()
// method.
func ObjectFromBytes(b []byte) (metav1.Object, error) {
	udi, _, err := scheme.Codecs.UniversalDecoder().Decode(b, nil, &unstructured.Unstructured{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode spec: %v", err)
	}
	ud, ok := udi.(*unstructured.Unstructured)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *unstructured.Unstructured, got %T", ud)
	}

	if isSupportedCustomResource(ud) {
		return &manifest.CustomResource{Unstructured: ud}, nil
	}

	switch ud.GetKind() {
	case manifest.KindAPIService:
		return apiServiceFromBytes(b)
	case manifest.KindClusterRole:
		return clusterRoleFromBytes(b)
	case manifest.KindClusterRoleBinding:
		return clusterRoleBindingFromBytes(b)
	case manifest.KindConfigMap:
		return configMapFromBytes(b)
	case manifest.KindCustomResourceDefinition:
		return customResourceDefinitionFromBytes(b)
	case manifest.KindDaemonSet:
		return daemonSetFromBytes(b)
	case manifest.KindDeployment:
		return deploymentFromBytes(b)
	case manifest.KindIngress:
		return ingressFromBytes(b)
	case manifest.KindJob:
		return jobFromBytes(b)
	case manifest.KindNetworkPolicy:
		return networkPolicyFromBytes(b)
	case manifest.KindPod:
		return podFromBytes(b)
	case manifest.KindPodDisruptionBudget:
		return podDisruptionBudgetFromBytes(b)
	case manifest.KindPodSecurityPolicy:
		return podSecurityPolicyFromBytes(b)
	case manifest.KindRole:
		return roleFromBytes(b)
	case manifest.KindRoleBinding:
		return roleBindingFromBytes(b)
	case manifest.KindSecret:
		return secretFromBytes(b)
	case manifest.KindService:
		return serviceFromBytes(b)
	case manifest.KindServiceAccount:
		return serviceAccountFromBytes(b)
	}

	return nil, fmt.Errorf("unsupported type: %q", ud.GroupVersionKind())
}

// RegisterCustomResourceDefinition adds the given CRD to the set that will be unmarshalled without
// error by ObjectFromBytes.
func RegisterCustomResourceDefinition(crd *v1beta1ext.CustomResourceDefinition) {
	RegisterCustomResourceDefinitionGVK(schema.GroupVersionKind{
		Group:   crd.Spec.Group,
		Version: crd.Spec.Version,
		Kind:    crd.Spec.Names.Kind,
	})
}

// RegisterCustomResourceDefinitionGVK adds the given CRD (as defined by its GroupVersionKind) to
// the set that will be unmarshalled without error by ObjectFromBytes.
func RegisterCustomResourceDefinitionGVK(gvk schema.GroupVersionKind) {
	supportedCRDs[gvk] = struct{}{}
}

// isSupportedCustomResource returns true if the provided unstructured object is in the set of
// CustomResourceDefinitions that were registered with RegisterCustomResourceDefinition.
func isSupportedCustomResource(u *unstructured.Unstructured) bool {
	_, supported := supportedCRDs[u.GroupVersionKind()]
	return supported
}

// apiServiceFromBytes unmarshals the manifest data into *apiregistrationv1beta1.APIService object.
func apiServiceFromBytes(manifest []byte) (*apiregistrationv1beta1.APIService, error) {
	svci, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &apiregistrationv1beta1.APIService{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Role manifest: %v", err)
	}
	svc, ok := svci.(*apiregistrationv1beta1.APIService)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *apiregistrationv1beta1.APIService, got %T", svci)
	}
	return svc, nil
}

// roleFromBytes unmarshals the manifest data into *rbacv1.role object.
func roleFromBytes(manifest []byte) (*rbacv1.Role, error) {
	ri, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &rbacv1.Role{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Role manifest: %v", err)
	}
	r, ok := ri.(*rbacv1.Role)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *rbacv1.Role, got %T", ri)
	}
	return r, nil
}

// roleBindingFromBytes unmarshals the manifest data into *rbacv1.roleBinding object.
func roleBindingFromBytes(manifest []byte) (*rbacv1.RoleBinding, error) {
	rbi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &rbacv1.RoleBinding{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode RoleBinding manifest: %v", err)
	}
	rb, ok := rbi.(*rbacv1.RoleBinding)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *rbacv1.RoleBinding, got %T", rbi)
	}
	return rb, nil
}

// clusterRoleFromBytes unmarshals the manifest data into *rbacv1.clusterRole object.
func clusterRoleFromBytes(manifest []byte) (*rbacv1.ClusterRole, error) {
	cri, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &rbacv1.ClusterRole{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode ClusterRole manifest: %v", err)
	}
	cr, ok := cri.(*rbacv1.ClusterRole)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *rbacv1.ClusterRole, got %T", cri)
	}
	return cr, nil
}

// clusterRoleBindingFromBytes unmarshals the manifest data into *rbacv1.clusterRoleBinding object.
func clusterRoleBindingFromBytes(manifest []byte) (*rbacv1.ClusterRoleBinding, error) {
	crbi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &rbacv1.ClusterRoleBinding{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode ClusterRoleBinding manifest: %v", err)
	}
	crb, ok := crbi.(*rbacv1.ClusterRoleBinding)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *rbacv1.ClusterRoleBinding, got %T", crbi)
	}
	return crb, nil
}

// configMapFromBytes unmarshals the manifest data into *v1.ConfigMap object.
func configMapFromBytes(manifest []byte) (*v1.ConfigMap, error) {
	ci, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &v1.ConfigMap{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode ConfigMap manifest: %v", err)
	}
	cm, ok := ci.(*v1.ConfigMap)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *v1.ConfigMap, got %T", ci)
	}
	return cm, nil
}

// customResourceDefinitionFromBytes unmarshals the manifest data into *v1beta1ext.CustomResourceDefinition object.
func customResourceDefinitionFromBytes(manifest []byte) (*v1beta1ext.CustomResourceDefinition, error) {
	crdi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &v1beta1ext.CustomResourceDefinition{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode CustomResourceDefinition manifest: %v", err)
	}
	crd, ok := crdi.(*v1beta1ext.CustomResourceDefinition)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *v1beta1ext.CustomResourceDefinition, got %T", crd)
	}
	return crd, nil
}

// daemonSetFromBytes unmarshals the manifest data into *appsv1beta2.Daemonset object.
func daemonSetFromBytes(manifest []byte) (*appsv1beta2.DaemonSet, error) {
	di, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &appsv1beta2.DaemonSet{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode DaemonSet manifest: %v", err)
	}
	ds, ok := di.(*appsv1beta2.DaemonSet)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *appsv1beta2.DaemonSet, got %T", di)
	}
	return ds, nil
}

// deploymentFromBytes unmarshals the manifest data into *appsv1beta2.Deployment object.
func deploymentFromBytes(manifest []byte) (*appsv1beta2.Deployment, error) {
	di, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &appsv1beta2.Deployment{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Deployment manifest: %v", err)
	}
	d, ok := di.(*appsv1beta2.Deployment)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *extensions.Deployment, got %T", di)
	}
	return d, nil
}

// ingressFromBytes unmarshals the manifest data into *extensionsv1beta1.Ingress object.
func ingressFromBytes(manifest []byte) (*extensionsv1beta1.Ingress, error) {
	igi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &extensionsv1beta1.Ingress{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Ingress manifest: %v", err)
	}
	ig, ok := igi.(*extensionsv1beta1.Ingress)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *extensionsv1beta1.Ingress, got %T", igi)
	}
	return ig, nil
}

// jobFromBytes unmarshals the manifest data into *batchv1.Job object.
func jobFromBytes(manifest []byte) (*batchv1.Job, error) {
	ji, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &batchv1.Job{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Job manifest: %v", err)
	}
	j, ok := ji.(*batchv1.Job)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *batchv1.Job, got %T", ji)
	}

	var defaultActiveDeadlineSeconds int64 = 120

	if j.Spec.ActiveDeadlineSeconds == nil {
		j.Spec.ActiveDeadlineSeconds = &defaultActiveDeadlineSeconds
	}
	return j, nil
}

// networkPolicyFromBytes unmarshals the manifest data into *v1.Namespace object.
func networkPolicyFromBytes(manifest []byte) (*netv1.NetworkPolicy, error) {
	ni, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &netv1.NetworkPolicy{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode NetworkPolicy manifest: %v", err)
	}
	np, ok := ni.(*netv1.NetworkPolicy)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *netv1.NetworkPolicy, got %T", ni)
	}
	return np, nil
}

// podFromBytes unmarshals the manifest data into *v1.Pod object.
func podFromBytes(manifest []byte) (*v1.Pod, error) {
	pi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &v1.Pod{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Pod manifest: %v", err)
	}
	pod, ok := pi.(*v1.Pod)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *v1.Pod, got %T", pi)
	}
	return pod, nil
}

// podDisruptionBudgetFromBytes unmarshals the manifest data into *policyv1beta1.PodDisruptionBudget
// object.
func podDisruptionBudgetFromBytes(manifest []byte) (*policyv1beta1.PodDisruptionBudget, error) {
	pi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &policyv1beta1.PodDisruptionBudget{})
	if err != nil {
		return nil, fmt.Errorf("decode PodDisruptionBudget manifest: %v", err)
	}
	pdb, ok := pi.(*policyv1beta1.PodDisruptionBudget)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *policyv1beta1.PodDisruptionBudget, got %T", pi)
	}
	return pdb, nil
}

// podSecurityPolicyFromBytes unmarshals the manifest data into *extensionsv1beta1.PodSecurityPolicy
// object.
func podSecurityPolicyFromBytes(manifest []byte) (*extensionsv1beta1.PodSecurityPolicy, error) {
	pi, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &extensionsv1beta1.PodSecurityPolicy{})
	if err != nil {
		return nil, fmt.Errorf("decode PodSecurityPolicy manifest: %v", err)
	}
	psp, ok := pi.(*extensionsv1beta1.PodSecurityPolicy)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *extensionsv1beta1.PodSecurityPolicy, got %T", pi)
	}
	return psp, nil
}

// secretFromBytes unmarshals the manifest data into *v1.Secret object.
func secretFromBytes(manifest []byte) (*v1.Secret, error) {
	ci, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &v1.Secret{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode Secret manifest: %v", err)
	}
	cm, ok := ci.(*v1.Secret)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *v1.Secret, got %T", ci)
	}
	return cm, nil
}

// serviceFromBytes unmarshals the manifest data into *v1.Service object.
func serviceFromBytes(manifest []byte) (*v1.Service, error) {
	si, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &v1.Service{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode service manifest: %v", err)
	}
	s, ok := si.(*v1.Service)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *v1.service, got %T", si)
	}
	return s, nil
}

// serviceAccountFromBytes unmarshals the manifest data into *v1.ServiceAccount object.
func serviceAccountFromBytes(manifest []byte) (*v1.ServiceAccount, error) {
	sai, _, err := scheme.Codecs.UniversalDecoder().Decode(manifest, nil, &v1.ServiceAccount{})
	if err != nil {
		return nil, fmt.Errorf("unable to decode ServiceAccount manifest: %v", err)
	}
	sa, ok := sai.(*v1.ServiceAccount)
	if !ok {
		return nil, fmt.Errorf("expected manifest to decode into *v1.ServiceAccount, got %T", sai)
	}
	return sa, nil
}
