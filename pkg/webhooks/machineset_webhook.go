package webhooks

import (
	"context"
	"fmt"
	"reflect"

	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/scheme"
)

// machineSetValidatorHandler validates MachineSet API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type machineSetValidatorHandler struct {
	*admissionHandler
}

// machineSetDefaulterHandler defaults MachineSet API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type machineSetDefaulterHandler struct {
	*admissionHandler
}

// NewMachineSetValidator returns a new machineSetValidatorHandler.
func NewMachineSetValidator(client client.Client, featureGate featuregate.MutableFeatureGate) (*admission.Webhook, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	dns, err := getDNS()
	if err != nil {
		return nil, err
	}

	return createMachineSetValidator(infra, client, dns, featureGate), nil
}

func createMachineSetValidator(infra *osconfigv1.Infrastructure, client client.Client, dns *osconfigv1.DNS, featureGate featuregate.MutableFeatureGate) *admission.Webhook {
	admissionConfig := &admissionConfig{
		dnsDisconnected: dns.Spec.PublicZone == nil,
		clusterID:       infra.Status.InfrastructureName,
		client:          client,
		featureGates:    featureGate,
	}

	return admission.WithCustomValidator(scheme.Scheme, &machinev1beta1.MachineSet{}, &machineSetValidatorHandler{
		admissionHandler: &admissionHandler{
			admissionConfig:   admissionConfig,
			webhookOperations: getMachineValidatorOperation(infra.Status.PlatformStatus.Type),
		},
	})
}

// NewMachineSetDefaulter returns a new machineSetDefaulterHandler.
func NewMachineSetDefaulter() (*admission.Webhook, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return createMachineSetDefaulter(infra.Status.PlatformStatus, infra.Status.InfrastructureName), nil
}

func createMachineSetDefaulter(platformStatus *osconfigv1.PlatformStatus, clusterID string) *admission.Webhook {
	return admission.WithCustomDefaulter(scheme.Scheme, &machinev1beta1.MachineSet{}, &machineSetDefaulterHandler{
		admissionHandler: &admissionHandler{
			admissionConfig:   &admissionConfig{clusterID: clusterID},
			webhookOperations: getMachineDefaulterOperation(platformStatus),
		},
	})
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetValidatorHandler) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	warnings := admission.Warnings{}

	ms, ok := obj.(*machinev1beta1.MachineSet)
	if !ok {
		return warnings, apierrors.NewBadRequest(fmt.Sprintf("expected a MachineSet but got a %T", obj))
	}

	klog.V(3).Infof("Validate webhook called for MachineSet: %s", ms.GetName())

	ok, warnings, errs := h.validateMachineSet(ms, nil)
	if !ok {
		return warnings, errs.ToAggregate()
	}

	return warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetValidatorHandler) ValidateUpdate(ctx context.Context, oldObj, obj runtime.Object) (admission.Warnings, error) {
	warnings := admission.Warnings{}

	ms, ok := obj.(*machinev1beta1.MachineSet)
	if !ok {
		return warnings, apierrors.NewBadRequest(fmt.Sprintf("expected a MachineSet but got a %T", obj))
	}

	oldMS, ok := oldObj.(*machinev1beta1.MachineSet)
	if !ok {
		return warnings, apierrors.NewBadRequest(fmt.Sprintf("expected a MachineSet but got a %T", oldObj))
	}

	klog.V(3).Infof("Validate webhook called for MachineSet: %s", ms.GetName())

	ok, warnings, errs := h.validateMachineSet(ms, oldMS)
	if !ok {
		return warnings, errs.ToAggregate()
	}

	return warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetValidatorHandler) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	warnings := admission.Warnings{}

	ms, ok := obj.(*machinev1beta1.MachineSet)
	if !ok {
		return warnings, apierrors.NewBadRequest(fmt.Sprintf("expected a MachineSet but got a %T", obj))
	}

	klog.V(3).Infof("Validate webhook called for MachineSet: %s", ms.GetName())

	ok, warnings, errs := h.validateMachineSet(ms, nil)
	if !ok {
		return warnings, errs.ToAggregate()
	}

	return warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetDefaulterHandler) Default(ctx context.Context, obj runtime.Object) error {
	ms, ok := obj.(*machinev1beta1.MachineSet)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a MachineSet but got a %T", obj))
	}

	klog.V(3).Infof("Mutate webhook called for MachineSet: %s", ms.GetName())

	ok, _, errs := h.defaultMachineSet(ms)
	if !ok {
		return errs.ToAggregate()
	}

	return nil
}

func (h *machineSetValidatorHandler) validateMachineSet(ms, oldMS *machinev1beta1.MachineSet) (bool, []string, field.ErrorList) {
	errs := validateMachineSetSpec(ms, oldMS)

	// Create a Machine from the MachineSet and validate the Machine template
	m := &machinev1beta1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ms.GetNamespace(),
		},
		Spec: ms.Spec.Template.Spec,
	}
	ok, warnings, opsErrs := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		errs = append(errs, opsErrs...)
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

func (h *machineSetDefaulterHandler) defaultMachineSet(ms *machinev1beta1.MachineSet) (bool, []string, field.ErrorList) {
	// Create a Machine from the MachineSet and default the Machine template
	m := &machinev1beta1.Machine{Spec: ms.Spec.Template.Spec}
	ok, warnings, errs := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		return false, warnings, errs
	}

	// Restore the defaulted template
	ms.Spec.Template.Spec = m.Spec
	return true, warnings, nil
}

// validateMachineSetSpec is used to validate any changes to the MachineSet spec outside of
// the providerSpec. Eg it can be used to verify changes to the selector.
func validateMachineSetSpec(ms, oldMS *machinev1beta1.MachineSet) field.ErrorList {
	var errs field.ErrorList
	if oldMS != nil && !reflect.DeepEqual(ms.Spec.Selector, oldMS.Spec.Selector) {
		errs = append(errs, field.Forbidden(field.NewPath("spec", "selector"), "selector is immutable"))
	}

	selector, err := metav1.LabelSelectorAsSelector(&ms.Spec.Selector)
	if err != nil {
		errs = append(errs, field.Invalid(field.NewPath("spec", "selector"), ms.Spec.Selector, fmt.Sprintf("could not convert label selector to selector: %v", err)))
	}
	if selector != nil && !selector.Matches(labels.Set(ms.Spec.Template.Labels)) {
		errs = append(errs, field.Invalid(field.NewPath("spec", "template", "metadata", "labels"), ms.Spec.Template.Labels, "`selector` does not match template `labels`"))
	}

	return errs
}
