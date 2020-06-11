package v1beta1

import (
	"context"
	"encoding/json"
	"net/http"

	osconfigv1 "github.com/openshift/api/config/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// machineSetValidatorHandler validates MachineSet API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type machineSetValidatorHandler struct {
	clusterID         string
	webhookOperations machineAdmissionFn
	decoder           *admission.Decoder
}

// machineSetDefaulterHandler defaults MachineSet API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type machineSetDefaulterHandler struct {
	clusterID         string
	webhookOperations machineAdmissionFn
	decoder           *admission.Decoder
}

// NewMachineSetValidator returns a new machineSetValidatorHandler.
func NewMachineSetValidator() (*machineSetValidatorHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return createMachineSetValidator(infra.Status.PlatformStatus.Type, infra.Status.InfrastructureName), nil
}

func createMachineSetValidator(platform osconfigv1.PlatformType, clusterID string) *machineSetValidatorHandler {
	return &machineSetValidatorHandler{
		clusterID:         clusterID,
		webhookOperations: getMachineValidatorOperation(platform),
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
		clusterID:         clusterID,
		webhookOperations: getMachineDefaulterOperation(platformStatus),
	}
}

// InjectDecoder injects the decoder.
func (v *machineSetValidatorHandler) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// InjectDecoder injects the decoder.
func (v *machineSetDefaulterHandler) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetValidatorHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	ms := &MachineSet{}

	if err := h.decoder.Decode(req, ms); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Validate webhook called for MachineSet: %s", ms.GetName())

	if ok, err := h.validateMachineSet(ms); !ok {
		return admission.Denied(err.Error())
	}

	return admission.Allowed("MachineSet valid")
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineSetDefaulterHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	ms := &MachineSet{}

	if err := h.decoder.Decode(req, ms); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Mutate webhook called for MachineSet: %s", ms.GetName())

	if ok, err := h.defaultMachineSet(ms); !ok {
		return admission.Denied(err.Error())
	}

	marshaledMachineSet, err := json.Marshal(ms)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledMachineSet)
}

func (h *machineSetValidatorHandler) validateMachineSet(ms *MachineSet) (bool, utilerrors.Aggregate) {
	var errs []error

	// Create a Machine from the MachineSet and validate the Machine template
	m := &Machine{Spec: ms.Spec.Template.Spec}
	if ok, err := h.webhookOperations(m, h.clusterID); !ok {
		errs = append(errs, err.Errors()...)
	}

	if len(errs) > 0 {
		return false, utilerrors.NewAggregate(errs)
	}
	return true, nil
}

func (h *machineSetDefaulterHandler) defaultMachineSet(ms *MachineSet) (bool, utilerrors.Aggregate) {
	var errs []error

	// Create a Machine from the MachineSet and default the Machine template
	m := &Machine{Spec: ms.Spec.Template.Spec}
	if ok, err := h.webhookOperations(m, h.clusterID); !ok {
		errs = append(errs, err.Errors()...)
	} else {
		// Restore the defaulted template
		ms.Spec.Template.Spec = m.Spec
	}

	if len(errs) > 0 {
		return false, utilerrors.NewAggregate(errs)
	}
	return true, nil
}
