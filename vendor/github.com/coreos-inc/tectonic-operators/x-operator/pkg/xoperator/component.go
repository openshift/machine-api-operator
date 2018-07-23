package xoperator

import (
	"fmt"

	"github.com/golang/glog"
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
	"github.com/coreos-inc/tectonic-operators/lib/manifest/lastapplied"
	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/constants"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/xoperator/components"
)

// componentFromObject is a wrapper to convert Object to corresponding Component.
func (xo *xoperator) componentFromObject(obj metav1.Object) (types.Component, error) {
	switch obj := obj.(type) {
	case *apiregistrationv1beta1.APIService:
		return components.NewAPIServiceUpdater(xo.client, obj, xo.cache.apiServices), nil
	case *rbacv1.ClusterRole:
		return components.NewClusterRoleUpdater(xo.client, obj, xo.cache.clusterRoles), nil
	case *rbacv1.ClusterRoleBinding:
		return components.NewClusterRoleBindingUpdater(xo.client, obj, xo.cache.clusterRoleBindings), nil
	case *v1.ConfigMap:
		return components.NewConfigMapUpdater(xo.client, obj, xo.cache.configmaps), nil
	case *manifest.CustomResource:
		return components.NewCustomResourceUpdater(xo.client, obj), nil
	case *v1beta1ext.CustomResourceDefinition:
		return components.NewCustomResourceDefinitionUpdater(xo.client, obj, xo.cache.customResourceDefinitionKinds), nil
	case *appsv1beta2.DaemonSet:
		return components.NewDaemonSetUpdater(xo.client, obj, xo.cache.daemonsets), nil
	case *appsv1beta2.Deployment:
		return components.NewDeploymentUpdater(xo.client, obj, xo.cache.deployments), nil
	case *extensionsv1beta1.Ingress:
		return components.NewIngressUpdater(xo.client, obj, xo.cache.ingresss), nil
	case *netv1.NetworkPolicy:
		return components.NewNetworkPolicyUpdater(xo.client, obj, xo.cache.networkPolicys), nil
	case *rbacv1.Role:
		return components.NewRoleUpdater(xo.client, obj, xo.cache.roles), nil
	case *rbacv1.RoleBinding:
		return components.NewRoleBindingUpdater(xo.client, obj, xo.cache.roleBindings), nil
	case *v1.Secret:
		return components.NewSecretUpdater(xo.client, obj, xo.cache.secrets), nil
	case *v1.Service:
		return components.NewServiceUpdater(xo.client, obj, xo.cache.services), nil
	case *v1.ServiceAccount:
		return components.NewServiceAccountUpdater(xo.client, obj, xo.cache.serviceAccounts), nil
	}
	return nil, fmt.Errorf("Unable to convert object to component, unknown type: %T", obj)
}

// updateComponent updates individual component.
func (xo *xoperator) updateComponent(original, modified metav1.Object, strategy constants.UpgradeStrategy, behaviour constants.UpgradeBehaviour) error {
	var ocmp types.Component
	if original != nil {
		var err error
		ocmp, err = xo.componentFromObject(original)
		if err != nil {
			return fmt.Errorf("unable to convert old object to component: %v", err)
		}
	}
	ncmp, err := xo.componentFromObject(modified)
	if err != nil {
		return fmt.Errorf("unable to convert new object to component: %v", err)
	}
	componentName := manifest.ComponentName(modified)

	glog.Infof("Updating component %s", componentName)
	if _, err := xo.setTaskStatus(optypes.TaskStatus{Name: componentName, State: optypes.TaskStateRunning}); err != nil {
		return fmt.Errorf("error updating task status: %v", err)
	}

	err = nil
	switch behaviour {
	case constants.UpgradeBehaviourCreateOrUpgrade:
		err = ncmp.CreateOrUpgrade(ocmp, strategy)
	case constants.UpgradeBehaviourUpgradeIfExists:
		err = ncmp.UpgradeIfExists(ocmp, strategy)
	}
	if err != nil {
		if _, err := xo.setTaskStatus(optypes.TaskStatus{Name: componentName, State: optypes.TaskStateBackOff, Reason: err.Error()}); err != nil {
			return fmt.Errorf("error updating task status: %v", err)
		}
		return fmt.Errorf("Failed update of component: %s due to: %v", componentName, err)
	}

	if _, err := xo.setTaskStatus(optypes.TaskStatus{Name: componentName, State: optypes.TaskStateCompleted}); err != nil {
		return fmt.Errorf("error updating task status: %v", err)
	}
	glog.Infof("Finished update of component: %s", componentName)
	return nil
}

// runMigration runs the batch job as migration.
func (xo *xoperator) runMigration(mig *batchv1.Job) error {
	glog.Infof("running migration %s", mig.GetName())
	if _, err := xo.setTaskStatus(optypes.TaskStatus{Name: manifest.ComponentName(mig), State: optypes.TaskStateRunning}); err != nil {
		return fmt.Errorf("error updating task status: %v", err)
	}

	if err := components.RunJobMigration(xo.client, mig); err != nil {
		if _, err2 := xo.setTaskStatus(optypes.TaskStatus{Name: manifest.ComponentName(mig), State: optypes.TaskStateBackOff, Reason: err.Error()}); err2 != nil {
			return fmt.Errorf("error updating task status: %v", err2)
		}
		return fmt.Errorf("failed migration: %s due to: %v", mig.GetName(), err)
	}

	if _, err := xo.setTaskStatus(optypes.TaskStatus{Name: manifest.ComponentName(mig), State: optypes.TaskStateCompleted}); err != nil {
		return fmt.Errorf("error updating task status: %v", err)
	}

	glog.Infof("completed migration %s", mig.GetName())
	return nil
}

// getOriginalObject reads the original object.
func getOriginalObject(cdef *types.UpgradeDefinition, current, modified metav1.Object) (metav1.Object, error) {
	if current != nil {
		original, err := lastapplied.Get(current, true)
		if err == nil {
			return original, nil
		} else if err == lastapplied.ErrorLastAppliedAnnotationMissing {
			glog.Warningf("No last-applied annotations from the current manifest %s/%s, will use on-disk manifest", current.GetNamespace(), current.GetName())
		} else {
			return nil, fmt.Errorf("failed to read last-applied annotation: %v", err)
		}
	}

	// Now try to read out the on-disk manifest.
	if cdef == nil {
		glog.Infof("Install mode, no original object is needed, skipping.")
		return nil, nil
	}

	if obj, found := cdef.FindComponent(modified, modified.GetNamespace(), modified.GetName()); found {
		return obj, nil
	}
	glog.Infof("No original object found for %s/%s", modified.GetNamespace(), modified.GetName())

	return nil, nil
}

// getCurrentObject returns the current running object that has the same namespace/name/kind of the 'modified' component.
func getCurrentObject(client opclient.Interface, modified metav1.Object) (metav1.Object, error) {
	ns, name := modified.GetNamespace(), modified.GetName()
	switch modified := modified.(type) {
	case *apiregistrationv1beta1.APIService:
		return client.GetAPIService(name)
	case *rbacv1.ClusterRole:
		return client.GetClusterRole(name)
	case *rbacv1.ClusterRoleBinding:
		return client.GetClusterRoleBinding(name)
	case *v1.ConfigMap:
		return client.GetConfigMap(ns, name)
	case *manifest.CustomResource:
		gvk := modified.GroupVersionKind()
		return client.GetCustomResource(gvk.Group, gvk.Version, ns, gvk.Kind, name)
	case *v1beta1ext.CustomResourceDefinition:
		return client.GetCustomResourceDefinition(name)
	case *appsv1beta2.DaemonSet:
		return client.GetDaemonSet(ns, name)
	case *appsv1beta2.Deployment:
		return client.GetDeployment(ns, name)
	case *extensionsv1beta1.Ingress:
		return client.GetIngress(ns, name)
	case *netv1.NetworkPolicy:
		return client.GetNetworkPolicy(ns, name)
	case *rbacv1.Role:
		return client.GetRole(ns, name)
	case *rbacv1.RoleBinding:
		return client.GetRoleBinding(ns, name)
	case *v1.Secret:
		return client.GetSecret(ns, name)
	case *v1.Service:
		return client.GetService(ns, name)
	case *v1.ServiceAccount:
		return client.GetServiceAccount(ns, name)
	}
	return nil, fmt.Errorf("unrecognized type: %T", modified)
}

// getOriginalObjectFromLastApplied reads the original object from the 'last-applied' annotation.
func getOriginalObjectFromLastApplied(current metav1.Object) (metav1.Object, error) {
	if current == nil {
		glog.Infof("Install mode, no original object is needed, skipping.")
		return nil, nil
	}

	original, err := lastapplied.Get(current, true)
	if err != nil {
		if err == lastapplied.ErrorLastAppliedAnnotationMissing {
			return nil, fmt.Errorf("no last-applied annotations from the current manifest %s/%s", current.GetNamespace(), current.GetName())
		}
		return nil, fmt.Errorf("failed to read last-applied annotation: %v", err)
	}
	return original, nil
}

// setupLastAppliedAnnotation sets the 'last-applied' annotation in 'original'
// to be the same as 'modified' iff original and current both exist.
func setupLastAppliedAnnotation(original, current, modified metav1.Object) error {
	// In modified, it will always have the 'last-applied' that reflects itself.
	if err := lastapplied.Set(modified, modified); err != nil {
		glog.Errorf("Failed to set last-applied annotation for %s/%s: %v", modified.GetNamespace(), modified.GetName(), err)
		return err
	}

	if original != nil && current != nil {
		// Make 'original' has the same last-applied annotation as the 'current'
		// to avoid patch conflict.
		lastapplied.Copy(original, current)
		return nil
	}

	glog.Infof("No original or current manifest found, it's in install mode for manifest for %s/%s", modified.GetNamespace(), modified.GetName())
	return nil
}
