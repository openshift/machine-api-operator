package vsphere

import (
	"context"
	"fmt"

	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	apivshpere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1alpha1"
	machineapierros "github.com/openshift/machine-api-operator/pkg/controller/machine"
	"github.com/openshift/machine-api-operator/pkg/controller/vsphere/session"
	"github.com/pkg/errors"
	apicorev1 "k8s.io/api/core/v1"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	credentialsSecretUser     = "user"
	credentialsSecretPassword = "password"
)

// machineScopeParams defines the input parameters used to create a new MachineScope.
type machineScopeParams struct {
	context.Context
	client  runtimeclient.Client
	machine *machinev1.Machine
}

// machineScope defines a scope defined around a machine and its cluster.
type machineScope struct {
	context.Context
	// vsphere session
	session *session.Session
	// api server controller runtime client
	client runtimeclient.Client
	// machine resource
	machine            *machinev1.Machine
	providerSpec       *apivshpere.VSphereMachineProviderSpec
	providerStatus     *apivshpere.VSphereMachineProviderStatus
	machineToBePatched runtimeclient.Patch
}

// newMachineScope creates a new machineScope from the supplied parameters.
// This is meant to be called for each machine actuator operation.
func newMachineScope(params machineScopeParams) (*machineScope, error) {
	providerSpec, err := apivshpere.ProviderSpecFromRawExtension(params.machine.Spec.ProviderSpec.Value)
	if err != nil {
		return nil, machineapierros.InvalidMachineConfiguration("failed to get machine config: %v", err)
	}

	providerStatus, err := apivshpere.ProviderStatusFromRawExtension(params.machine.Status.ProviderStatus)
	if err != nil {
		return nil, machineapierros.InvalidMachineConfiguration("failed to get machine provider status: %v", err.Error())
	}

	user, password, err := getCredentialsSecret(params.client, params.machine.GetNamespace(), *providerSpec)
	if err != nil {
		return nil, fmt.Errorf("%v: error getting credentials: %v", params.machine.GetName(), err)
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
		session:            authSession,
		machine:            params.machine,
		providerSpec:       providerSpec,
		providerStatus:     providerStatus,
		machineToBePatched: runtimeclient.MergeFrom(params.machine.DeepCopy()),
	}, nil
}

// Patch patches the machine spec and machine status after reconciling.
func (s *machineScope) PatchMachine() error {
	klog.V(3).Infof("%v: patching", s.machine.GetName())
	// TODO: copy s.providerStatus in to machine.status
	// TODO: patch machine

	if err := s.client.Status().Patch(context.Background(), s.machine, s.machineToBePatched); err != nil {
		klog.Errorf("Failed to update machine %q: %v", s.machine.GetName(), err)
		return err
	}
	return nil
}

func (s *machineScope) GetSession() *session.Session {
	return s.session
}

// This is a temporary assumption to expose credentials as a secret
// TODO: re-evaluate this when is clear how the credentials are exposed
// for us to consume
//
// expects:
//apiVersion: v1
//kind: Secret
//metadata:
//  name: vsphere
//  namespace: openshift-machine-api
//type: Opaque
//data:
//  user: base64 string
//  password: base64 string
func getCredentialsSecret(client runtimeclient.Client, namespace string, spec apivshpere.VSphereMachineProviderSpec) (string, string, error) {
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
