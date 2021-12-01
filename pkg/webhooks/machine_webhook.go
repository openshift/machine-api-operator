package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/util/lifecyclehooks"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	DefaultUserDataSecret  = "worker-user-data"
	DefaultSecretNamespace = "openshift-machine-api"
)

func secretExists(c client.Client, name, namespace string) (bool, error) {
	key := client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}
	obj := &corev1.Secret{}

	if err := c.Get(context.Background(), key, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func CredentialsSecretExists(c client.Client, name, namespace string) []string {
	secretExists, err := secretExists(c, name, namespace)
	if err != nil {
		return []string{
			field.Invalid(
				field.NewPath("providerSpec", "credentialsSecret"),
				name,
				fmt.Sprintf("failed to get credentialsSecret: %v", err),
			).Error(),
		}
	}

	if !secretExists {
		return []string{
			field.Invalid(
				field.NewPath("providerSpec", "credentialsSecret"),
				name,
				"not found. Expected CredentialsSecret to exist",
			).Error(),
		}
	}

	return []string{}
}

type MachineAdmissionFn func(m *machinev1.Machine, client client.Client) (bool, []string, utilerrors.Aggregate)

// machineValidatorHandler validates Machine API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type machineValidatorHandler struct {
	*admissionHandler
}

// machineDefaulterHandler defaults Machine API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type machineDefaulterHandler struct {
	*admissionHandler
}

type admissionHandler struct {
	client            client.Client
	webhookOperations MachineAdmissionFn
	decoder           *admission.Decoder
}

// InjectDecoder injects the decoder.
func (a *admissionHandler) InjectDecoder(d *admission.Decoder) error {
	a.decoder = d
	return nil
}

// NewMachineValidator returns a new machineValidatorHandler.
func NewMachineValidator(client client.Client, admissionFn MachineAdmissionFn) *machineValidatorHandler {
	return &machineValidatorHandler{
		admissionHandler: &admissionHandler{
			client:            client,
			webhookOperations: admissionFn,
		},
	}
}

// NewMachineDefaulter returns a new machineDefaulterHandler.
func NewMachineDefaulter(client client.Client, admissionFn MachineAdmissionFn) *machineDefaulterHandler {
	return &machineDefaulterHandler{
		admissionHandler: &admissionHandler{
			client:            client,
			webhookOperations: admissionFn,
		},
	}
}

func (h *machineValidatorHandler) validateMachine(m, oldM *machinev1.Machine) (bool, []string, utilerrors.Aggregate) {
	errs := validateMachineLifecycleHooks(m, oldM)

	ok, warnings, err := h.webhookOperations(m, h.client)
	if !ok {
		errs = append(errs, err.Errors()...)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}
	return true, warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineValidatorHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	m := &machinev1.Machine{}

	if err := h.decoder.Decode(req, m); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	var oldM *machinev1.Machine
	if len(req.OldObject.Raw) > 0 {
		// oldM must only be initialised if there is an old object (ie on UPDATE or DELETE).
		// It should be nil otherwise to allow skipping certain validations that rely on
		// the presence of the old object.
		oldM = &machinev1.Machine{}
		if err := h.decoder.DecodeRaw(req.OldObject, oldM); err != nil {
			return admission.Errored(http.StatusBadRequest, err)
		}
	}

	klog.V(3).Infof("Validate webhook called for Machine: %s", m.GetName())

	ok, warnings, errs := h.validateMachine(m, oldM)
	if !ok {
		return admission.Denied(errs.Error()).WithWarnings(warnings...)
	}

	return admission.Allowed("Machine valid").WithWarnings(warnings...)
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineDefaulterHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	m := &machinev1.Machine{}

	if err := h.decoder.Decode(req, m); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Mutate webhook called for Machine: %s", m.GetName())

	ok, warnings, errs := h.webhookOperations(m, h.client)
	if !ok {
		return admission.Denied(errs.Error()).WithWarnings(warnings...)
	}

	marshaledMachine, err := json.Marshal(m)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err).WithWarnings(warnings...)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledMachine).WithWarnings(warnings...)
}

func validateMachineLifecycleHooks(m, oldM *machinev1.Machine) []error {
	var errs []error

	if nameErrs := checkUniqueHookNames(m.Spec.LifecycleHooks.PreDrain, field.NewPath("spec", "lifecycleHooks", "preDrain")); len(nameErrs) > 0 {
		errs = append(errs, nameErrs...)
	}
	if nameErrs := checkUniqueHookNames(m.Spec.LifecycleHooks.PreTerminate, field.NewPath("spec", "lifecycleHooks", "preTerminate")); len(nameErrs) > 0 {
		errs = append(errs, nameErrs...)
	}

	if isDeleting(m) && oldM != nil {
		changedPreDrain := lifecyclehooks.GetChangedLifecycleHooks(oldM.Spec.LifecycleHooks.PreDrain, m.Spec.LifecycleHooks.PreDrain)
		if len(changedPreDrain) > 0 {
			errs = append(errs, field.Forbidden(field.NewPath("spec", "lifecycleHooks", "preDrain"), fmt.Sprintf("pre-drain hooks are immutable when machine is marked for deletion: the following hooks are new or changed: %+v", changedPreDrain)))
		}

		changedPreTerminate := lifecyclehooks.GetChangedLifecycleHooks(oldM.Spec.LifecycleHooks.PreTerminate, m.Spec.LifecycleHooks.PreTerminate)
		if len(changedPreTerminate) > 0 {
			errs = append(errs, field.Forbidden(field.NewPath("spec", "lifecycleHooks", "preTerminate"), fmt.Sprintf("pre-terminate hooks are immutable when machine is marked for deletion: the following hooks are new or changed: %+v", changedPreTerminate)))
		}
	}

	return errs
}

// checkUniqueHookNames checks that the names of hooks within a lifecycle stage are unique
func checkUniqueHookNames(hooks []machinev1.LifecycleHook, parent *field.Path) []error {
	errs := []error{}
	names := make(map[string]struct{})

	for i, hook := range hooks {
		if _, found := names[hook.Name]; found {
			errs = append(errs, field.Forbidden(parent.Index(i).Child("name"), fmt.Sprintf("hook names must be unique within a lifecycle stage, the following hook name is already set: %s", hook.Name)))
		} else {
			names[hook.Name] = struct{}{}
		}
	}

	return errs
}

func isDeleting(obj metav1.Object) bool {
	return obj.GetDeletionTimestamp() != nil
}
