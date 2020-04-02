package vsphere

import (
	"context"
	"fmt"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	apivsphere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	machineapierros "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere/session"
	"github.com/pkg/errors"
	apicorev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	userDataSecretKey = "userData"
)

// machineScopeParams defines the input parameters used to create a new MachineScope.
type machineScopeParams struct {
	context.Context
	client    runtimeclient.Client
	apiReader runtimeclient.Reader
	machine   *machinev1.Machine
}

// machineScope defines a scope defined around a machine and its cluster.
type machineScope struct {
	context.Context
	// vsphere session
	session *session.Session
	// api server controller runtime client
	client runtimeclient.Client
	// client reader that bypasses the manager's cache
	apiReader runtimeclient.Reader
	// vSphere cloud-provider config
	vSphereConfig *vSphereConfig
	// machine resource
	machine            *machinev1.Machine
	providerSpec       *apivsphere.VSphereMachineProviderSpec
	providerStatus     *apivsphere.VSphereMachineProviderStatus
	machineToBePatched runtimeclient.Patch
}

// newMachineScope creates a new machineScope from the supplied parameters.
// This is meant to be called for each machine actuator operation.
func newMachineScope(params machineScopeParams) (*machineScope, error) {
	if params.Context == nil {
		return nil, fmt.Errorf("%v: machine scope require a context", params.machine.GetName())
	}

	vSphereConfig, err := getVSphereConfig(params.apiReader)
	if err != nil {
		klog.Errorf("Failed to fetch vSphere config: %v", err)
	}

	providerSpec, err := apivsphere.ProviderSpecFromRawExtension(params.machine.Spec.ProviderSpec.Value)
	if err != nil {
		return nil, machineapierros.InvalidMachineConfiguration("failed to get machine config: %v", err)
	}

	providerStatus, err := apivsphere.ProviderStatusFromRawExtension(params.machine.Status.ProviderStatus)
	if err != nil {
		return nil, machineapierros.InvalidMachineConfiguration("failed to get machine provider status: %v", err.Error())
	}

	user, password, err := getCredentialsSecret(params.client, params.machine.GetNamespace(), *providerSpec)
	if err != nil {
		return nil, fmt.Errorf("%v: error getting credentials: %v", params.machine.GetName(), err)
	}
	if providerSpec.Workspace == nil {
		return nil, fmt.Errorf("%v: no workspace provided", params.machine.GetName())
	}
	authSession, err := session.GetOrCreate(context.TODO(),
		providerSpec.Workspace.Server, providerSpec.Workspace.Datacenter,
		user, password)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create vSphere session")
	}

	return &machineScope{
		Context:            params.Context,
		client:             params.client,
		apiReader:          params.apiReader,
		session:            authSession,
		machine:            params.machine,
		providerSpec:       providerSpec,
		providerStatus:     providerStatus,
		vSphereConfig:      vSphereConfig,
		machineToBePatched: runtimeclient.MergeFrom(params.machine.DeepCopy()),
	}, nil
}

// Patch patches the machine spec and machine status after reconciling.
func (s *machineScope) PatchMachine() error {
	klog.V(3).Infof("%v: patching machine", s.machine.GetName())

	providerStatus, err := apivsphere.RawExtensionFromProviderStatus(s.providerStatus)
	if err != nil {
		return machineapierros.InvalidMachineConfiguration("failed to get machine provider status: %v", err.Error())
	}
	s.machine.Status.ProviderStatus = providerStatus

	statusCopy := *s.machine.Status.DeepCopy()

	// patch machine
	if err := s.client.Patch(context.Background(), s.machine, s.machineToBePatched); err != nil {
		klog.Errorf("Failed to patch machine %q: %v", s.machine.GetName(), err)
		return err
	}

	s.machine.Status = statusCopy

	// patch status
	if err := s.client.Status().Patch(context.Background(), s.machine, s.machineToBePatched); err != nil {
		klog.Errorf("Failed to patch machine status %q: %v", s.machine.GetName(), err)
		return err
	}

	return nil
}

func (s *machineScope) GetSession() *session.Session {
	return s.session
}

// GetUserData fetches the user-data from the secret referenced in the Machine's
// provider spec, if one is set.
func (s *machineScope) GetUserData() ([]byte, error) {
	if s.providerSpec == nil || s.providerSpec.UserDataSecret == nil {
		return nil, nil
	}

	userDataSecret := &apicorev1.Secret{}

	objKey := runtimeclient.ObjectKey{
		Namespace: s.machine.Namespace,
		Name:      s.providerSpec.UserDataSecret.Name,
	}

	if err := s.client.Get(context.Background(), objKey, userDataSecret); err != nil {
		return nil, err
	}

	userData, exists := userDataSecret.Data[userDataSecretKey]
	if !exists {
		return nil, fmt.Errorf("secret %s missing %s key", objKey, userDataSecretKey)
	}

	return userData, nil
}

// getCredentialsSecret returns the username and password from the VSphere credentials secret.
// The secret is expected to be in the format documented here:
// https://vmware.github.io/vsphere-storage-for-kubernetes/documentation/k8s-secret.html
//
// Assuming the vcenter is our dev server vcsa.vmware.devcluster.openshift.com,
// the secret would be in this format:
//apiVersion: v1
//kind: Secret
//metadata:
//  name: vsphere
//  namespace: openshift-machine-api
//type: Opaque
//data:
//  vcsa.vmware.devcluster.openshift.com.username: base64 string
//  vcsa.vmware.devcluster.openshift.com.password: base64 string
func getCredentialsSecret(client runtimeclient.Client, namespace string, spec apivsphere.VSphereMachineProviderSpec) (string, string, error) {
	if spec.CredentialsSecret == nil {
		return "", "", nil
	}

	var credentialsSecret apicorev1.Secret
	if err := client.Get(context.Background(),
		runtimeclient.ObjectKey{Namespace: namespace, Name: spec.CredentialsSecret.Name},
		&credentialsSecret); err != nil {

		if apimachineryerrors.IsNotFound(err) {
			machineapierros.InvalidMachineConfiguration("credentials secret %v/%v not found: %v", namespace, spec.CredentialsSecret.Name, err.Error())
		}
		return "", "", fmt.Errorf("error getting credentials secret %v/%v: %v", namespace, spec.CredentialsSecret.Name, err)
	}

	// TODO: add provider spec validation logic and move this check there
	if spec.Workspace == nil {
		return "", "", errors.New("no workspace")
	}

	credentialsSecretUser := fmt.Sprintf("%s.username", spec.Workspace.Server)
	credentialsSecretPassword := fmt.Sprintf("%s.password", spec.Workspace.Server)

	user, exists := credentialsSecret.Data[credentialsSecretUser]
	if !exists {
		return "", "", machineapierros.InvalidMachineConfiguration("secret %v/%v does not have %q field set", namespace, spec.CredentialsSecret.Name, credentialsSecretUser)
	}

	password, exists := credentialsSecret.Data[credentialsSecretPassword]
	if !exists {
		return "", "", machineapierros.InvalidMachineConfiguration("secret %v/%v does not have %q field set", namespace, spec.CredentialsSecret.Name, credentialsSecretPassword)
	}

	return string(user), string(password), nil
}
