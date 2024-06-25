package operator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehash"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/metrics"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
	mapiwebhooks "github.com/openshift/machine-api-operator/pkg/webhooks"
)

const (
	checkStatusRequeuePeriod            = 5 * time.Second
	deploymentMinimumAvailabilityTime   = 3 * time.Minute
	machineAPITerminationHandler        = "machine-api-termination-handler"
	MachineWebhookPort                  = 8440
	MachineSetWebhookPort               = 8443
	machineExposeMetricsPort            = 8441
	machineSetExposeMetricsPort         = 8442
	machineHealthCheckExposeMetricsPort = 8444
	defaultMachineHealthPort            = 9440
	defaultMachineSetHealthPort         = 9441
	defaultMachineHealthCheckHealthPort = 9442
	kubeRBACConfigName                  = "config"
	certStoreName                       = "machine-api-controllers-tls"
	externalTrustBundleConfigMapName    = "mao-trusted-ca"
	hostKubeConfigPath                  = "/var/lib/kubelet/kubeconfig"
	hostKubePKIPath                     = "/var/lib/kubelet/pki"
	operatorStatusNoOpMessage           = "Cluster Machine API Operator is in NoOp mode"
	machineSetWebhookVolumeName         = "machineset-webhook-cert"
	machineWebhookVolumeName            = "machine-webhook-cert"
	kubernetesOSlabel                   = "kubernetes.io/os"
	kubernetesOSlabelLinux              = "linux"

	minimumWorkerReplicas = int32(2)
)

var (
	// daemonsetMaxUnavailable must be set to "10%" to conform with other
	// daemonsets.
	daemonsetMaxUnavailable = intstr.FromString("10%")

	commonPodTemplateAnnotations = map[string]string{
		"target.workload.openshift.io/management": `{"effect": "PreferredDuringScheduling"}`,
	}
)

func (optr *Operator) syncAll(config *OperatorConfig) (reconcile.Result, error) {
	if err := optr.statusProgressing(); err != nil {
		klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		return reconcile.Result{}, fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}

	if config.Controllers.Provider == clusterAPIControllerNoOp {
		klog.V(3).Info("Provider is NoOp, skipping synchronisation")
		if err := optr.statusAvailable(operatorStatusNoOpMessage); err != nil {
			klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
			return reconcile.Result{}, fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
		}
		return reconcile.Result{}, nil
	}

	errors := []error{}
	// Sync webhook configuration
	if err := optr.syncWebhookConfiguration(config); err != nil {
		errors = append(errors, fmt.Errorf("error syncing machine API webhook configurations: %w", err))
	}

	// can we add features to the config and then pass them when creating the deployment in the below function
	if err := optr.syncClusterAPIController(config); err != nil {
		errors = append(errors, fmt.Errorf("error syncing machine-api-controller: %w", err))
	}

	// Sync Termination Handler DaemonSet if supported
	if config.Controllers.TerminationHandler != clusterAPIControllerNoOp {
		if err := optr.syncTerminationHandler(config); err != nil {
			errors = append(errors, fmt.Errorf("error syncing termination handler: %w", err))
		}
	}

	if len(errors) > 0 {
		err := utilerrors.NewAggregate(errors)
		if err := optr.statusDegraded(err.Error()); err != nil {
			// Just log the error here.  We still want to
			// return the outer error.
			klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		}
		klog.Errorf("Error syncing machine controller components: %v", err)
		return reconcile.Result{}, err
	}

	result, err := optr.checkRolloutStatus(config)
	if err != nil {
		if err := optr.statusDegraded(err.Error()); err != nil {
			// Just log the error here.  We still want to
			// return the outer error.
			klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		}
		klog.Errorf("Error waiting for resource to sync: %v", err)
		return reconcile.Result{}, err
	}
	if result.Requeue || result.RequeueAfter > 0 {
		// The deployment is not yet rolled out, do not set the status to available yet
		return result, nil
	}

	klog.V(3).Info("Synced up all machine API configurations")

	initializing, err := optr.isInitializing()
	if err != nil {
		if err := optr.statusDegraded(err.Error()); err != nil {
			// Just log the error here.  We still want to
			// return the outer error.
			klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		}
		klog.Errorf("Error determining state of operator: %v", err)
		return reconcile.Result{}, err
	}

	if initializing {
		if err := optr.checkMinimumWorkerMachines(); err != nil {
			if err := optr.statusDegraded(err.Error()); err != nil {
				// Just log the error here.  We still want to
				// return the outer error.
				klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
			}

			klog.Errorf("Cluster is initializing and minimum worker Machine requirements are not met: %v", err)
			// Check again every requeue period until the Machines come up.
			// If we error we will eventually hit the maximum errors and drop from the queue.
			return reconcile.Result{RequeueAfter: checkStatusRequeuePeriod}, nil
		}

		klog.V(3).Info("All cluster Machines are now Running")
	}

	message := fmt.Sprintf("Cluster Machine API Operator is available at %s", optr.printOperandVersions())
	if err := optr.statusAvailable(message); err != nil {
		klog.Errorf("Error syncing ClusterOperatorStatus: %v", err)
		return reconcile.Result{}, fmt.Errorf("error syncing ClusterOperatorStatus: %v", err)
	}
	return reconcile.Result{}, nil
}

func (optr *Operator) checkRolloutStatus(config *OperatorConfig) (reconcile.Result, error) {
	// Check for machine-controllers deployment
	result, err := optr.checkDeploymentRolloutStatus(newDeployment(config, nil))
	if err != nil {
		return reconcile.Result{}, err
	}
	if result.Requeue || result.RequeueAfter > 0 {
		return result, nil
	}

	if config.Controllers.TerminationHandler != clusterAPIControllerNoOp {
		// Check for termination handler
		result, err := optr.checkDaemonSetRolloutStatus(newTerminationDaemonSet(config))
		if err != nil {
			return reconcile.Result{}, err
		}
		if result.Requeue || result.RequeueAfter > 0 {
			return result, nil
		}
	}

	return reconcile.Result{}, nil
}

func (optr *Operator) syncClusterAPIController(config *OperatorConfig) error {
	controllersDeployment := newDeployment(config, config.Features)

	// we watch some resources so that our deployment will redeploy without explicitly and carefully ordered resource creation
	inputHashes, err := resourcehash.MultipleObjectHashStringMapForObjectReferences(
		context.TODO(),
		optr.kubeClient,
		resourcehash.NewObjectRef().ForConfigMap().InNamespace(config.TargetNamespace).Named(externalTrustBundleConfigMapName),
	)
	if err != nil {
		return fmt.Errorf("invalid dependency reference: %q", err)
	}
	ensureDependecyAnnotations(inputHashes, controllersDeployment)

	expectedGeneration := resourcemerge.ExpectedDeploymentGeneration(controllersDeployment, optr.generations)
	d, updated, err := resourceapply.ApplyDeployment(context.TODO(), optr.kubeClient.AppsV1(),
		events.NewLoggingEventRecorder(optr.name), controllersDeployment, expectedGeneration)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetDeploymentGeneration(&optr.generations, d)
	}

	return nil
}

func (optr *Operator) syncTerminationHandler(config *OperatorConfig) error {
	terminationDaemonSet := newTerminationDaemonSet(config)
	expectedGeneration := resourcemerge.ExpectedDaemonSetGeneration(terminationDaemonSet, optr.generations)
	ds, updated, err := resourceapply.ApplyDaemonSet(context.TODO(), optr.kubeClient.AppsV1(),
		events.NewLoggingEventRecorder(optr.name), terminationDaemonSet, expectedGeneration)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetDaemonSetGeneration(&optr.generations, ds)
	}
	return nil
}

func (optr *Operator) syncWebhookConfiguration(config *OperatorConfig) error {
	if err := optr.syncMachineValidatingWebhook(); err != nil {
		return err
	}
	if err := optr.syncMachineMutatingWebhook(); err != nil {
		return err
	}
	if config.PlatformType == v1.BareMetalPlatformType {
		if err := optr.syncMetal3RemediationValidatingWebhook(); err != nil {
			return err
		}
		if err := optr.syncMetal3RemediationMutatingWebhook(); err != nil {
			return err
		}
	}
	return nil
}

func (optr *Operator) syncMachineValidatingWebhook() error {
	validatingWebhook, updated, err := resourceapply.ApplyValidatingWebhookConfigurationImproved(context.TODO(), optr.kubeClient.AdmissionregistrationV1(),
		events.NewLoggingEventRecorder(optr.name),
		mapiwebhooks.NewMachineValidatingWebhookConfiguration(),
		optr.cache)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetValidatingWebhooksConfigurationGeneration(&optr.generations, validatingWebhook)
	}

	return nil
}

func (optr *Operator) syncMachineMutatingWebhook() error {
	mutatingWebhook, updated, err := resourceapply.ApplyMutatingWebhookConfigurationImproved(context.TODO(), optr.kubeClient.AdmissionregistrationV1(),
		events.NewLoggingEventRecorder(optr.name),
		mapiwebhooks.NewMachineMutatingWebhookConfiguration(),
		optr.cache)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetMutatingWebhooksConfigurationGeneration(&optr.generations, mutatingWebhook)
	}

	return nil
}

// Metal3Remediation(Templates) were backported from metal3, their CRDs and the
// actual webhook implementation can be found in cluster-api-provider-baremetal
func (optr *Operator) syncMetal3RemediationValidatingWebhook() error {
	validatingWebhook, updated, err := resourceapply.ApplyValidatingWebhookConfigurationImproved(context.TODO(), optr.kubeClient.AdmissionregistrationV1(),
		events.NewLoggingEventRecorder(optr.name),
		mapiwebhooks.NewMetal3RemediationValidatingWebhookConfiguration(),
		optr.cache)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetValidatingWebhooksConfigurationGeneration(&optr.generations, validatingWebhook)
	}

	return nil
}

// Metal3Remediation(Templates) were backported from metal3, their CRDs and the
// actual webhook implementation can be found in cluster-api-provider-baremetal
func (optr *Operator) syncMetal3RemediationMutatingWebhook() error {
	mutatingWebhook, updated, err := resourceapply.ApplyMutatingWebhookConfigurationImproved(context.TODO(), optr.kubeClient.AdmissionregistrationV1(),
		events.NewLoggingEventRecorder(optr.name),
		mapiwebhooks.NewMetal3RemediationMutatingWebhookConfiguration(),
		optr.cache)
	if err != nil {
		return err
	}
	if updated {
		resourcemerge.SetMutatingWebhooksConfigurationGeneration(&optr.generations, mutatingWebhook)
	}

	return nil
}

func (optr *Operator) checkDeploymentRolloutStatus(resource *appsv1.Deployment) (reconcile.Result, error) {
	d, err := optr.kubeClient.AppsV1().Deployments(resource.Namespace).Get(context.Background(), resource.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return reconcile.Result{}, fmt.Errorf("deployment %s is not found", resource.Name)
	}
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("getting Deployment %s during rollout: %v", resource.Name, err)
	}

	if d.DeletionTimestamp != nil {
		return reconcile.Result{}, fmt.Errorf("deployment %s is being deleted", resource.Name)
	}

	if d.Generation > d.Status.ObservedGeneration || d.Status.UpdatedReplicas != d.Status.Replicas || d.Status.UnavailableReplicas > 0 {
		klog.V(3).Infof("deployment %s is not ready. status: (replicas: %d, updated: %d, ready: %d, unavailable: %d)", d.Name, d.Status.Replicas, d.Status.UpdatedReplicas, d.Status.ReadyReplicas, d.Status.UnavailableReplicas)
		return reconcile.Result{Requeue: true, RequeueAfter: checkStatusRequeuePeriod}, nil
	}

	c := conditions.GetDeploymentCondition(d, appsv1.DeploymentAvailable)
	if c == nil {
		klog.V(3).Infof("deployment %s is not reporting available yet", resource.Name)
		return reconcile.Result{Requeue: true, RequeueAfter: checkStatusRequeuePeriod}, nil
	}

	if c.Status == corev1.ConditionFalse {
		klog.V(3).Infof("deployment %s is reporting available=false", resource.Name)
		return reconcile.Result{Requeue: true, RequeueAfter: checkStatusRequeuePeriod}, nil
	}

	if c.LastTransitionTime.Time.Add(deploymentMinimumAvailabilityTime).After(time.Now()) {
		klog.V(3).Infof("deployment %s has been available for less than %s", resource.Name, deploymentMinimumAvailabilityTime)
		// Requeue at the deploymentMinimumAvailabilityTime mark so we don't spam retries
		nextCheck := time.Until(c.LastTransitionTime.Time.Add(deploymentMinimumAvailabilityTime))
		return reconcile.Result{Requeue: true, RequeueAfter: nextCheck}, nil
	}

	return reconcile.Result{}, nil
}

func (optr *Operator) checkDaemonSetRolloutStatus(resource *appsv1.DaemonSet) (reconcile.Result, error) {
	d, err := optr.kubeClient.AppsV1().DaemonSets(resource.Namespace).Get(context.Background(), resource.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return reconcile.Result{}, fmt.Errorf("daemonset %s is not found", resource.Name)
	}
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("getting DaemonSet %s during rollout: %v", resource.Name, err)
	}

	if d.DeletionTimestamp != nil {
		return reconcile.Result{}, fmt.Errorf("daemonset %s is being deleted", resource.Name)
	}

	if d.Generation > d.Status.ObservedGeneration || d.Status.UpdatedNumberScheduled != d.Status.DesiredNumberScheduled || d.Status.NumberUnavailable > 0 {
		klog.V(3).Infof("daemonset %s is not ready. status: (desired: %d, updated: %d, available: %d, unavailable: %d)", d.Name, d.Status.DesiredNumberScheduled, d.Status.UpdatedNumberScheduled, d.Status.NumberAvailable, d.Status.NumberUnavailable)
		return reconcile.Result{Requeue: true, RequeueAfter: checkStatusRequeuePeriod}, nil
	}

	return reconcile.Result{}, nil
}

// checkMinimumWorkerMachines looks at the worker Machines in the cluster and checks if they are
// running. If fewer than 2 worker Machines are Running and the expected number is higher than 1, it
// will return an error. This is used during initialization of the cluster to prevent the operator
// from being Available until the minimum required number of worker Machines have started working
// correctly.
func (optr *Operator) checkMinimumWorkerMachines() error {
	machineSets, err := optr.machineClient.MachineV1beta1().MachineSets(optr.namespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("could not list MachineSets: %w", err)
	}

	expectedReplicas := int32(0)
	nonRunningMachines := []string{}
	for _, machineSet := range machineSets.Items {
		replicas, nonRunningMachineSetMachines, err := optr.checkRunningMachineSetMachines(machineSet)
		if err != nil {
			return fmt.Errorf("could not determine running Machines in MachineSet %q: %w", machineSet.GetName(), err)
		}
		expectedReplicas += replicas
		nonRunningMachines = append(nonRunningMachines, nonRunningMachineSetMachines...)
	}

	// If any MachineSet doesn't have the correct number of replicas, we error before this point.
	// So the running replicas should be (total replicas) - (non-running replicas).
	// Only error if the number of expected replicas is higher than 1. This is because masters are
	// epxected to be schedulable when there are fewer than 2 workers expected. A better test would
	// be to check if masters are schedulable but this favors the worker count over querying the API
	// Kubernetes API again.
	runningReplicas := expectedReplicas - int32(len(nonRunningMachines))
	if expectedReplicas > 1 && runningReplicas < minimumWorkerReplicas {
		return fmt.Errorf("minimum worker replica count (%d) not yet met: current running replicas %d, waiting for %v", minimumWorkerReplicas, runningReplicas, nonRunningMachines)
	}

	return nil
}

func (optr *Operator) checkRunningMachineSetMachines(machineSet machinev1beta1.MachineSet) (int32, []string, error) {
	replicas := ptr.Deref(machineSet.Spec.Replicas, 0)

	selector, err := metav1.LabelSelectorAsSelector(&machineSet.Spec.Selector)
	if err != nil {
		return 0, []string{}, fmt.Errorf("could not convert MachineSet label selector to selector: %w", err)
	}

	machines, err := optr.machineClient.MachineV1beta1().Machines(optr.namespace).List(context.Background(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	if err != nil {
		return 0, []string{}, fmt.Errorf("could not get a list of machines: %w", err)
	}

	if int32(len(machines.Items)) != replicas {
		return 0, []string{}, fmt.Errorf("replicas not satisfied for MachineSet: expected %d replicas, got %d current replicas", replicas, len(machines.Items))
	}

	nonRunningMachines := []string{}
	for _, machine := range machines.Items {
		phase := ptr.Deref(machine.Status.Phase, "")
		if phase != "Running" {
			nonRunningMachines = append(nonRunningMachines, machine.GetName())
		}
	}

	return replicas, nonRunningMachines, nil
}

func newDeployment(config *OperatorConfig, features map[string]bool) *appsv1.Deployment {
	replicas := int32(1)
	template := newPodTemplateSpec(config, features)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-api-controllers",
			Namespace: config.TargetNamespace,
			Annotations: map[string]string{
				maoOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "controller",
			},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"api":     "clusterapi",
					"k8s-app": "controller",
				},
			},
			Template: *template,
		},
	}
}

// List of the volumes needed by newKubeProxyContainer
func newRBACConfigVolumes() []corev1.Volume {
	var readOnly int32 = 420
	return []corev1.Volume{
		{
			Name: kubeRBACConfigName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "kube-rbac-proxy",
					},
					DefaultMode: ptr.To[int32](readOnly),
				},
			},
		},
		{
			Name: certStoreName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  certStoreName,
					DefaultMode: ptr.To[int32](readOnly),
				},
			},
		},
		{
			Name: "trusted-ca",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					Items: []corev1.KeyToPath{{Key: "ca-bundle.crt", Path: "tls-ca-bundle.pem"}},
					LocalObjectReference: corev1.LocalObjectReference{
						Name: externalTrustBundleConfigMapName,
					},
					Optional: ptr.To[bool](true),
				},
			},
		},
	}
}

func newPodTemplateSpec(config *OperatorConfig, features map[string]bool) *corev1.PodTemplateSpec {
	containers := newContainers(config, features)
	withMHCProxy := config.Controllers.MachineHealthCheck != ""
	proxyContainers := newKubeProxyContainers(config.Controllers.KubeRBACProxy, withMHCProxy)
	tolerations := []corev1.Toleration{
		{
			Key:    "node-role.kubernetes.io/master",
			Effect: corev1.TaintEffectNoSchedule,
		},
		{
			Key:      "CriticalAddonsOnly",
			Operator: corev1.TolerationOpExists,
		},
		{
			Key:               "node.kubernetes.io/not-ready",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: ptr.To[int64](120),
		},
		{
			Key:               "node.kubernetes.io/unreachable",
			Effect:            corev1.TaintEffectNoExecute,
			Operator:          corev1.TolerationOpExists,
			TolerationSeconds: ptr.To[int64](120),
		},
	}

	var readOnly int32 = 420
	volumes := []corev1.Volume{
		{
			Name: machineSetWebhookVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					// keep this aligned with service.beta.openshift.io/serving-cert-secret-name annotation on its services
					SecretName:  "machine-api-operator-webhook-cert",
					DefaultMode: ptr.To[int32](readOnly),
					Items: []corev1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "tls.crt",
						},
						{
							Key:  "tls.key",
							Path: "tls.key",
						},
					},
				},
			},
		},
		{
			Name: machineWebhookVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					// keep this aligned with service.beta.openshift.io/serving-cert-secret-name annotation on its services
					SecretName:  "machine-api-operator-machine-webhook-cert",
					DefaultMode: ptr.To[int32](readOnly),
					Items: []corev1.KeyToPath{
						{
							Key:  "tls.crt",
							Path: "tls.crt",
						},
						{
							Key:  "tls.key",
							Path: "tls.key",
						},
					},
				},
			},
		},
		{
			Name: "bound-sa-token",
			VolumeSource: corev1.VolumeSource{
				Projected: &corev1.ProjectedVolumeSource{
					Sources: []corev1.VolumeProjection{
						{
							ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
								Audience: "openshift",
								Path:     "token",
							},
						},
					},
				},
			},
		},
	}
	volumes = append(volumes, newRBACConfigVolumes()...)

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: commonPodTemplateAnnotations,
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "controller",
			},
		},
		Spec: corev1.PodSpec{
			Containers:         append(containers, proxyContainers...),
			PriorityClassName:  "system-node-critical",
			NodeSelector:       map[string]string{"node-role.kubernetes.io/master": ""},
			ServiceAccountName: "machine-api-controllers",
			Tolerations:        tolerations,
			Volumes:            volumes,
		},
	}
}

func getProxyArgs(config *OperatorConfig) []corev1.EnvVar {
	var envVars []corev1.EnvVar

	if config.Proxy == nil {
		return envVars
	}
	if config.Proxy.Status.HTTPProxy != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "HTTP_PROXY",
			Value: config.Proxy.Spec.HTTPProxy,
		})
	}
	if config.Proxy.Status.HTTPSProxy != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "HTTPS_PROXY",
			Value: config.Proxy.Spec.HTTPSProxy,
		})
	}
	if config.Proxy.Status.NoProxy != "" {
		envVars = append(envVars, corev1.EnvVar{
			Name:  "NO_PROXY",
			Value: config.Proxy.Status.NoProxy,
		})
	}
	return envVars
}

// buildFeatureGatesString builds a string with the format: --feature-gates=<name>=<bool>,<name>=<bool>...
func buildFeatureGatesString(featureGates map[string]bool) string {
	var parts []string
	for name, enabled := range featureGates {
		part := fmt.Sprintf("%s=%t", name, enabled)
		parts = append(parts, part)
	}
	return "--feature-gates=" + strings.Join(parts, ",")
}

func newContainers(config *OperatorConfig, features map[string]bool) []corev1.Container {
	resources := corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceMemory: resource.MustParse("20Mi"),
			corev1.ResourceCPU:    resource.MustParse("10m"),
		},
	}
	args := []string{
		"--logtostderr=true",
		"--v=3",
		"--leader-elect=true",
		"--leader-elect-lease-duration=120s",
		fmt.Sprintf("--namespace=%s", config.TargetNamespace),
	}

	machineControllerArgs := append([]string{}, args...)
	switch config.PlatformType {
	case v1.AzurePlatformType:
		machineControllerArgs = append(machineControllerArgs, "--max-concurrent-reconciles=10")
	}

	// Use the map of features to create a --feature-gates=<name>=<bool>,<name>=<bool>... arg
	machineControllerArgs = append(machineControllerArgs, buildFeatureGatesString(features))

	proxyEnvArgs := getProxyArgs(config)

	containers := []corev1.Container{
		{
			Name:      "machineset-controller",
			Image:     config.Controllers.MachineSet,
			Command:   []string{"/machineset-controller"},
			Args:      args,
			Resources: resources,
			Env:       proxyEnvArgs,
			Ports: []corev1.ContainerPort{
				{
					Name:          "webhook-server",
					ContainerPort: MachineSetWebhookPort,
				},
				{
					Name:          "healthz",
					ContainerPort: defaultMachineSetHealthPort,
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.Parse("healthz"),
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.Parse("healthz"),
					},
				},
			},
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/etc/machine-api-operator/tls",
					Name:      machineSetWebhookVolumeName,
					ReadOnly:  true,
				},
			},
		},
		{
			Name:      "machine-controller",
			Image:     config.Controllers.Provider,
			Command:   []string{"/machine-controller-manager"},
			Args:      machineControllerArgs,
			Resources: resources,
			Env: append(proxyEnvArgs, corev1.EnvVar{
				Name: "NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			}, corev1.EnvVar{
				Name:  "RELEASE_VERSION",
				Value: os.Getenv("RELEASE_VERSION"),
			}),
			Ports: []corev1.ContainerPort{
				{
					Name:          "machine-webhook",
					ContainerPort: MachineWebhookPort,
				},
				{
					Name:          "healthz",
					ContainerPort: defaultMachineHealthPort,
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.Parse("healthz"),
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.Parse("healthz"),
					},
				},
			},
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			VolumeMounts: []corev1.VolumeMount{
				{
					MountPath: "/etc/pki/ca-trust/extracted/pem",
					Name:      "trusted-ca",
					ReadOnly:  true,
				},
				{
					MountPath: "/var/run/secrets/openshift/serviceaccount",
					Name:      "bound-sa-token",
					ReadOnly:  true,
				},
				{
					MountPath: "/etc/machine-api-operator/tls",
					Name:      machineWebhookVolumeName,
					ReadOnly:  true,
				},
			},
		},
		{
			Name:                     "nodelink-controller",
			Image:                    config.Controllers.NodeLink,
			Command:                  []string{"/nodelink-controller"},
			Args:                     args,
			Env:                      proxyEnvArgs,
			Resources:                resources,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		},
	}
	if config.Controllers.MachineHealthCheck != "" {
		containers = append(containers, corev1.Container{
			Name:      "machine-healthcheck-controller",
			Image:     config.Controllers.MachineHealthCheck,
			Command:   []string{"/machine-healthcheck"},
			Args:      args,
			Env:       proxyEnvArgs,
			Resources: resources,
			Ports: []corev1.ContainerPort{
				{
					Name:          "healthz",
					ContainerPort: defaultMachineHealthCheckHealthPort,
				},
			},
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/healthz",
						Port: intstr.Parse("healthz"),
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{
						Path: "/readyz",
						Port: intstr.Parse("healthz"),
					},
				},
			},
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		})
	}
	return containers
}

func newKubeProxyContainers(image string, withMHCProxy bool) []corev1.Container {
	proxyContainers := []corev1.Container{
		newKubeProxyContainer(image, "machineset-mtrc", metrics.DefaultMachineSetMetricsAddress, machineSetExposeMetricsPort),
		newKubeProxyContainer(image, "machine-mtrc", metrics.DefaultMachineMetricsAddress, machineExposeMetricsPort),
	}
	if withMHCProxy {
		proxyContainers = append(proxyContainers,
			newKubeProxyContainer(image, "mhc-mtrc", metrics.DefaultHealthCheckMetricsAddress, machineHealthCheckExposeMetricsPort),
		)
	}
	return proxyContainers
}

func newKubeProxyContainer(image, portName, upstreamPort string, exposePort int32) corev1.Container {
	configMountPath := "/etc/kube-rbac-proxy"
	tlsCertMountPath := "/etc/tls/private"
	resources := corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceMemory: resource.MustParse("20Mi"),
			corev1.ResourceCPU:    resource.MustParse("10m"),
		},
	}
	args := []string{
		fmt.Sprintf("--secure-listen-address=0.0.0.0:%d", exposePort),
		fmt.Sprintf("--upstream=http://localhost%s", upstreamPort),
		fmt.Sprintf("--config-file=%s/config-file.yaml", configMountPath),
		fmt.Sprintf("--tls-cert-file=%s/tls.crt", tlsCertMountPath),
		fmt.Sprintf("--tls-private-key-file=%s/tls.key", tlsCertMountPath),
		"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
		"--logtostderr=true",
		"--v=3",
	}
	ports := []corev1.ContainerPort{{
		Name:          portName,
		ContainerPort: exposePort,
	}}

	return corev1.Container{
		Name:                     fmt.Sprintf("kube-rbac-proxy-%s", portName),
		Image:                    image,
		Args:                     args,
		Resources:                resources,
		Ports:                    ports,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      kubeRBACConfigName,
				MountPath: configMountPath,
			},
			{
				Name:      certStoreName,
				MountPath: tlsCertMountPath,
			}},
	}
}

func newTerminationDaemonSet(config *OperatorConfig) *appsv1.DaemonSet {
	template := newTerminationPodTemplateSpec(config)

	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineAPITerminationHandler,
			Namespace: config.TargetNamespace,
			Annotations: map[string]string{
				maoOwnedAnnotation: "",
			},
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "termination-handler",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				RollingUpdate: &appsv1.RollingUpdateDaemonSet{
					MaxUnavailable: &daemonsetMaxUnavailable,
				},
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"api":     "clusterapi",
					"k8s-app": "termination-handler",
				},
			},
			Template: *template,
		},
	}
}

func newTerminationPodTemplateSpec(config *OperatorConfig) *corev1.PodTemplateSpec {
	containers := newTerminationContainers(config)

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: commonPodTemplateAnnotations,
			Labels: map[string]string{
				"api":     "clusterapi",
				"k8s-app": "termination-handler",
			},
		},
		Spec: corev1.PodSpec{
			Containers:        containers,
			PriorityClassName: "system-node-critical",
			NodeSelector: map[string]string{
				machinecontroller.MachineInterruptibleInstanceLabelName: "",
				kubernetesOSlabel: kubernetesOSlabelLinux,
			},
			ServiceAccountName:           machineAPITerminationHandler,
			AutomountServiceAccountToken: ptr.To[bool](false),
			HostNetwork:                  true,
			Volumes: []corev1.Volume{
				{
					Name: "kubeconfig",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: hostKubeConfigPath,
						},
					},
				},
				{
					Name: "pki",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: hostKubePKIPath,
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				{
					Operator: corev1.TolerationOpExists,
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}
}

func newTerminationContainers(config *OperatorConfig) []corev1.Container {
	resources := corev1.ResourceRequirements{
		Requests: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceMemory: resource.MustParse("20Mi"),
			corev1.ResourceCPU:    resource.MustParse("10m"),
		},
	}
	terminationArgs := []string{
		"--logtostderr=true",
		"--v=3",
		"--node-name=$(NODE_NAME)",
		fmt.Sprintf("--namespace=%s", config.TargetNamespace),
		"--poll-interval-seconds=5",
	}

	proxyEnvArgs := getProxyArgs(config)

	return []corev1.Container{
		{
			Name:      "termination-handler",
			Image:     config.Controllers.TerminationHandler,
			Command:   []string{"/termination-handler"},
			Args:      terminationArgs,
			Resources: resources,
			Env: append(proxyEnvArgs, corev1.EnvVar{
				Name:  "KUBECONFIG",
				Value: hostKubeConfigPath,
			}, corev1.EnvVar{
				Name: "NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "spec.nodeName",
				},
				},
			}),

			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "kubeconfig",
					MountPath: hostKubeConfigPath,
					ReadOnly:  true,
				},
				{
					Name:      "pki",
					MountPath: hostKubePKIPath,
					ReadOnly:  true,
				},
			},
		},
	}
}

// ensureDependecyAnnotations uses inputHash map of external dependencies to force new generation of the deployment
// triggering the Kubernetes rollout as defined when the inputHash changes by adding it annotation to the deployment object.
func ensureDependecyAnnotations(inputHashes map[string]string, deployment *appsv1.Deployment) {
	for k, v := range inputHashes {
		annotationKey := fmt.Sprintf("operator.openshift.io/dep-%s", k)
		if deployment.Annotations == nil {
			deployment.Annotations = map[string]string{}
		}
		if deployment.Spec.Template.Annotations == nil {
			deployment.Spec.Template.Annotations = map[string]string{}
		}
		deployment.Annotations[annotationKey] = v
		deployment.Spec.Template.Annotations[annotationKey] = v
	}
}
