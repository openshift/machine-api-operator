package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"

	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
func NewMachineSetValidator(client client.Client) (*machineSetValidatorHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	dns, err := getDNS()
	if err != nil {
		return nil, err
	}

	return createMachineSetValidator(infra, client, dns), nil
}

func createMachineSetValidator(infra *osconfigv1.Infrastructure, client client.Client, dns *osconfigv1.DNS) *machineSetValidatorHandler {
	admissionConfig := &admissionConfig{
		dnsDisconnected: dns.Spec.PublicZone == nil,
		clusterID:       infra.Status.InfrastructureName,
		client:          client,
	}
	return &machineSetValidatorHandler{
		admissionHandler: &admissionHandler{
			admissionConfig:   admissionConfig,
			webhookOperations: getMachineValidatorOperation(infra.Status.PlatformStatus.Type),
		},
	}
}

// NewMachineSetDefaulter returns a new machineSetDefaulterHandler.
func NewMachineSetDefaulter() (*machineSetDefaulterHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return createMachineSetDefaulter(infra.Status.PlatformStatus, infra.Status.InfrastructureName), nil
}

func createMachineSetDefaulter(platformStatus *osconfigv1.PlatformStatus, clusterID string) *machineSetDefaulterHandler {
	return &machineSetDefaulterHandler{
		admissionHandler: &admissionHandler{
			admissionConfig:   &admissionConfig{clusterID: clusterID},
			webhookOperations: getMachineDefaulterOperation(platformStatus),
		},
	}
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetValidatorHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	ms := &machinev1beta1.MachineSet{}

	if err := h.decoder.Decode(req, ms); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	var oldMS *machinev1beta1.MachineSet
	if len(req.OldObject.Raw) > 0 {
		// oldMS must only be initialised if there is an old object (ie on UPDATE or DELETE).
		// It should be nil otherwise to allow skipping certain validations that rely on
		// the presence of the old object.
		oldMS = &machinev1beta1.MachineSet{}
		if err := h.decoder.DecodeRaw(req.OldObject, oldMS); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	klog.V(3).Infof("Validate webhook called for MachineSet: %s", ms.GetName())

	ok, warnings, errs := h.validateMachineSet(ms, oldMS)
	if !ok {
		return admission.Denied(errs.Error()).WithWarnings(warnings...)
	}

	return admission.Allowed("MachineSet valid").WithWarnings(warnings...)
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetDefaulterHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	ms := &machinev1beta1.MachineSet{}

	if err := h.decoder.Decode(req, ms); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Mutate webhook called for MachineSet: %s", ms.GetName())

	ok, warnings, errs := h.defaultMachineSet(ms)
	if !ok {
		return admission.Denied(errs.Error()).WithWarnings(warnings...)
	}

	marshaledMachineSet, err := json.Marshal(ms)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err).WithWarnings(warnings...)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledMachineSet).WithWarnings(warnings...)
}

func (h *machineSetValidatorHandler) validateMachineSet(ms, oldMS *machinev1beta1.MachineSet) (bool, []string, utilerrors.Aggregate) {
	errs := validateMachineSetSpec(ms, oldMS)

	// Create a Machine from the MachineSet and validate the Machine template
	m := &machinev1beta1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: ms.GetNamespace(),
		},
		Spec: ms.Spec.Template.Spec,
	}
	ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		errs = append(errs, err.Errors()...)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}
	return true, warnings, nil
}

func (h *machineSetDefaulterHandler) defaultMachineSet(ms *machinev1beta1.MachineSet) (bool, []string, utilerrors.Aggregate) {
	// Create a Machine from the MachineSet and default the Machine template
	m := &machinev1beta1.Machine{Spec: ms.Spec.Template.Spec}
	ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		return false, warnings, utilerrors.NewAggregate(err.Errors())
	}

	// Restore the defaulted template
	ms.Spec.Template.Spec = m.Spec
	return true, warnings, nil
}

// validateMachineSetSpec is used to validate any changes to the MachineSet spec outside of
// the providerSpec. Eg it can be used to verify changes to the selector.
func validateMachineSetSpec(ms, oldMS *machinev1beta1.MachineSet) []error {
	var errs []error
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
