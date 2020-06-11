package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	osconfigv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog"
	"k8s.io/utils/pointer"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	azure "sigs.k8s.io/cluster-api-provider-azure/pkg/apis/azureprovider/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	yaml "sigs.k8s.io/yaml"
)

var (
	// AWS Defaults
	defaultAWSIAMInstanceProfile = func(clusterID string) *string {
		return pointer.StringPtr(fmt.Sprintf("%s-worker-profile", clusterID))
	}
	defaultAWSSecurityGroup = func(clusterID string) string {
		return fmt.Sprintf("%s-worker-sg", clusterID)
	}
	defaultAWSSubnet = func(clusterID, az string) string {
		return fmt.Sprintf("%s-private-%s", clusterID, az)
	}

	// Azure Defaults
	defaultAzureVnet = func(clusterID string) string {
		return fmt.Sprintf("%s-vnet", clusterID)
	}
	defaultAzureSubnet = func(clusterID string) string {
		return fmt.Sprintf("%s-worker-subnet", clusterID)
	}
	defaultAzureNetworkResourceGroup = func(clusterID string) string {
		return fmt.Sprintf("%s-rg", clusterID)
	}
	defaultAzureImageResourceID = func(clusterID string) string {
		return fmt.Sprintf("/resourceGroups/%s/providers/Microsoft.Compute/images/%s", clusterID+"-rg", clusterID)
	}
	defaultAzureManagedIdentiy = func(clusterID string) string {
		return fmt.Sprintf("%s-identity", clusterID)
	}
	defaultAzureResourceGroup = func(clusterID string) string {
		return fmt.Sprintf("%s-rg", clusterID)
	}
)

const (
	defaultUserDataSecret  = "worker-user-data"
	defaultSecretNamespace = "openshift-machine-api"

	// AWS Defaults
	defaultAWSCredentialsSecret = "aws-cloud-credentials"
	defaultAWSInstanceType      = "m4.large"

	// Azure Defaults
	defaultAzureVMSize            = "Standard_D4s_V3"
	defaultAzureCredentialsSecret = "azure-cloud-credentials"
	defaultAzureOSDiskOSType      = "Linux"
	defaultAzureOSDiskStorageType = "Premium_LRS"
)

func getInfra() (*osconfigv1.Infrastructure, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	client, err := osclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	infra, err := client.ConfigV1().Infrastructures().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	return infra, nil
}

type handlerValidationFn func(h *validatorHandler, m *Machine) (bool, utilerrors.Aggregate)
type handlerMutationFn func(h *defaulterHandler, m *Machine) (bool, utilerrors.Aggregate)

// validatorHandler validates Machine API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type validatorHandler struct {
	clusterID         string
	webhookOperations handlerValidationFn
	decoder           *admission.Decoder
}

// defaulterHandler defaults Machine API resources.
// implements type Handler interface.
// https://godoc.org/github.com/kubernetes-sigs/controller-runtime/pkg/webhook/admission#Handler
type defaulterHandler struct {
	clusterID         string
	webhookOperations handlerMutationFn
	decoder           *admission.Decoder
}

// NewValidator returns a new validatorHandler.
func NewMachineValidator() (*validatorHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return createMachineValidator(infra.Status.PlatformStatus.Type, infra.Status.InfrastructureName), nil
}

func createMachineValidator(platform osconfigv1.PlatformType, clusterID string) *validatorHandler {
	h := &validatorHandler{
		clusterID: clusterID,
	}

	switch platform {
	case osconfigv1.AWSPlatformType:
		h.webhookOperations = validateAWS
	case osconfigv1.AzurePlatformType:
		h.webhookOperations = validateAzure
	default:
		// just no-op
		h.webhookOperations = func(h *validatorHandler, m *Machine) (bool, utilerrors.Aggregate) {
			return true, nil
		}
	}
	return h
}

// NewDefaulter returns a new defaulterHandler.
func NewMachineDefaulter() (*defaulterHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return createMachineDefaulter(infra.Status.PlatformStatus.Type, infra.Status.InfrastructureName), nil
}

func createMachineDefaulter(platform osconfigv1.PlatformType, clusterID string) *defaulterHandler {
	h := &defaulterHandler{
		clusterID: clusterID,
	}

	switch platform {
	case osconfigv1.AWSPlatformType:
		h.webhookOperations = defaultAWS
	case osconfigv1.AzurePlatformType:
		h.webhookOperations = defaultAzure
	default:
		// just no-op
		h.webhookOperations = func(h *defaulterHandler, m *Machine) (bool, utilerrors.Aggregate) {
			return true, nil
		}
	}
	return h
}

// InjectDecoder injects the decoder.
func (v *validatorHandler) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// InjectDecoder injects the decoder.
func (v *defaulterHandler) InjectDecoder(d *admission.Decoder) error {
	v.decoder = d
	return nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *validatorHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	m := &Machine{}

	if err := h.decoder.Decode(req, m); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Validate webhook called for Machine: %s", m.GetName())

	if ok, err := h.webhookOperations(h, m); !ok {
		return admission.Denied(err.Error())
	}

	return admission.Allowed("Machine valid")
}

// Handle handles HTTP requests for admission webhook servers.
func (h *defaulterHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	m := &Machine{}

	if err := h.decoder.Decode(req, m); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	klog.V(3).Infof("Mutate webhook called for Machine: %s", m.GetName())

	if ok, err := h.webhookOperations(h, m); !ok {
		return admission.Denied(err.Error())
	}

	marshaledMachine, err := json.Marshal(m)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledMachine)
}

func defaultAWS(h *defaulterHandler, m *Machine) (bool, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting AWS providerSpec")

	var errs []error
	providerSpec := new(aws.AWSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, utilerrors.NewAggregate(errs)
	}

	if providerSpec.InstanceType == "" {
		providerSpec.InstanceType = defaultAWSInstanceType
	}
	if providerSpec.IAMInstanceProfile == nil {
		providerSpec.IAMInstanceProfile = &aws.AWSResourceReference{ID: defaultAWSIAMInstanceProfile(h.clusterID)}
	}
	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: defaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret}
	}

	if providerSpec.SecurityGroups == nil {
		providerSpec.SecurityGroups = []aws.AWSResourceReference{
			{
				Filters: []aws.Filter{
					{
						Name:   "tag:Name",
						Values: []string{defaultAWSSecurityGroup(h.clusterID)},
					},
				},
			},
		}
	}

	if providerSpec.Subnet.ARN == nil && providerSpec.Subnet.ID == nil && providerSpec.Subnet.Filters == nil {
		providerSpec.Subnet.Filters = []aws.Filter{
			{
				Name:   "tag:Name",
				Values: []string{defaultAWSSubnet(h.clusterID, providerSpec.Placement.AvailabilityZone)},
			},
		}
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, utilerrors.NewAggregate(errs)
	}

	m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}
	return true, nil
}

func unmarshalInto(m *Machine, providerSpec interface{}) error {
	if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &providerSpec); err != nil {
		return field.Invalid(field.NewPath("providerSpec", "value"), providerSpec, err.Error())
	}
	return nil
}

func validateAWS(h *validatorHandler, m *Machine) (bool, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating AWS providerSpec")

	var errs []error
	providerSpec := new(aws.AWSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, utilerrors.NewAggregate(errs)
	}

	if providerSpec.AMI.ARN == nil && providerSpec.AMI.Filters == nil && providerSpec.AMI.ID == nil {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "ami"),
				"expected either providerSpec.ami.arn or providerSpec.ami.filters or providerSpec.ami.id to be populated",
			),
		)
	}

	if providerSpec.InstanceType == "" {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "instanceType"),
				"expected providerSpec.instanceType to be populated",
			),
		)
	}

	if providerSpec.IAMInstanceProfile == nil {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "iamInstanceProfile"),
				"expected providerSpec.iamInstanceProfile to be populated",
			),
		)
	}

	if providerSpec.UserDataSecret == nil {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "userDataSecret"),
				"expected providerSpec.userDataSecret to be populated",
			),
		)
	}

	if providerSpec.CredentialsSecret == nil {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "credentialsSecret"),
				"expected providerSpec.credentialsSecret to be populated",
			),
		)
	}

	if providerSpec.SecurityGroups == nil {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "securityGroups"),
				"expected providerSpec.securityGroups to be populated",
			),
		)
	}

	if providerSpec.Subnet.ARN == nil && providerSpec.Subnet.ID == nil && providerSpec.Subnet.Filters == nil && providerSpec.Placement.AvailabilityZone == "" {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "subnet"),
				"expected either providerSpec.subnet.arn or providerSpec.subnet.id or providerSpec.subnet.filters or providerSpec.placement.availabilityZone to be populated",
			),
		)
	}
	// TODO(alberto): Validate providerSpec.BlockDevices.
	// https://github.com/openshift/cluster-api-provider-aws/pull/299#discussion_r433920532

	if len(errs) > 0 {
		return false, utilerrors.NewAggregate(errs)
	}

	return true, nil
}

func defaultAzure(h *defaulterHandler, m *Machine) (bool, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting Azure providerSpec")

	var errs []error
	providerSpec := new(azure.AzureMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, utilerrors.NewAggregate(errs)
	}

	if providerSpec.VMSize == "" {
		providerSpec.VMSize = defaultAzureVMSize
	}

	// Vnet and Subnet need to be provided together by the user
	if providerSpec.Vnet == "" && providerSpec.Subnet == "" {
		providerSpec.Vnet = defaultAzureVnet(h.clusterID)
		providerSpec.Subnet = defaultAzureSubnet(h.clusterID)

		// NetworkResourceGroup can be set by the user without Vnet and Subnet,
		// only override if they didn't set it
		if providerSpec.NetworkResourceGroup == "" {
			providerSpec.NetworkResourceGroup = defaultAzureNetworkResourceGroup(h.clusterID)
		}
	}

	if providerSpec.Image.ResourceID == "" {
		providerSpec.Image.ResourceID = defaultAzureImageResourceID(h.clusterID)
	}

	if providerSpec.ManagedIdentity == "" {
		providerSpec.ManagedIdentity = defaultAzureManagedIdentiy(h.clusterID)
	}

	if providerSpec.ResourceGroup == "" {
		providerSpec.ResourceGroup = defaultAzureResourceGroup(h.clusterID)
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.SecretReference{Name: defaultUserDataSecret, Namespace: defaultSecretNamespace}
	} else {
		if providerSpec.UserDataSecret.Namespace == "" {
			providerSpec.UserDataSecret.Namespace = defaultSecretNamespace
		}
		if providerSpec.UserDataSecret.Name == "" {
			providerSpec.UserDataSecret.Name = defaultUserDataSecret
		}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.SecretReference{Name: defaultAzureCredentialsSecret, Namespace: defaultSecretNamespace}
	} else {
		if providerSpec.CredentialsSecret.Namespace == "" {
			providerSpec.CredentialsSecret.Namespace = defaultSecretNamespace
		}
		if providerSpec.CredentialsSecret.Name == "" {
			providerSpec.CredentialsSecret.Name = defaultAzureCredentialsSecret
		}
	}

	if providerSpec.OSDisk.OSType == "" {
		providerSpec.OSDisk.OSType = defaultAzureOSDiskOSType
	}

	if providerSpec.OSDisk.ManagedDisk.StorageAccountType == "" {
		providerSpec.OSDisk.ManagedDisk.StorageAccountType = defaultAzureOSDiskStorageType
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, utilerrors.NewAggregate(errs)
	}

	m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}
	return true, nil
}

func validateAzure(h *validatorHandler, m *Machine) (bool, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating Azure providerSpec")

	var errs []error
	providerSpec := new(azure.AzureMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, utilerrors.NewAggregate(errs)
	}

	if providerSpec.Location == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "location"), "location should be set to one of the supported Azure regions"))
	}

	if providerSpec.VMSize == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "vmSize"), "vmSize should be set to one of the supported Azure VM sizes"))
	}

	// Vnet requires Subnet
	if providerSpec.Vnet != "" && providerSpec.Subnet == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "subnet"), "must provide a subnet when a virtual network is specified"))
	}

	// Subnet requires Vnet
	if providerSpec.Subnet != "" && providerSpec.Vnet == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "vnet"), "must provide a virtual network when supplying subnets"))
	}

	// Vnet + Subnet requires NetworkResourceGroup
	if (providerSpec.Vnet != "" || providerSpec.Subnet != "") && providerSpec.NetworkResourceGroup == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "networkResourceGroup"), "must provide a network resource group when a virtual network or subnet is specified"))
	}

	if providerSpec.Image.ResourceID == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "image", "resourceID"), "resourceID must be provided"))
	}

	if providerSpec.ManagedIdentity == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "managedIdentity"), "managedIdentity must be provided"))
	}

	if providerSpec.ResourceGroup == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "resourceGropu"), "resourceGroup must be provided"))
	}

	if providerSpec.UserDataSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret"), "userDataSecret must be provided"))
	} else {
		if providerSpec.UserDataSecret.Namespace == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret", "namespace"), "namespace must be provided"))
		}
		if providerSpec.UserDataSecret.Name == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret", "name"), "name must be provided"))
		}
	}

	if providerSpec.CredentialsSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret"), "credentialsSecret must be provided"))
	} else {
		if providerSpec.CredentialsSecret.Namespace == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret", "namespace"), "namespace must be provided"))
		}
		if providerSpec.CredentialsSecret.Name == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret", "name"), "name must be provided"))
		}
	}

	if providerSpec.OSDisk.DiskSizeGB <= 0 {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "osDisk", "diskSizeGB"), providerSpec.OSDisk.DiskSizeGB, "diskSizeGB must be greater than zero"))
	}

	if providerSpec.OSDisk.OSType == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "osDisk", "osType"), "osType must be provided"))
	}
	if providerSpec.OSDisk.ManagedDisk.StorageAccountType == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "osDisk", "managedDisk", "storageAccountType"), "storageAccountType must be provided"))
	}

	if len(errs) > 0 {
		return false, utilerrors.NewAggregate(errs)
	}
	return true, nil
}
