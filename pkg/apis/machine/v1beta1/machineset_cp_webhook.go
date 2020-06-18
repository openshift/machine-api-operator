package v1beta1

import (
	"context"
	"fmt"
	"net/http"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// MachineSetCPHandler validates ControlPlane MachineSet API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type MachineSetCPHandler struct {
	decoder *admission.Decoder
}

// NewMachineSetCPValidator returns a new MachineSetCPHandler.
func NewMachineSetCPValidator() (*MachineSetCPHandler, error) {
	return createMachineSetCPValidator(), nil
}

func createMachineSetCPValidator() *MachineSetCPHandler {
	return &MachineSetCPHandler{}
}

// InjectDecoder injects the decoder.
func (v *MachineSetCPHandler) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// Handle handles HTTP requests for admission webhook servers.
func (v *MachineSetCPHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	oldMS := &MachineSet{}

	// Delete requests, the req.Object is empty.
	if err := v.decoder.DecodeRaw(req.OldObject, oldMS); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Validate webhook called for CP MachineSets: %s", oldMS.GetName())

	newMS := &MachineSet{}
	if req.Operation != admissionv1beta1.Delete {
		if err := v.decoder.Decode(req, newMS); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	// Succeed if deleting non-CP MachineSet or Updating a non-CP MachineSet
	// and the user is not attempting to change it to a CP MachineSet.
	if !isCPMS(oldMS) && (req.Operation == admissionv1beta1.Delete || !isCPMS(newMS)) {
		return admission.Allowed("MachineSet is Not Control Plane.")
	}

	// If the user is updating a CP MachineSet, as long as machine role is
	// unchanged, we're ok.
	if req.Operation != admissionv1beta1.Delete && isCPMS(newMS) && isCPMS(oldMS) {
		return admission.Allowed("Control Plane MachineSet is Valid.")
	}

	// User is peforming an unallowed operation

	// TODO(michaelgugino): Ensure we account for MachineDeployment ownership
	// of a CP machineset in the future if we use them.
	return admission.Denied(fmt.Sprintf("Requested %v of Control Plane MachineSet Not Allowed.", req.Operation))

}

func isCPMS(ms *MachineSet) bool {
	if ms.Spec.Template.ObjectMeta.Labels == nil {
		return false
	}
	val, ok := ms.Spec.Template.ObjectMeta.Labels["machine.openshift.io/cluster-api-machine-role"]
	if ok {
		if val == "master" {
			return true
		}
	}
	return false
}
