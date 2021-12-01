package vsphere

import (
	"context"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	mapiwebhooks "github.com/openshift/machine-api-operator/pkg/webhooks"
	corev1 "k8s.io/api/core/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// vSphere Defaults
	defaultVSphereCredentialsSecret = "vsphere-cloud-credentials"
	// Minimum vSphere values taken from vSphere reconciler
	minVSphereCPU       = 2
	minVSphereMemoryMiB = 2048
	// https://docs.openshift.com/container-platform/4.1/installing/installing_vsphere/installing-vsphere.html#minimum-resource-requirements_installing-vsphere
	minVSphereDiskGiB = 120
)

func DefaultVSphere(m *machinev1.Machine, client runtimeclient.Client) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting vSphere providerSpec")

	var errs []error
	var warnings []string

	if m.Spec.ProviderSpec.Value == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "value"), "a value must be provided"))
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	infra := &configv1.Infrastructure{}
	infraName := runtimeclient.ObjectKey{Name: globalInfrastuctureName}
	if err := client.Get(context.Background(), infraName, infra); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	// Only enforce the clusterID if it's not set.
	// Otherwise a discrepancy on the value would leave the machine orphan
	// and would trigger a new machine creation by the machineSet.
	// https://bugzilla.redhat.com/show_bug.cgi?id=1857175
	if m.Labels == nil {
		m.Labels = make(map[string]string)
	}
	if _, ok := m.Labels[machinev1.MachineClusterIDLabel]; !ok {
		m.Labels[machinev1.MachineClusterIDLabel] = infra.Status.InfrastructureName
	}

	providerSpec, err := ProviderSpecFromRawExtension(m.Spec.ProviderSpec.Value)
	if err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: mapiwebhooks.DefaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultVSphereCredentialsSecret}
	}

	rawProviderSpec, err := RawExtensionFromProviderSpec(providerSpec)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	m.Spec.ProviderSpec.Value = rawProviderSpec
	return true, warnings, nil
}

func ValidateVSphere(m *machinev1.Machine, client runtimeclient.Client) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating vSphere providerSpec")

	var errs []error
	var warnings []string

	if m.Spec.ProviderSpec.Value == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "value"), "a value must be provided"))
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	providerSpec, err := ProviderSpecFromRawExtension(m.Spec.ProviderSpec.Value)
	if err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.Template == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "template"), "template must be provided"))
	}

	workspaceWarnings, workspaceErrors := validateVSphereWorkspace(providerSpec.Workspace, field.NewPath("providerSpec", "workspace"))
	warnings = append(warnings, workspaceWarnings...)
	errs = append(errs, workspaceErrors...)

	errs = append(errs, validateVSphereNetwork(providerSpec.Network, field.NewPath("providerSpec", "network"))...)

	if providerSpec.NumCPUs < minVSphereCPU {
		warnings = append(warnings, fmt.Sprintf("providerSpec.numCPUs: %d is missing or less than the minimum value (%d): nodes may not boot correctly", providerSpec.NumCPUs, minVSphereCPU))
	}
	if providerSpec.MemoryMiB < minVSphereMemoryMiB {
		warnings = append(warnings, fmt.Sprintf("providerSpec.memoryMiB: %d is missing or less than the recommended minimum value (%d): nodes may not boot correctly", providerSpec.MemoryMiB, minVSphereMemoryMiB))
	}
	if providerSpec.DiskGiB < minVSphereDiskGiB {
		warnings = append(warnings, fmt.Sprintf("providerSpec.diskGiB: %d is missing or less than the recommended minimum (%d): nodes may fail to start if disk size is too low", providerSpec.DiskGiB, minVSphereDiskGiB))
	}

	if providerSpec.UserDataSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret"), "userDataSecret must be provided"))
	} else {
		if providerSpec.UserDataSecret.Name == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret", "name"), "name must be provided"))
		}
	}

	if providerSpec.CredentialsSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret"), "credentialsSecret must be provided"))
	} else {
		if providerSpec.CredentialsSecret.Name == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret", "name"), "name must be provided"))
		} else {
			warnings = append(warnings, mapiwebhooks.CredentialsSecretExists(client, providerSpec.CredentialsSecret.Name, m.GetNamespace())...)
		}
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}
	return true, warnings, nil
}

func validateVSphereWorkspace(workspace *machinev1.Workspace, parentPath *field.Path) ([]string, []error) {
	if workspace == nil {
		return []string{}, []error{field.Required(parentPath, "workspace must be provided")}
	}

	var errs []error
	var warnings []string
	if workspace.Server == "" {
		errs = append(errs, field.Required(parentPath.Child("server"), "server must be provided"))
	}
	if workspace.Datacenter == "" {
		warnings = append(warnings, fmt.Sprintf("%s: datacenter is unset: if more than one datacenter is present, VMs cannot be created", parentPath.Child("datacenter")))
	}
	if workspace.Folder != "" {
		expectedPrefix := fmt.Sprintf("/%s/vm/", workspace.Datacenter)
		if !strings.HasPrefix(workspace.Folder, expectedPrefix) {
			errMsg := fmt.Sprintf("folder must be absolute path: expected prefix %q", expectedPrefix)
			errs = append(errs, field.Invalid(parentPath.Child("folder"), workspace.Folder, errMsg))
		}
	}

	return warnings, errs
}

func validateVSphereNetwork(network machinev1.NetworkSpec, parentPath *field.Path) []error {
	if len(network.Devices) == 0 {
		return []error{field.Required(parentPath.Child("devices"), "at least 1 network device must be provided")}
	}

	var errs []error
	for i, spec := range network.Devices {
		fldPath := parentPath.Child("devices").Index(i)
		if spec.NetworkName == "" {
			errs = append(errs, field.Required(fldPath.Child("networkName"), "networkName must be provided"))
		}
	}

	return errs
}
