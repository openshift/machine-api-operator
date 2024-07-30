package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	goruntime "runtime"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	"k8s.io/utils/strings/slices"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/scheme"
	"sigs.k8s.io/yaml"

	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/machine-api-operator/pkg/util/lifecyclehooks"
)

type systemSpecifications struct {
	minMemoryGiB             int32
	maxMemoryGiB             int32
	minProcessorSharedCapped float64
	minProcessorDedicated    float64
	maxProcessor             float64
}

type machineArch string

var (
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
		// image gallery names cannot have dashes
		galleryName := strings.Replace(clusterID, "-", "_", -1)
		imageName := clusterID
		if arch == ARM64 {
			// append gen2 to the image name for ARM64.
			// Although the installer creates a gen2 image for AMD64, we cannot guarantee that clusters created
			// before that change will have a -gen2 image.
			imageName = fmt.Sprintf("%s-gen2", clusterID)
		}
		return fmt.Sprintf("/resourceGroups/%s/providers/Microsoft.Compute/galleries/gallery_%s/images/%s/versions/%s", clusterID+"-rg", galleryName, imageName, azureRHCOSVersion)
	}
	defaultAzureManagedIdentiy = func(clusterID string) string {
		return fmt.Sprintf("%s-identity", clusterID)
	}
	defaultAzureResourceGroup = func(clusterID string) string {
		return fmt.Sprintf("%s-rg", clusterID)
	}

	// GCP Defaults
	defaultGCPNetwork = func(clusterID string) string {
		return fmt.Sprintf("%s-network", clusterID)
	}
	defaultGCPSubnetwork = func(clusterID string) string {
		return fmt.Sprintf("%s-worker-subnet", clusterID)
	}
	defaultGCPTags = func(clusterID string) []string {
		return []string{fmt.Sprintf("%s-worker", clusterID)}
	}

	defaultGCPDiskImage = func() string {
		if arch == ARM64 {
			return defaultGCPARMDiskImage
		}
		return defaultGCPX86DiskImage
	}

	// Power VS variables

	//powerVSMachineConfigurations contains the known Power VS system types and their allowed configuration limits
	powerVSMachineConfigurations = map[string]systemSpecifications{
		"s922": {
			minMemoryGiB:             32,
			maxMemoryGiB:             942,
			minProcessorSharedCapped: 0.5,
			minProcessorDedicated:    1,
			maxProcessor:             15,
		},
		"e880": {
			minMemoryGiB:             32,
			maxMemoryGiB:             7463,
			minProcessorSharedCapped: 0.5,
			minProcessorDedicated:    1,
			maxProcessor:             143,
		},
		"e980": {
			minMemoryGiB:             32,
			maxMemoryGiB:             15307,
			minProcessorSharedCapped: 0.5,
			minProcessorDedicated:    1,
			maxProcessor:             143,
		},
	}
)

const (
	arch              = machineArch(goruntime.GOARCH)
	ARM64 machineArch = "arm64"
	AMD64 machineArch = "amd64"

	defaultUserDataSecret  = "worker-user-data"
	defaultSecretNamespace = "openshift-machine-api"

	// AWS Defaults
	defaultAWSCredentialsSecret = "aws-cloud-credentials"
	defaultAWSX86InstanceType   = "m5.large"
	defaultAWSARMInstanceType   = "m6g.large"

	// Azure Defaults
	defaultAzureX86VMSize         = "Standard_D4s_V3"
	defaultAzureARMVMSize         = "Standard_D4ps_V5"
	defaultAzureCredentialsSecret = "azure-cloud-credentials"
	defaultAzureOSDiskOSType      = "Linux"
	defaultAzureOSDiskStorageType = "Premium_LRS"

	// Azure OSDisk constants
	azureMaxDiskSizeGB                 = 32768
	azureEphemeralStorageLocationLocal = "Local"
	azureCachingTypeNone               = "None"
	azureCachingTypeReadOnly           = "ReadOnly"
	azureCachingTypeReadWrite          = "ReadWrite"
	azureRHCOSVersion                  = "latest" // The installer only sets up one version but its name may vary, using latest will pull it no matter the name.

	// GCP Defaults
	defaultGCPX86MachineType    = "n1-standard-4"
	defaultGCPARMMachineType    = "t2a-standard-4"
	defaultGCPCredentialsSecret = "gcp-cloud-credentials"
	defaultGCPDiskSizeGb        = 128
	defaultGCPDiskType          = "pd-standard"
	// https://releases-rhcos-art.apps.ocp-virt.prod.psi.redhat.com/?stream=prod/streams/4.14-9.2&release=414.92.202307070025-0&arch=x86_64#414.92.202307070025-0
	// https://github.com/openshift/installer/commit/0cec4e1403d78387729f21f04d0f764f63fc552e
	defaultGCPX86DiskImage = "projects/rhcos-cloud/global/images/rhcos-414-92-202307070025-0-gcp-x86-64"
	defaultGCPARMDiskImage = "projects/rhcos-cloud/global/images/rhcos-414-92-202307070025-0-gcp-aarch64"
	defaultGCPGPUCount     = 1

	// vSphere Defaults
	defaultVSphereCredentialsSecret = "vsphere-cloud-credentials"
	// Minimum vSphere values taken from vSphere reconciler
	minVSphereCPU       = 2
	minVSphereMemoryMiB = 2048
	// https://docs.openshift.com/container-platform/4.1/installing/installing_vsphere/installing-vsphere.html#minimum-resource-requirements_installing-vsphere
	minVSphereDiskGiB = 120

	// Nutanix Defaults
	// Minimum Nutanix values taken from Nutanix reconciler
	defaultNutanixCredentialsSecret = "nutanix-credentials"
	minNutanixCPUSockets            = 1
	minNutanixCPUPerSocket          = 1
	minNutanixMemoryMiB             = 2048
	minNutanixDiskGiB               = 20

	// PowerVS Defaults
	defaultPowerVSCredentialsSecret = "powervs-credentials"
	defaultPowerVSSysType           = "s922"
	defaultPowerVSProcType          = "Shared"
	defaultPowerVSProcessor         = "0.5"
	defaultPowerVSMemory            = 32
	powerVSServiceInstance          = "serviceInstance"
	powerVSNetwork                  = "network"
	powerVSImage                    = "image"
	powerVSSystemTypeE880           = "e880"
	powerVSSystemTypeE980           = "e980"
	azureProviderIDPrefix           = "azure://"
	azureProvidersKey               = "providers"
	azureSubscriptionsKey           = "subscriptions"
	azureResourceGroupsLowerKey     = "resourcegroups"
	azureLocationsKey               = "locations"
	azureBuiltInResourceNamespace   = "Microsoft.Resources"
)

// GCP Confidential VM supports Compute Engine machine types in the following series:
// reference: https://cloud.google.com/compute/confidential-vm/docs/os-and-machine-type#machine-type
var gcpConfidentialComputeSupportedMachineSeries = []string{"n2d", "c2d"}

// defaultInstanceTypeForCloudProvider returns the default instance type for the given cloud provider and architecture.
// If the cloud provider is not supported, an empty string is returned.
// If the architecture is not supported, the default instance type for AMD64 is returned as a fallback.
// The function also takes a pointer to a slice of strings to append warnings to.
func defaultInstanceTypeForCloudProvider(cloudProvider osconfigv1.PlatformType, arch machineArch, warnings *[]string) string {
	cloudProviderArchMachineTypes := map[osconfigv1.PlatformType]map[machineArch]string{
		osconfigv1.AWSPlatformType: {
			AMD64: defaultAWSX86InstanceType,
			ARM64: defaultAWSARMInstanceType,
		},
		osconfigv1.AzurePlatformType: {
			AMD64: defaultAzureX86VMSize,
			ARM64: defaultAzureARMVMSize,
		},
		osconfigv1.GCPPlatformType: {
			AMD64: defaultGCPX86MachineType,
			ARM64: defaultGCPARMMachineType,
		},
	}
	if cloudProviderMap, ok := cloudProviderArchMachineTypes[cloudProvider]; ok {
		if instanceType, ok := cloudProviderArchMachineTypes[cloudProvider][arch]; ok {
			*warnings = append(*warnings, fmt.Sprintf("setting the default instance type %q "+
				"for cloud provider %q, based on the control plane architecture (%q)", instanceType, cloudProvider, arch))
			return instanceType
		}
		// If the arch is not supported, return the default for AMD64.
		warning := fmt.Sprintf("no default instance type found for provider %q, arch %q. "+
			"Defaulting to the amd64 one: %q", cloudProvider, arch, cloudProviderMap[AMD64])
		*warnings = append(*warnings, warning)
		klog.Warningln(warning)
		return cloudProviderMap[AMD64]
	}
	// If the cloud provider is not supported, return an empty string.
	klog.Errorf("no default instance types found for cloud provider %q", cloudProvider)
	return ""
}

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

func credentialsSecretExists(c client.Client, name, namespace string) []string {
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

func getDNS() (*osconfigv1.DNS, error) {
	cfg, err := ctrl.GetConfig()
	if err != nil {
		return nil, err
	}
	client, err := osclientset.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dns, err := client.ConfigV1().DNSes().Get(context.Background(), "cluster", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return dns, nil
}

type machineAdmissionFn func(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList)

type admissionConfig struct {
	clusterID       string
	platformStatus  *osconfigv1.PlatformStatus
	dnsDisconnected bool
	client          client.Client
}

type admissionHandler struct {
	*admissionConfig
	webhookOperations machineAdmissionFn
	decoder           *admission.Decoder
}

// InjectDecoder injects the decoder.
func (a *admissionHandler) InjectDecoder(d *admission.Decoder) error {
	a.decoder = d
	return nil
}

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

// NewValidator returns a new machineValidatorHandler.
func NewMachineValidator(client client.Client) (*admission.Webhook, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	dns, err := getDNS()
	if err != nil {
		return nil, err
	}

	return admission.WithCustomValidator(scheme.Scheme, &machinev1beta1.Machine{}, createMachineValidator(infra, client, dns)), nil
}

func createMachineValidator(infra *osconfigv1.Infrastructure, client client.Client, dns *osconfigv1.DNS) *machineValidatorHandler {
	admissionConfig := &admissionConfig{
		dnsDisconnected: dns.Spec.PublicZone == nil,
		clusterID:       infra.Status.InfrastructureName,
		platformStatus:  infra.Status.PlatformStatus,
		client:          client,
	}
	return &machineValidatorHandler{
		admissionHandler: &admissionHandler{
			admissionConfig:   admissionConfig,
			webhookOperations: getMachineValidatorOperation(infra.Status.PlatformStatus.Type),
		},
	}
}

func getMachineValidatorOperation(platform osconfigv1.PlatformType) machineAdmissionFn {
	switch platform {
	case osconfigv1.AWSPlatformType:
		return validateAWS
	case osconfigv1.AzurePlatformType:
		return validateAzure
	case osconfigv1.GCPPlatformType:
		return validateGCP
	case osconfigv1.VSpherePlatformType:
		return validateVSphere
	case osconfigv1.PowerVSPlatformType:
		return validatePowerVS
	case osconfigv1.NutanixPlatformType:
		return validateNutanix
	default:
		// just no-op
		return func(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
			return true, []string{}, nil
		}
	}
}

// NewDefaulter returns a new machineDefaulterHandler.
func NewMachineDefaulter() (*admission.Webhook, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return admission.WithCustomDefaulter(scheme.Scheme, &machinev1beta1.Machine{}, createMachineDefaulter(infra.Status.PlatformStatus, infra.Status.InfrastructureName)), nil
}

func createMachineDefaulter(platformStatus *osconfigv1.PlatformStatus, clusterID string) *machineDefaulterHandler {
	return &machineDefaulterHandler{
		admissionHandler: &admissionHandler{
			admissionConfig:   &admissionConfig{clusterID: clusterID},
			webhookOperations: getMachineDefaulterOperation(platformStatus),
		},
	}
}

func getMachineDefaulterOperation(platformStatus *osconfigv1.PlatformStatus) machineAdmissionFn {
	switch platformStatus.Type {
	case osconfigv1.AWSPlatformType:
		region := ""
		if platformStatus.AWS != nil {
			region = platformStatus.AWS.Region
		}
		return awsDefaulter{region: region}.defaultAWS
	case osconfigv1.AzurePlatformType:
		return defaultAzure
	case osconfigv1.GCPPlatformType:
		return defaultGCP
	case osconfigv1.VSpherePlatformType:
		return defaultVSphere
	case osconfigv1.PowerVSPlatformType:
		return defaultPowerVS
	case osconfigv1.NutanixPlatformType:
		return defaultNutanix
	default:
		// just no-op
		return func(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
			return true, []string{}, nil
		}
	}
}

func (h *machineValidatorHandler) validateMachine(m, oldM *machinev1beta1.Machine) (bool, []string, field.ErrorList) {
	// Skip validation if we just remove the finalizer.
	// For more information: https://issues.redhat.com/browse/OCPCLOUD-1426
	if !m.DeletionTimestamp.IsZero() {
		isFinalizerOnly, err := isFinalizerOnlyRemoval(m, oldM)
		if err != nil {
			return false, nil, field.ErrorList{field.InternalError(field.NewPath(""), err)}
		}
		if isFinalizerOnly {
			return true, nil, nil
		}
	}

	errs := validateMachineLifecycleHooks(m, oldM)

	ok, warnings, opErrs := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		errs = append(errs, opErrs...)
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineValidatorHandler) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	m, ok := obj.(*machinev1beta1.Machine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a Machine but got a %T", obj))
	}

	klog.V(3).Infof("Validate webhook called for Machine: %s", m.GetName())

	ok, warnings, errs := h.validateMachine(m, nil)
	if !ok {
		return warnings, errs.ToAggregate()
	}

	return warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineValidatorHandler) ValidateUpdate(ctx context.Context, oldObj, obj runtime.Object) (admission.Warnings, error) {
	m, ok := obj.(*machinev1beta1.Machine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a Machine but got a %T", obj))
	}

	mOld, ok := oldObj.(*machinev1beta1.Machine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a Machine but got a %T", oldObj))
	}

	klog.V(3).Infof("Validate webhook called for Machine: %s", m.GetName())

	ok, warnings, errs := h.validateMachine(m, mOld)
	if !ok {
		return warnings, errs.ToAggregate()
	}

	return warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineValidatorHandler) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	m, ok := obj.(*machinev1beta1.Machine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a Machine but got a %T", obj))
	}

	klog.V(3).Infof("Validate webhook called for Machine: %s", m.GetName())

	ok, warnings, errs := h.validateMachine(m, nil)
	if !ok {
		return warnings, errs.ToAggregate()
	}

	return warnings, nil
}

// Handle handles HTTP requests for admission webhook servers.
func (h *machineDefaulterHandler) Default(ctx context.Context, obj runtime.Object) error {
	m, ok := obj.(*machinev1beta1.Machine)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a MachineSet but got a %T", obj))
	}

	klog.V(3).Infof("Mutate webhook called for Machine: %s", m.GetName())

	// Only enforce the clusterID if it's not set.
	// Otherwise a discrepancy on the value would leave the machine orphan
	// and would trigger a new machine creation by the machineSet.
	// https://bugzilla.redhat.com/show_bug.cgi?id=1857175
	if m.Labels == nil {
		m.Labels = make(map[string]string)
	}
	if _, ok := m.Labels[machinev1beta1.MachineClusterIDLabel]; !ok {
		m.Labels[machinev1beta1.MachineClusterIDLabel] = h.clusterID
	}

	ok, _, errs := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		return errs.ToAggregate()
	}

	return nil
}

type awsDefaulter struct {
	region string
}

func (a awsDefaulter) defaultAWS(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Defaulting AWS providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.AWSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if providerSpec.InstanceType == "" {
		providerSpec.InstanceType = defaultInstanceTypeForCloudProvider(osconfigv1.AWSPlatformType, arch, &warnings)
	}

	if providerSpec.InstanceType == "" {
		// this should never happen
		errs = append(errs, field.Required(field.NewPath("instanceType"), "instanceType is required and no "+
			"default value was found"))
	}

	if providerSpec.Placement.Region == "" {
		providerSpec.Placement.Region = a.region
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: defaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret}
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "value"), err))
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func unmarshalInto(m *machinev1beta1.Machine, providerSpec interface{}) *field.Error {
	if m.Spec.ProviderSpec.Value == nil {
		return field.Required(field.NewPath("providerSpec", "value"), "a value must be provided")
	}

	if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &providerSpec); err != nil {
		return field.Invalid(field.NewPath("providerSpec", "value"), providerSpec, err.Error())
	}
	return nil
}

func validateUnknownFields(m *machinev1beta1.Machine, providerSpec interface{}) error {
	if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &providerSpec, yaml.DisallowUnknownFields); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			unknownField := strings.Replace(strings.Split(err.Error(), "unknown field ")[1], "\"", "", -1)
			return &field.Error{
				Type:     field.ErrorTypeNotSupported,
				Field:    field.NewPath("providerSpec", "value").String(),
				BadValue: unknownField,
				Detail:   fmt.Sprintf("Unknown field (%s) will be ignored", unknownField),
			}
		}
	}
	return nil
}

func validateAWS(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Validating AWS providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.AWSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if err := validateUnknownFields(m, providerSpec); err != nil {
		warnings = append(warnings, err.Error())
	}

	if !validateGVK(providerSpec.GroupVersionKind(), osconfigv1.AWSPlatformType) {
		warnings = append(warnings, fmt.Sprintf("incorrect GroupVersionKind for AWSMachineProviderConfig object: %s", providerSpec.GroupVersionKind()))
	}

	if providerSpec.AMI.ID == nil {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "ami"),
				"expected providerSpec.ami.id to be populated",
			),
		)
	}

	if providerSpec.AMI.ARN != nil {
		warnings = append(
			warnings,
			"can't use providerSpec.ami.arn, only providerSpec.ami.id can be used to reference AMI",
		)
	}

	if providerSpec.AMI.Filters != nil {
		warnings = append(
			warnings,
			"can't use providerSpec.ami.filters, only providerSpec.ami.id can be used to reference AMI",
		)
	}

	if providerSpec.Placement.Region == "" {
		errs = append(
			errs,
			field.Required(
				field.NewPath("providerSpec", "placement", "region"),
				"expected providerSpec.placement.region to be populated",
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
	} else {
		warnings = append(warnings, credentialsSecretExists(config.client, providerSpec.CredentialsSecret.Name, m.GetNamespace())...)
	}

	if providerSpec.Subnet.ARN == nil && providerSpec.Subnet.ID == nil && providerSpec.Subnet.Filters == nil {
		warnings = append(
			warnings,
			"providerSpec.subnet: No subnet has been provided. Instances may be created in an unexpected subnet and may not join the cluster.",
		)
	}

	if providerSpec.IAMInstanceProfile == nil {
		warnings = append(warnings, "providerSpec.iamInstanceProfile: no IAM instance profile provided: nodes may be unable to join the cluster")
	}

	// TODO(alberto): Validate providerSpec.BlockDevices.
	// https://github.com/openshift/cluster-api-provider-aws/pull/299#discussion_r433920532

	switch providerSpec.Placement.Tenancy {
	case "", machinev1beta1.DefaultTenancy, machinev1beta1.DedicatedTenancy, machinev1beta1.HostTenancy:
		// Do nothing, valid values
	default:
		errs = append(
			errs,
			field.Invalid(
				field.NewPath("providerSpec", "tenancy"),
				providerSpec.Placement.Tenancy,
				fmt.Sprintf("Invalid providerSpec.tenancy, the only allowed options are: %s, %s, %s", machinev1beta1.DefaultTenancy, machinev1beta1.DedicatedTenancy, machinev1beta1.HostTenancy),
			),
		)
	}

	duplicatedTags := getDuplicatedTags(providerSpec.Tags)
	if len(duplicatedTags) > 0 {
		warnings = append(warnings, fmt.Sprintf("providerSpec.tags: duplicated tag names (%s): only the first value will be used.", strings.Join(duplicatedTags, ",")))
	}

	switch providerSpec.NetworkInterfaceType {
	case "", machinev1beta1.AWSENANetworkInterfaceType, machinev1beta1.AWSEFANetworkInterfaceType:
		// Do nothing, valid values
	default:
		errs = append(
			errs,
			field.Invalid(
				field.NewPath("providerSpec", "networkInterfaceType"),
				providerSpec.NetworkInterfaceType,
				fmt.Sprintf("Valid values are: %s, %s and omitted", machinev1beta1.AWSENANetworkInterfaceType, machinev1beta1.AWSEFANetworkInterfaceType),
			),
		)
	}

	switch providerSpec.MetadataServiceOptions.Authentication {
	case "", machinev1beta1.MetadataServiceAuthenticationOptional, machinev1beta1.MetadataServiceAuthenticationRequired:
		// Valid values
	default:
		errs = append(
			errs,
			field.Invalid(
				field.NewPath("providerSpec", "metadataServiceOptions", "authentication"),
				providerSpec.MetadataServiceOptions.Authentication,
				fmt.Sprintf("Allowed values are either '%s' or '%s'", machinev1beta1.MetadataServiceAuthenticationOptional, machinev1beta1.MetadataServiceAuthenticationRequired),
			),
		)
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	return true, warnings, nil
}

// getDuplicatedTags iterates through the AWS TagSpecifications
// to determine if any tag Name is duplicated within the list.
// A list of duplicated names will be returned.
func getDuplicatedTags(tagSpecs []machinev1beta1.TagSpecification) []string {
	tagNames := map[string]int{}
	duplicatedTags := []string{}
	for _, spec := range tagSpecs {
		tagNames[spec.Name] += 1
		// Only append the duplicated tag on the second occurrence to prevent it
		// being listed multiple times when there are more than 2 occurrences.
		if tagNames[spec.Name] == 2 {
			duplicatedTags = append(duplicatedTags, spec.Name)
		}
	}
	return duplicatedTags
}

func defaultAzure(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Defaulting Azure providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.AzureMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if providerSpec.VMSize == "" {
		providerSpec.VMSize = defaultInstanceTypeForCloudProvider(osconfigv1.AzurePlatformType, arch, &warnings)
	}

	if providerSpec.VMSize == "" {
		// this should never happen
		errs = append(errs, field.Required(field.NewPath("vmSize"), "vmSize is required and no "+
			"default value was found"))
	}

	// Vnet and Subnet need to be provided together by the user
	if providerSpec.Vnet == "" && providerSpec.Subnet == "" {
		providerSpec.Vnet = defaultAzureVnet(config.clusterID)
		providerSpec.Subnet = defaultAzureSubnet(config.clusterID)
	}

	if providerSpec.Image == (machinev1beta1.Image{}) {
		providerSpec.Image.ResourceID = defaultAzureImageResourceID(config.clusterID)
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.SecretReference{Name: defaultUserDataSecret}
	} else if providerSpec.UserDataSecret.Name == "" {
		providerSpec.UserDataSecret.Name = defaultUserDataSecret
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

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "value"), err))
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func validateAzure(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Validating Azure providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.AzureMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if err := validateUnknownFields(m, providerSpec); err != nil {
		warnings = append(warnings, err.Error())
	}

	if !validateGVK(providerSpec.GroupVersionKind(), osconfigv1.AzurePlatformType) {
		warnings = append(warnings, fmt.Sprintf("incorrect GroupVersionKind for AzureMachineProviderSpec object: %s", providerSpec.GroupVersionKind()))
	}

	if providerSpec.VMSize == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "vmSize"), "vmSize should be set to one of the supported Azure VM sizes"))
	}

	if providerSpec.PublicIP && config.dnsDisconnected {
		errs = append(errs, field.Forbidden(field.NewPath("providerSpec", "publicIP"), "publicIP is not allowed in Azure disconnected installation with publish strategy as internal"))
	}
	// Vnet requires Subnet
	if providerSpec.Vnet != "" && providerSpec.Subnet == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "subnet"), "must provide a subnet when a virtual network is specified"))
	}

	// Subnet requires Vnet
	if providerSpec.Subnet != "" && providerSpec.Vnet == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "vnet"), "must provide a virtual network when supplying subnets"))
	}

	errs = append(errs, validateAzureImage(providerSpec.Image)...)

	if providerSpec.UserDataSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret"), "userDataSecret must be provided"))
	} else if providerSpec.UserDataSecret.Name == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret", "name"), "name must be provided"))
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
		if providerSpec.CredentialsSecret.Name != "" && providerSpec.CredentialsSecret.Namespace != "" {
			warnings = append(warnings, credentialsSecretExists(config.client, providerSpec.CredentialsSecret.Name, providerSpec.CredentialsSecret.Namespace)...)
		}
	}

	if providerSpec.OSDisk.DiskSizeGB <= 0 || providerSpec.OSDisk.DiskSizeGB >= azureMaxDiskSizeGB {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "osDisk", "diskSizeGB"), providerSpec.OSDisk.DiskSizeGB, "diskSizeGB must be greater than zero and less than 32768"))
	}

	if providerSpec.OSDisk.DiskSettings.EphemeralStorageLocation != azureEphemeralStorageLocationLocal && providerSpec.OSDisk.DiskSettings.EphemeralStorageLocation != "" {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "osDisk", "diskSettings", "ephemeralStorageLocation"), providerSpec.OSDisk.DiskSettings.EphemeralStorageLocation,
			fmt.Sprintf("osDisk.diskSettings.ephemeralStorageLocation can either be omitted or set to %s", azureEphemeralStorageLocationLocal)))
	}

	switch providerSpec.OSDisk.CachingType {
	case azureCachingTypeNone, azureCachingTypeReadOnly, azureCachingTypeReadWrite, "":
		// Valid scenarios, do nothing
	default:
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "osDisk", "cachingType"), providerSpec.OSDisk.CachingType,
			fmt.Sprintf("osDisk.cachingType can be only %s, %s, %s or omitted", azureCachingTypeNone, azureCachingTypeReadOnly, azureCachingTypeReadWrite)))
	}

	if providerSpec.OSDisk.DiskSettings.EphemeralStorageLocation == azureEphemeralStorageLocationLocal && providerSpec.OSDisk.CachingType != azureCachingTypeReadOnly {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "osDisk", "cachingType"), providerSpec.OSDisk.CachingType, "Instances using an ephemeral OS disk support only Readonly caching"))
	}
	if providerSpec.CapacityReservationGroupID != "" {
		err := validateAzureCapacityReservationGroupID(providerSpec.CapacityReservationGroupID)
		if err != nil {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "capacityReservationGroupID"), providerSpec.CapacityReservationGroupID, err.Error()))
		}
	}
	switch providerSpec.UltraSSDCapability {
	case machinev1beta1.AzureUltraSSDCapabilityEnabled, machinev1beta1.AzureUltraSSDCapabilityDisabled, "":
		// Valid scenarios, do nothing
	default:
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "ultraSSDCapability"), providerSpec.UltraSSDCapability,
			fmt.Sprintf("ultraSSDCapability can be only %s, %s or omitted", machinev1beta1.AzureUltraSSDCapabilityEnabled, machinev1beta1.AzureUltraSSDCapabilityDisabled)))
	}

	errs = append(errs, validateAzureSecurityProfile(m.Name, providerSpec, field.NewPath("providerSpec", "securityProfile"))...)

	errs = append(errs, validateAzureDataDisks(m.Name, providerSpec, field.NewPath("providerSpec", "dataDisks"))...)

	errs = append(errs, validateAzureDiagnostics(providerSpec.Diagnostics, field.NewPath("providerSpec", "diagnostics"))...)

	if isAzureGovCloud(config.platformStatus) && providerSpec.SpotVMOptions != nil {
		warnings = append(warnings, "spot VMs may not be supported when using GovCloud region")
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

func validateAzureImage(image machinev1beta1.Image) field.ErrorList {
	var errs field.ErrorList
	if image == (machinev1beta1.Image{}) {
		return append(errs, field.Required(field.NewPath("providerSpec", "image"), "an image reference must be provided"))
	}

	if image.ResourceID != "" {
		if image != (machinev1beta1.Image{ResourceID: image.ResourceID}) {
			return append(errs, field.Required(field.NewPath("providerSpec", "image", "resourceID"), "resourceID is already specified, other fields such as [Offer, Publisher, SKU, Version] should not be set"))
		}
		return errs
	}

	// Resource ID not provided, so Offer, Publisher, SKU and Version are required
	if image.Offer == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "image", "Offer"), "Offer must be provided"))
	}
	if image.Publisher == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "image", "Publisher"), "Publisher must be provided"))
	}
	if image.SKU == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "image", "SKU"), "SKU must be provided"))
	}
	if image.Version == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "image", "Version"), "Version must be provided"))
	}

	return errs
}

func validateAzureDiagnostics(diagnosticsSpec machinev1beta1.AzureDiagnostics, parentPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if diagnosticsSpec.Boot != nil {
		cmPath := parentPath.Child("boot", "customerManaged")

		switch diagnosticsSpec.Boot.StorageAccountType {
		case machinev1beta1.CustomerManagedAzureDiagnosticsStorage:
			if diagnosticsSpec.Boot.CustomerManaged == nil {
				errs = append(errs, field.Required(cmPath, "customerManaged configuration must be provided"))
			} else if diagnosticsSpec.Boot.CustomerManaged.StorageAccountURI == "" {
				errs = append(errs, field.Required(cmPath.Child("storageAccountURI"), "storageAccountURI must be provided"))
			}

		case machinev1beta1.AzureManagedAzureDiagnosticsStorage:
			if diagnosticsSpec.Boot.CustomerManaged != nil {
				errs = append(errs, field.Invalid(cmPath, diagnosticsSpec.Boot.CustomerManaged, "customerManaged may not be set when type is AzureManaged"))
			}

		default:
			errs = append(errs, field.Invalid(parentPath.Child("boot", "storageAccountType"), diagnosticsSpec.Boot.StorageAccountType, fmt.Sprintf("storageAccountType must be one of: %s, %s", machinev1beta1.AzureManagedAzureDiagnosticsStorage, machinev1beta1.CustomerManagedAzureDiagnosticsStorage)))
		}
	}

	return errs
}

func defaultGCP(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Defaulting GCP providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.GCPMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if providerSpec.MachineType == "" {
		providerSpec.MachineType = defaultInstanceTypeForCloudProvider(osconfigv1.GCPPlatformType, arch, &warnings)
	}

	if providerSpec.MachineType == "" {
		// this should never happen
		errs = append(errs, field.Required(field.NewPath("providerSpec", "machineType"), "machineType "+
			"is required and no default value was found"))
	}

	if len(providerSpec.NetworkInterfaces) == 0 {
		providerSpec.NetworkInterfaces = append(providerSpec.NetworkInterfaces, &machinev1beta1.GCPNetworkInterface{
			Network:    defaultGCPNetwork(config.clusterID),
			Subnetwork: defaultGCPSubnetwork(config.clusterID),
		})
	}

	providerSpec.Disks = defaultGCPDisks(providerSpec.Disks, config.clusterID)

	if len(providerSpec.GPUs) != 0 {
		// In case Count was not set it should default to 1, since there is no valid reason for it to be purposely set to 0.
		if providerSpec.GPUs[0].Count == 0 {
			providerSpec.GPUs[0].Count = defaultGCPGPUCount
		}
	}

	if len(providerSpec.Tags) == 0 {
		providerSpec.Tags = defaultGCPTags(config.clusterID)
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: defaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultGCPCredentialsSecret}
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "value"), err))
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func defaultGCPDisks(disks []*machinev1beta1.GCPDisk, clusterID string) []*machinev1beta1.GCPDisk {
	if len(disks) == 0 {
		return []*machinev1beta1.GCPDisk{
			{
				AutoDelete: true,
				Boot:       true,
				SizeGB:     defaultGCPDiskSizeGb,
				Type:       defaultGCPDiskType,
				Image:      defaultGCPDiskImage(),
			},
		}
	}

	for _, disk := range disks {
		if disk.Type == "" {
			disk.Type = defaultGCPDiskType
		}

		if disk.Image == "" {
			disk.Image = defaultGCPDiskImage()
		}
	}

	return disks
}

func validateGCP(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Validating GCP providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.GCPMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if err := validateUnknownFields(m, providerSpec); err != nil {
		warnings = append(warnings, err.Error())
	}

	if !validateGVK(providerSpec.GroupVersionKind(), osconfigv1.GCPPlatformType) {
		warnings = append(warnings, fmt.Sprintf("incorrect GroupVersionKind for GCPMachineProviderSpec object: %s", providerSpec.GroupVersionKind()))
	}

	if providerSpec.Region == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "region"), "region is required"))
	}

	if !strings.HasPrefix(providerSpec.Zone, providerSpec.Region) {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "zone"), providerSpec.Zone, fmt.Sprintf("zone not in configured region (%s)", providerSpec.Region)))
	}

	if providerSpec.MachineType == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "machineType"), "machineType should be set to one of the supported GCP machine types"))
	}

	if providerSpec.OnHostMaintenance != "" && providerSpec.OnHostMaintenance != machinev1beta1.MigrateHostMaintenanceType && providerSpec.OnHostMaintenance != machinev1beta1.TerminateHostMaintenanceType {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "onHostMaintenance"), providerSpec.OnHostMaintenance, fmt.Sprintf("onHostMaintenance must be either %s or %s.", machinev1beta1.MigrateHostMaintenanceType, machinev1beta1.TerminateHostMaintenanceType)))
	}

	errs = append(errs, validateShieldedInstanceConfig(providerSpec)...)

	errs = append(errs, validateGCPConfidentialComputing(providerSpec)...)

	if providerSpec.RestartPolicy != "" && providerSpec.RestartPolicy != machinev1beta1.RestartPolicyAlways && providerSpec.RestartPolicy != machinev1beta1.RestartPolicyNever {
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "restartPolicy"), providerSpec.RestartPolicy, fmt.Sprintf("restartPolicy must be either %s or %s.", machinev1beta1.RestartPolicyNever, machinev1beta1.RestartPolicyAlways)))
	}

	if len(providerSpec.GPUs) != 0 || strings.HasPrefix(providerSpec.MachineType, "a2-") {
		if providerSpec.OnHostMaintenance == machinev1beta1.MigrateHostMaintenanceType {
			errs = append(errs, field.Forbidden(field.NewPath("providerSpec", "onHostMaintenance"), fmt.Sprintf("When GPUs are specified or using machineType with pre-attached GPUs(A2 machine family), onHostMaintenance must be set to %s.", machinev1beta1.TerminateHostMaintenanceType)))
		}
	}

	errs = append(errs, validateGCPNetworkInterfaces(providerSpec.NetworkInterfaces, field.NewPath("providerSpec", "networkInterfaces"))...)
	errs = append(errs, validateGCPDisks(providerSpec.Disks, field.NewPath("providerSpec", "disks"))...)
	errs = append(errs, validateGCPGPUs(providerSpec.GPUs, field.NewPath("providerSpec", "gpus"), providerSpec.MachineType)...)

	if len(providerSpec.ServiceAccounts) == 0 {
		warnings = append(warnings, "providerSpec.serviceAccounts: no service account provided: nodes may be unable to join the cluster")
	} else {
		errs = append(errs, validateGCPServiceAccounts(providerSpec.ServiceAccounts, field.NewPath("providerSpec", "serviceAccounts"))...)
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
			warnings = append(warnings, credentialsSecretExists(config.client, providerSpec.CredentialsSecret.Name, m.GetNamespace())...)
		}
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

func validateShieldedInstanceConfig(providerSpec *machinev1beta1.GCPMachineProviderSpec) field.ErrorList {
	var errs field.ErrorList

	if providerSpec.ShieldedInstanceConfig != (machinev1beta1.GCPShieldedInstanceConfig{}) {

		if providerSpec.ShieldedInstanceConfig.SecureBoot != "" && providerSpec.ShieldedInstanceConfig.SecureBoot != machinev1beta1.SecureBootPolicyEnabled && providerSpec.ShieldedInstanceConfig.SecureBoot != machinev1beta1.SecureBootPolicyDisabled {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "shieldedInstanceConfig", "secureBoot"),
				providerSpec.ShieldedInstanceConfig.SecureBoot,
				fmt.Sprintf("secureBoot must be either %s or %s.", machinev1beta1.SecureBootPolicyEnabled, machinev1beta1.SecureBootPolicyDisabled)))
		}

		if providerSpec.ShieldedInstanceConfig.IntegrityMonitoring != "" && providerSpec.ShieldedInstanceConfig.IntegrityMonitoring != machinev1beta1.IntegrityMonitoringPolicyEnabled && providerSpec.ShieldedInstanceConfig.IntegrityMonitoring != machinev1beta1.IntegrityMonitoringPolicyDisabled {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "shieldedInstanceConfig", "integrityMonitoring"),
				providerSpec.ShieldedInstanceConfig.IntegrityMonitoring,
				fmt.Sprintf("integrityMonitoring must be either %s or %s.", machinev1beta1.IntegrityMonitoringPolicyEnabled, machinev1beta1.IntegrityMonitoringPolicyDisabled)))
		}

		if providerSpec.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule != "" && providerSpec.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule != machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled && providerSpec.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule != machinev1beta1.VirtualizedTrustedPlatformModulePolicyDisabled {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "shieldedInstanceConfig", "virtualizedTrustedPlatformModule"),
				providerSpec.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule,
				fmt.Sprintf("virtualizedTrustedPlatformModule must be either %s or %s.", machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled, machinev1beta1.VirtualizedTrustedPlatformModulePolicyDisabled)))
		}
		if providerSpec.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule == machinev1beta1.VirtualizedTrustedPlatformModulePolicyDisabled && providerSpec.ShieldedInstanceConfig.IntegrityMonitoring != machinev1beta1.IntegrityMonitoringPolicyDisabled {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "shieldedInstanceConfig", "virtualizedTrustedPlatformModule"),
				providerSpec.ShieldedInstanceConfig.VirtualizedTrustedPlatformModule,
				fmt.Sprintf("integrityMonitoring requires virtualizedTrustedPlatformModule %s.", machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled)))
		}
	}
	return errs
}

func validateGCPConfidentialComputing(providerSpec *machinev1beta1.GCPMachineProviderSpec) field.ErrorList {
	var errs field.ErrorList

	switch providerSpec.ConfidentialCompute {
	case machinev1beta1.ConfidentialComputePolicyEnabled:
		// Check on host maintenance
		if providerSpec.OnHostMaintenance != machinev1beta1.TerminateHostMaintenanceType {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "onHostMaintenance"),
				providerSpec.OnHostMaintenance,
				fmt.Sprintf("ConfidentialCompute require OnHostMaintenance to be set to %s, the current value is: %s", machinev1beta1.TerminateHostMaintenanceType, providerSpec.OnHostMaintenance)))
		}
		// Check machine series supports confidential computing
		machineSeries := strings.Split(providerSpec.MachineType, "-")[0]
		if !slices.Contains(gcpConfidentialComputeSupportedMachineSeries, machineSeries) {
			errs = append(errs, field.Invalid(field.NewPath("providerSpec", "machineType"),
				providerSpec.MachineType,
				fmt.Sprintf("ConfidentialCompute require machine type in the following series: %s", strings.Join(gcpConfidentialComputeSupportedMachineSeries, `,`))),
			)
		}
	case machinev1beta1.ConfidentialComputePolicyDisabled, "":
	default:
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "confidentialCompute"),
			providerSpec.ConfidentialCompute,
			fmt.Sprintf("ConfidentialCompute must be either %s or %s.", machinev1beta1.ConfidentialComputePolicyEnabled, machinev1beta1.ConfidentialComputePolicyDisabled)))
	}

	return errs
}

func validateGCPNetworkInterfaces(networkInterfaces []*machinev1beta1.GCPNetworkInterface, parentPath *field.Path) field.ErrorList {
	if len(networkInterfaces) == 0 {
		return field.ErrorList{field.Required(parentPath, "at least 1 network interface is required")}
	}

	var errs field.ErrorList
	for i, ni := range networkInterfaces {
		fldPath := parentPath.Index(i)

		if ni.Network == "" {
			errs = append(errs, field.Required(fldPath.Child("network"), "network is required"))
		}

		if ni.Subnetwork == "" {
			errs = append(errs, field.Required(fldPath.Child("subnetwork"), "subnetwork is required"))
		}
	}

	return errs
}

func validateGCPDisks(disks []*machinev1beta1.GCPDisk, parentPath *field.Path) field.ErrorList {
	if len(disks) == 0 {
		return field.ErrorList{field.Required(parentPath, "at least 1 disk is required")}
	}

	var errs field.ErrorList
	for i, disk := range disks {
		fldPath := parentPath.Index(i)

		if disk.SizeGB != 0 {
			if disk.SizeGB < 16 {
				errs = append(errs, field.Invalid(fldPath.Child("sizeGb"), disk.SizeGB, "must be at least 16GB in size"))
			} else if disk.SizeGB > 65536 {
				errs = append(errs, field.Invalid(fldPath.Child("sizeGb"), disk.SizeGB, "exceeding maximum GCP disk size limit, must be below 65536"))
			}
		}

		if disk.Type != "" {
			diskTypes := sets.NewString("pd-standard", "pd-ssd", "pd-balanced")
			if !diskTypes.Has(disk.Type) {
				errs = append(errs, field.NotSupported(fldPath.Child("type"), disk.Type, diskTypes.List()))
			}
		}
	}

	return errs
}

func validateGCPGPUs(guestAccelerators []machinev1beta1.GCPGPUConfig, parentPath *field.Path, machineType string) field.ErrorList {
	var errs field.ErrorList

	if len(guestAccelerators) > 1 {
		errs = append(errs, field.TooMany(parentPath, len(guestAccelerators), 1))
	} else if len(guestAccelerators) == 1 {
		accelerator := guestAccelerators[0]
		if accelerator.Type == "" {
			errs = append(errs, field.Required(parentPath.Child("Type"), "Type is required"))
		}

		if accelerator.Type == "nvidia-tesla-a100" {
			errs = append(errs, field.Invalid(parentPath.Child("Type"), accelerator.Type, " nvidia-tesla-a100 gpus, are only attached to the A2 machine types"))
		}

		if strings.HasPrefix(machineType, "a2-") {
			errs = append(errs, field.Invalid(parentPath, accelerator.Type, "A2 machine types have already attached gpus, additional gpus cannot be specified"))
		}
	}

	return errs
}

func validateGCPServiceAccounts(serviceAccounts []machinev1beta1.GCPServiceAccount, parentPath *field.Path) field.ErrorList {
	if len(serviceAccounts) != 1 {
		return field.ErrorList{field.Invalid(parentPath, fmt.Sprintf("%d service accounts supplied", len(serviceAccounts)), "exactly 1 service account must be supplied")}
	}

	var errs field.ErrorList
	for i, serviceAccount := range serviceAccounts {
		fldPath := parentPath.Index(i)

		if serviceAccount.Email == "" {
			errs = append(errs, field.Required(fldPath.Child("email"), "email is required"))
		}

		if len(serviceAccount.Scopes) == 0 {
			errs = append(errs, field.Required(fldPath.Child("scopes"), "at least 1 scope is required"))
		}
	}
	return errs
}

func defaultVSphere(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Defaulting vSphere providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.VSphereMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: defaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultVSphereCredentialsSecret}
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "value"), err))
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func validateVSphere(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Validating vSphere providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1beta1.VSphereMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if err := validateUnknownFields(m, providerSpec); err != nil {
		warnings = append(warnings, err.Error())
	}

	if !validateGVK(providerSpec.GroupVersionKind(), osconfigv1.VSpherePlatformType) {
		warnings = append(warnings, fmt.Sprintf("incorrect GroupVersionKind for VSphereMachineProviderSpec object: %s", providerSpec.GroupVersionKind()))
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
	if providerSpec.CloneMode == machinev1beta1.LinkedClone && providerSpec.DiskGiB > 0 {
		warnings = append(warnings, fmt.Sprintf("%s clone mode is set. DiskGiB parameter will be ignored, disk size from template will be used.", machinev1beta1.LinkedClone))
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
			warnings = append(warnings, credentialsSecretExists(config.client, providerSpec.CredentialsSecret.Name, m.GetNamespace())...)
		}
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

func validateVSphereWorkspace(workspace *machinev1beta1.Workspace, parentPath *field.Path) ([]string, field.ErrorList) {
	if workspace == nil {
		return []string{}, field.ErrorList{field.Required(parentPath, "workspace must be provided")}
	}

	var errs field.ErrorList
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

func validateVSphereNetwork(network machinev1beta1.NetworkSpec, parentPath *field.Path) field.ErrorList {
	if len(network.Devices) == 0 {
		return field.ErrorList{field.Required(parentPath.Child("devices"), "at least 1 network device must be provided")}
	}

	var errs field.ErrorList
	for i, spec := range network.Devices {
		fldPath := parentPath.Child("devices").Index(i)
		if spec.NetworkName == "" {
			errs = append(errs, field.Required(fldPath.Child("networkName"), "networkName must be provided"))
		}
	}

	return errs
}

func defaultNutanix(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Defaulting nutanix providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1.NutanixMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: defaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultNutanixCredentialsSecret}
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "value"), err))
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func validateNutanix(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Validating nutanix providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1.NutanixMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if err := validateUnknownFields(m, providerSpec); err != nil {
		warnings = append(warnings, err.Error())
	}

	if err := validateNutanixResourceIdentifier("cluster", providerSpec.Cluster); err != nil {
		errs = append(errs, err)
	}
	if err := validateNutanixResourceIdentifier("image", providerSpec.Image); err != nil {
		errs = append(errs, err)
	}
	// Currently, we only support one subnet per VM in Openshift
	// We may extend this to support more than one subnet per VM in future releases
	if len(providerSpec.Subnets) == 0 {
		subnets, _ := json.Marshal(providerSpec.Subnets)
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "subnets"), string(subnets), "missing subnets: nodes may fail to start if no subnets are configured"))
	} else if len(providerSpec.Subnets) > 1 {
		subnets, _ := json.Marshal(providerSpec.Subnets)
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "subnets"), string(subnets), "too many subnets: currently nutanix platform supports one subnet per VM but more than one subnets are configured"))
	}

	for _, subnet := range providerSpec.Subnets {
		if err := validateNutanixResourceIdentifier("subnet", subnet); err != nil {
			errs = append(errs, err)
		}
	}

	if providerSpec.VCPUSockets < minNutanixCPUSockets {
		warnings = append(warnings, fmt.Sprintf("providerSpec.vcpuSockets: %d is missing or less than the minimum value (%d): nodes may not boot correctly", providerSpec.VCPUSockets, minNutanixCPUSockets))
	}

	if providerSpec.VCPUsPerSocket < minNutanixCPUPerSocket {
		warnings = append(warnings, fmt.Sprintf("providerSpec.vcpusPerSocket: %d is missing or less than the minimum value (%d): nodes may not boot correctly", providerSpec.VCPUsPerSocket, minNutanixCPUPerSocket))
	}

	minNutanixMemory, err := resource.ParseQuantity(fmt.Sprintf("%dMi", minNutanixMemoryMiB))
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "memorySize"), fmt.Errorf("failed to parse minNutanixMemory: %v", err)))
		return false, warnings, errs
	}
	if providerSpec.MemorySize.Cmp(minNutanixMemory) < 0 {
		warnings = append(warnings, fmt.Sprintf("providerSpec.memorySize: %d is missing or less than the recommended minimum value (%d): nodes may not boot correctly", providerSpec.MemorySize.Value()/(1024*1024), minNutanixMemoryMiB))
	}

	minNutanixDiskSize, err := resource.ParseQuantity(fmt.Sprintf("%dGi", minNutanixDiskGiB))
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "systemDiskSize"), fmt.Errorf("failed to parse minNutanixDiskSize: %v", err)))
		return false, warnings, errs
	}
	if providerSpec.SystemDiskSize.Cmp(minNutanixDiskSize) < 0 {
		warnings = append(warnings, fmt.Sprintf("providerSpec.systemDiskSize: %d is missing or less than the recommended minimum (%d): nodes may fail to start if disk size is too low", providerSpec.SystemDiskSize.Value()/(1024*1024*1024), minNutanixDiskGiB))
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
			warnings = append(warnings, credentialsSecretExists(config.client, providerSpec.CredentialsSecret.Name, m.GetNamespace())...)
		}
	}

	// validate bootType
	if err := validateNutanixBootType(providerSpec.BootType); err != nil {
		errs = append(errs, err)
	}

	// validate project if configured
	if len(providerSpec.Project.Type) != 0 {
		if err := validateNutanixResourceIdentifier("project", providerSpec.Project); err != nil {
			errs = append(errs, err)
		}
	}

	// validate categories if configured
	if len(providerSpec.Categories) > 0 {
		for _, category := range providerSpec.Categories {
			if len(category.Key) < 1 || len(category.Key) > 64 {
				errs = append(errs, field.Invalid(field.NewPath("providerSpec", "categories", "key"), category.Key, "key must be a string with length between 1 and 64."))
			}
			if len(category.Value) < 1 || len(category.Value) > 64 {
				errs = append(errs, field.Invalid(field.NewPath("providerSpec", "categories", "value"), category.Value, "value must be a string with length between 1 and 64."))
			}
		}
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

func validateNutanixResourceIdentifier(resource string, identifier machinev1.NutanixResourceIdentifier) *field.Error {
	parentPath := field.NewPath("providerSpec")
	if identifier.Type == machinev1.NutanixIdentifierName {
		if identifier.Name == nil || *identifier.Name == "" {
			return field.Required(parentPath.Child(resource).Child("name"), fmt.Sprintf("%s name must be provided", resource))
		}
	} else if identifier.Type == machinev1.NutanixIdentifierUUID {
		if identifier.UUID == nil || *identifier.UUID == "" {
			return field.Required(parentPath.Child(resource).Child("uuid"), fmt.Sprintf("%s UUID must be provided", resource))
		}
	} else {
		return field.Invalid(parentPath.Child(resource).Child("type"), identifier.Type, fmt.Sprintf("%s type must be one of %s or %s", resource, machinev1.NutanixIdentifierName, machinev1.NutanixIdentifierUUID))
	}

	return nil
}

func validateNutanixBootType(bootType machinev1.NutanixBootType) *field.Error {
	parentPath := field.NewPath("providerSpec")
	// verify the bootType configurations
	// Type bootType field is optional, and valid values include: "", Legacy, UEFI, SecureBoot
	switch bootType {
	case "", machinev1.NutanixLegacyBoot, machinev1.NutanixUEFIBoot, machinev1.NutanixSecureBoot:
		// valid bootType
	default:
		errMsg := fmt.Sprintf("valid bootType values are: \"\", %q, %q, %q.",
			machinev1.NutanixLegacyBoot, machinev1.NutanixUEFIBoot, machinev1.NutanixSecureBoot)
		return field.Invalid(parentPath.Child("bootType"), bootType, errMsg)
	}

	return nil
}

func isAzureGovCloud(platformStatus *osconfigv1.PlatformStatus) bool {
	return platformStatus != nil && platformStatus.Azure != nil &&
		platformStatus.Azure.CloudName != osconfigv1.AzurePublicCloud
}

func validateMachineLifecycleHooks(m, oldM *machinev1beta1.Machine) field.ErrorList {
	var errs field.ErrorList

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

func validateAzureSecurityProfile(machineName string, spec *machinev1beta1.AzureMachineProviderSpec, parentPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	if spec.SecurityProfile == nil && spec.OSDisk.ManagedDisk.SecurityProfile.SecurityEncryptionType == "" {
		return errs
	}
	if spec.SecurityProfile == nil && spec.OSDisk.ManagedDisk.SecurityProfile.SecurityEncryptionType != "" {
		return append(errs, field.Required(parentPath, "securityProfile should be set when osDisk.managedDisk.securityProfile.securityEncryptionType is defined."))
	}

	switch spec.SecurityProfile.Settings.SecurityType {
	case machinev1beta1.SecurityTypesConfidentialVM:
		if spec.OSDisk.ManagedDisk.SecurityProfile.SecurityEncryptionType == "" {
			fieldPath := parentPath.Root().Child("osDisk").Child("managedDisk").Child("securityProfile")
			return append(errs, field.Required(fieldPath.Child("securityEncryptionType"),
				fmt.Sprintf("securityEncryptionType should be set when securityType is set to %s.",
					machinev1beta1.SecurityTypesConfidentialVM)))
		}

		if spec.SecurityProfile.Settings.ConfidentialVM == nil {
			return append(errs, field.Required(parentPath.Child("settings").Child("confidentialVM"),
				fmt.Sprintf("confidentialVM should be set when securityType is set to %s.",
					machinev1beta1.SecurityTypesConfidentialVM)))
		}

		if spec.SecurityProfile.Settings.ConfidentialVM.UEFISettings.VirtualizedTrustedPlatformModule != machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled {
			fieldPath := parentPath.Child("settings").Child("confidentialVM").Child("uefiSettings")
			return append(errs, field.Invalid(fieldPath.Child("virtualizedTrustedPlatformModule"),
				spec.SecurityProfile.Settings.ConfidentialVM.UEFISettings.VirtualizedTrustedPlatformModule,
				fmt.Sprintf("virtualizedTrustedPlatformModule should be enabled when securityType is set to %s.",
					machinev1beta1.SecurityTypesConfidentialVM)))
		}

		if spec.OSDisk.ManagedDisk.SecurityProfile.SecurityEncryptionType == machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState {
			if spec.SecurityProfile.EncryptionAtHost != nil && *spec.SecurityProfile.EncryptionAtHost {
				return append(errs, field.Invalid(parentPath.Child("encryptionAtHost"), spec.SecurityProfile.EncryptionAtHost,
					fmt.Sprintf("encryptionAtHost cannot be set to true when securityEncryptionType is set to %s.",
						machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState)))
			}

			if spec.SecurityProfile.Settings.ConfidentialVM.UEFISettings.SecureBoot != machinev1beta1.SecureBootPolicyEnabled {
				fieldPath := parentPath.Child("settings").Child("confidentialVM").Child("uefiSettings")
				return append(errs, field.Invalid(fieldPath.Child("secureBoot"), spec.SecurityProfile.Settings.ConfidentialVM.UEFISettings.SecureBoot,
					fmt.Sprintf("secureBoot should be enabled when securityEncryptionType is set to %s.",
						machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState)))
			}
		}
	case machinev1beta1.SecurityTypesTrustedLaunch:
		if spec.SecurityProfile.Settings.TrustedLaunch == nil {
			return append(errs, field.Required(parentPath.Child("settings").Child("trustedLaunch"),
				fmt.Sprintf("trustedLaunch should be set when securityType is set to %s.",
					machinev1beta1.SecurityTypesTrustedLaunch)))
		}
	default:
		if spec.SecurityProfile.Settings.SecurityType != machinev1beta1.SecurityTypesConfidentialVM &&
			spec.OSDisk.ManagedDisk.SecurityProfile.SecurityEncryptionType != "" {
			return append(errs, field.Invalid(parentPath.Child("settings").Child("securityType"),
				spec.SecurityProfile.Settings.SecurityType,
				fmt.Sprintf("securityType should be set to %s when securityEncryptionType is defined.",
					machinev1beta1.SecurityTypesConfidentialVM)))
		}

		if spec.SecurityProfile.Settings.TrustedLaunch != nil &&
			spec.SecurityProfile.Settings.SecurityType != machinev1beta1.SecurityTypesTrustedLaunch &&
			(spec.SecurityProfile.Settings.TrustedLaunch.UEFISettings.SecureBoot == machinev1beta1.SecureBootPolicyEnabled ||
				spec.SecurityProfile.Settings.TrustedLaunch.UEFISettings.VirtualizedTrustedPlatformModule == machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled) {
			return append(errs, field.Invalid(parentPath.Child("settings").Child("securityType"),
				spec.SecurityProfile.Settings.SecurityType,
				fmt.Sprintf("securityType should be set to %s when uefiSettings are enabled.",
					machinev1beta1.SecurityTypesTrustedLaunch)))
		}
	}

	return errs
}

func validateAzureDataDisks(machineName string, spec *machinev1beta1.AzureMachineProviderSpec, parentPath *field.Path) field.ErrorList {

	var errs field.ErrorList
	dataDiskLuns := make(map[int32]struct{})
	dataDiskNames := make(map[string]struct{})
	// defines rules for matching. strings must start and finish with an alphanumeric character
	// and can only contain letters, numbers, underscores, periods or hyphens.
	reg := regexp.MustCompile(`^[a-zA-Z0-9](?:[\w\.-]*[a-zA-Z0-9])?$`)

	for i, disk := range spec.DataDisks {
		fldPath := parentPath.Index(i)

		dataDiskName := machineName + "_" + disk.NameSuffix

		if len(dataDiskName) > 80 {
			errs = append(errs, field.Invalid(fldPath.Child("nameSuffix"), disk.NameSuffix, "too long, the overall disk name must not exceed 80 chars"))
		}

		if matched := reg.MatchString(disk.NameSuffix); !matched {
			errs = append(errs, field.Invalid(fldPath.Child("nameSuffix"), disk.NameSuffix, "nameSuffix must be provided, must start and finish with an alphanumeric character and can only contain letters, numbers, underscores, periods or hyphens"))
		}

		if _, exists := dataDiskNames[disk.NameSuffix]; exists {
			errs = append(errs, field.Invalid(fldPath.Child("nameSuffix"), disk.NameSuffix, "each Data Disk must have a unique nameSuffix"))
		}

		if disk.DiskSizeGB < 4 {
			errs = append(errs, field.Invalid(fldPath.Child("diskSizeGB"), disk.DiskSizeGB, "diskSizeGB must be provided and at least 4GB in size"))
		}

		if disk.Lun < 0 || disk.Lun > 63 {
			errs = append(errs, field.Invalid(fldPath.Child("lun"), disk.Lun, "must be greater than or equal to 0 and less than 64"))
		}

		if _, exists := dataDiskLuns[disk.Lun]; exists {
			errs = append(errs, field.Invalid(fldPath.Child("lun"), disk.Lun, "each Data Disk must have a unique lun"))
		}

		switch disk.DeletionPolicy {
		case machinev1beta1.DiskDeletionPolicyTypeDelete, machinev1beta1.DiskDeletionPolicyTypeDetach:
			// Valid scenarios, do nothing
		case "":
			errs = append(errs, field.Required(fldPath.Child("deletionPolicy"),
				fmt.Sprintf("deletionPolicy must be provided and must be either %s or %s",
					machinev1beta1.DiskDeletionPolicyTypeDelete, machinev1beta1.DiskDeletionPolicyTypeDetach)))
		default:
			errs = append(errs, field.Invalid(fldPath.Child("deletionPolicy"), disk.DeletionPolicy,
				fmt.Sprintf("must be either %s or %s", machinev1beta1.DiskDeletionPolicyTypeDelete, machinev1beta1.DiskDeletionPolicyTypeDetach)))
		}

		if (disk.ManagedDisk.StorageAccountType == machinev1beta1.StorageAccountUltraSSDLRS) &&
			(disk.CachingType != machinev1beta1.CachingTypeNone && disk.CachingType != "") {
			errs = append(errs,
				field.Invalid(fldPath.Child("cachingType"),
					disk.CachingType,
					fmt.Sprintf("must be \"None\" or omitted when storageAccountType is \"%s\"", machinev1beta1.StorageAccountUltraSSDLRS)),
			)
		}

		dataDiskLuns[disk.Lun] = struct{}{}
		dataDiskNames[disk.NameSuffix] = struct{}{}
	}

	return errs
}

func defaultPowerVS(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Defaulting PowerVS providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1.PowerVSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}
	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &machinev1.PowerVSSecretReference{Name: defaultUserDataSecret}
	}
	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &machinev1.PowerVSSecretReference{Name: defaultPowerVSCredentialsSecret}
	}
	if providerSpec.SystemType == "" {
		providerSpec.SystemType = defaultPowerVSSysType
	}
	if providerSpec.ProcessorType == "" {
		providerSpec.ProcessorType = defaultPowerVSProcType
	}
	if providerSpec.Processors.IntVal == 0 && providerSpec.Processors.StrVal == "" {
		switch providerSpec.ProcessorType {
		case machinev1.PowerVSProcessorTypeDedicated:
			providerSpec.Processors = intstr.IntOrString{Type: intstr.Int, IntVal: 1}
		default:
			providerSpec.Processors = intstr.IntOrString{Type: intstr.String, StrVal: "0.5"}
		}
	}
	if providerSpec.MemoryGiB == 0 {
		providerSpec.MemoryGiB = defaultPowerVSMemory
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, field.InternalError(field.NewPath("providerSpec", "value"), err))
	}

	if len(errs) > 0 {
		return false, warnings, errs
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func validatePowerVS(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, field.ErrorList) {
	klog.V(3).Infof("Validating PowerVS providerSpec")

	var errs field.ErrorList
	var warnings []string
	providerSpec := new(machinev1.PowerVSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, errs
	}

	if err := validateUnknownFields(m, providerSpec); err != nil {
		warnings = append(warnings, err.Error())
	}

	if providerSpec.KeyPairName == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "keyPairName"), "providerSpec.keyPairName must be provided"))
	}

	serviceInstanceErrors := validatePowerVSResourceIdentifiers(providerSpec.ServiceInstance, powerVSServiceInstance, field.NewPath("providerSpec", "serviceInstance"))
	errs = append(errs, serviceInstanceErrors...)

	imageErrors := validatePowerVSResourceIdentifiers(providerSpec.Image, powerVSImage, field.NewPath("providerSpec", "image"))
	errs = append(errs, imageErrors...)

	networkErrors := validatePowerVSResourceIdentifiers(providerSpec.Network, powerVSNetwork, field.NewPath("providerSpec", "network"))
	errs = append(errs, networkErrors...)

	if providerSpec.UserDataSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret"), "providerSpec.userDataSecret must be provided"))
	} else {
		if providerSpec.UserDataSecret.Name == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "userDataSecret", "name"), "providerSpec.userDataSecret.name must be provided"))
		}
	}

	if providerSpec.CredentialsSecret == nil {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret"), "providerSpec.credentialsSecret must be provided"))
	} else {
		if providerSpec.CredentialsSecret.Name == "" {
			errs = append(errs, field.Required(field.NewPath("providerSpec", "credentialsSecret", "name"), "providerSpec.credentialsSecret.name must be provided"))
		} else {
			warnings = append(warnings, credentialsSecretExists(config.client, providerSpec.CredentialsSecret.Name, m.GetNamespace())...)
		}
	}

	machineConfigWarnings, machineConfigErrors := validateMachineConfigurations(providerSpec, field.NewPath("providerSpec"))
	warnings = append(warnings, machineConfigWarnings...)
	errs = append(errs, machineConfigErrors...)

	if len(errs) > 0 {
		return false, warnings, errs
	}
	return true, warnings, nil
}

func validatePowerVSResourceIdentifiers(serviceInstance machinev1.PowerVSResource, resourceType string, parentPath *field.Path) field.ErrorList {
	var errs field.ErrorList

	switch serviceInstance.Type {
	case machinev1.PowerVSResourceTypeID:
		if serviceInstance.ID == nil {
			errs = append(errs, field.Required(parentPath.Child("id"),
				fmt.Sprintf("%s identifier is specified as ID but the value is nil", resourceType)))
		}
	case machinev1.PowerVSResourceTypeName:
		if serviceInstance.Name == nil {
			errs = append(errs, field.Required(parentPath.Child("name"),
				fmt.Sprintf("%s identifier is specified as Name but the value is nil", resourceType)))
		}
	case machinev1.PowerVSResourceTypeRegEx:
		if resourceType == powerVSServiceInstance || resourceType == powerVSImage {
			errs = append(errs, field.Invalid(parentPath, serviceInstance.Type,
				fmt.Sprintf("%s identifier is specified as %s but only %s and %s are valid resource identifiers",
					resourceType, serviceInstance.Type, machinev1.PowerVSResourceTypeID, machinev1.PowerVSResourceTypeName)))
		}
		if serviceInstance.RegEx == nil {
			errs = append(errs, field.Required(parentPath.Child("regex"),
				fmt.Sprintf("%s identifier is specified as Regex but the value is nil", resourceType)))
		}
	case "":
		errs = append(errs, field.Required(parentPath,
			fmt.Sprintf("%s identifier must be provided", resourceType)))
	default:
		errs = append(errs, field.Invalid(parentPath, serviceInstance.Type,
			fmt.Sprintf("%s identifier is specified as %s but only %s, %s and %s are valid resource identifiers",
				resourceType, serviceInstance.Type, machinev1.PowerVSResourceTypeID, machinev1.PowerVSResourceTypeName, machinev1.PowerVSResourceTypeRegEx)))
	}
	return errs
}

func validateMachineConfigurations(providerSpec *machinev1.PowerVSMachineProviderConfig, parentPath *field.Path) ([]string, field.ErrorList) {
	var errs field.ErrorList
	var warnings []string

	if providerSpec == nil {
		errs = append(errs, field.Required(parentPath, "providerSpec must be provided"))
		return warnings, errs
	}
	if val, found := powerVSMachineConfigurations[providerSpec.SystemType]; !found {
		warnings = append(warnings, fmt.Sprintf("providerSpec.SystemType: %s is not known, Currently known system types are %s, %s and %s", providerSpec.SystemType, defaultPowerVSSysType, powerVSSystemTypeE980, powerVSSystemTypeE880))
	} else {
		if providerSpec.MemoryGiB > val.maxMemoryGiB {
			errs = append(errs, field.Invalid(parentPath.Child("memoryGiB"), providerSpec.MemoryGiB, fmt.Sprintf("for %s systemtype the maximum supported memory value is %d", providerSpec.SystemType, val.maxMemoryGiB)))
		}

		if providerSpec.MemoryGiB < 0 {
			errs = append(errs, field.Invalid(parentPath.Child("memoryGiB"), providerSpec.MemoryGiB, "memory value cannot be negative"))
		} else if providerSpec.MemoryGiB < val.minMemoryGiB {
			warnings = append(warnings, fmt.Sprintf("providerspec.MemoryGiB %d is less than the minimum value %d", providerSpec.MemoryGiB, val.minMemoryGiB))
		}

		processor, err := getPowerVSProcessorValue(providerSpec.Processors)
		if err != nil {
			errs = append(errs, field.InternalError(parentPath.Child("processor"), fmt.Errorf("error while getting processor vlaue %w", err)))
			return warnings, errs
		} else {
			if processor > val.maxProcessor {
				errs = append(errs, field.Invalid(parentPath.Child("processor"), processor, fmt.Sprintf("for %s systemtype the maximum supported processor value is %f", providerSpec.SystemType, val.maxProcessor)))
			}
		}
		if processor < 0 {
			errs = append(errs, field.Invalid(parentPath.Child("processor"), processor, "processor value cannot be negative"))
		} else if providerSpec.ProcessorType == machinev1.PowerVSProcessorTypeDedicated && processor < val.minProcessorDedicated {
			warnings = append(warnings, fmt.Sprintf("providerspec.Processor %f is less than the minimum value %f for providerSpec.ProcessorType: %s", processor, val.minProcessorDedicated, providerSpec.ProcessorType))
		} else if processor < val.minProcessorSharedCapped {
			warnings = append(warnings, fmt.Sprintf("providerspec.Processor %f is less than the minimum value %f for providerSpec.ProcessorType: %s", processor, val.minProcessorSharedCapped, providerSpec.ProcessorType))
		}
	}
	return warnings, errs
}

func getPowerVSProcessorValue(processor intstr.IntOrString) (processors float64, err error) {
	switch processor.Type {
	case intstr.Int:
		processors = float64(processor.IntVal)
	case intstr.String:
		processors, err = strconv.ParseFloat(processor.StrVal, 64)
		if err != nil {
			err = fmt.Errorf("failed to convert Processors %s to float64", processor.StrVal)
		}
	}
	return
}

func isDeleting(obj metav1.Object) bool {
	return obj.GetDeletionTimestamp() != nil
}

// isFinalizerOnlyRemoval checks if the machine update only removes finalizers.
func isFinalizerOnlyRemoval(m, oldM *machinev1beta1.Machine) (bool, error) {
	// ignore updated managed fields as they don't affect the result
	machineCopy := m.DeepCopy()
	machineCopy.ManagedFields = oldM.ManagedFields

	patchBase := client.MergeFrom(oldM)
	data, err := patchBase.Data(machineCopy)
	if err != nil {
		return false, fmt.Errorf("cannot calculate patch data from machine object: %w", err)
	}

	return string(data) == `{"metadata":{"finalizers":null}}`, nil
}

func validateGVK(gvk schema.GroupVersionKind, platform osconfigv1.PlatformType) bool {
	switch platform {
	case osconfigv1.AWSPlatformType:
		return gvk.Kind == "AWSMachineProviderConfig" && (gvk.Group == "awsproviderconfig.openshift.io" || gvk.Group == "machine.openshift.io") && (gvk.Version == "v1beta1" || gvk.Version == "v1")
	case osconfigv1.AzurePlatformType:
		return gvk.Kind == "AzureMachineProviderSpec" && (gvk.Group == "azureproviderconfig.openshift.io" || gvk.Group == "machine.openshift.io") && (gvk.Version == "v1beta1" || gvk.Version == "v1")
	case osconfigv1.GCPPlatformType:
		return gvk.Kind == "GCPMachineProviderSpec" && (gvk.Group == "gcpprovider.openshift.io" || gvk.Group == "machine.openshift.io") && (gvk.Version == "v1beta1" || gvk.Version == "v1")
	case osconfigv1.VSpherePlatformType:
		return gvk.Kind == "VSphereMachineProviderSpec" && (gvk.Group == "vsphereprovider.openshift.io" || gvk.Group == "machine.openshift.io") && (gvk.Version == "v1beta1" || gvk.Version == "v1")
	default:
		return true
	}
}

// validateAzureCapacityReservationGroupID validate capacity reservation group ID.
func validateAzureCapacityReservationGroupID(capacityReservationGroupID string) error {
	id := strings.TrimPrefix(capacityReservationGroupID, azureProviderIDPrefix)
	err := parseAzureResourceID(id)
	if err != nil {
		return err
	}
	return nil
}

// parseAzureResourceID parses a string to an instance of ResourceID
func parseAzureResourceID(id string) error {
	if len(id) == 0 {
		return fmt.Errorf("invalid resource ID: id cannot be empty")
	}
	if !strings.HasPrefix(id, "/") {
		return fmt.Errorf("invalid resource ID: resource id '%s' must start with '/'", id)
	}
	parts := splitStringAndOmitEmpty(id, "/")

	if len(parts) < 2 {
		return fmt.Errorf("invalid resource ID: %s", id)
	}
	if !strings.EqualFold(parts[0], azureSubscriptionsKey) && !strings.EqualFold(parts[0], azureProvidersKey) {
		return fmt.Errorf("invalid resource ID: %s", id)
	}
	return appendNextAzureResourceIDValidation(parts, id)
}

func splitStringAndOmitEmpty(v, sep string) []string {
	r := make([]string, 0)
	for _, s := range strings.Split(v, sep) {
		if len(s) == 0 {
			continue
		}
		r = append(r, s)
	}
	return r
}

func appendNextAzureResourceIDValidation(parts []string, id string) error {
	if len(parts) == 0 {
		return nil
	}
	if len(parts) == 1 {
		// subscriptions and resourceGroups are not valid ids without their names
		if strings.EqualFold(parts[0], azureSubscriptionsKey) || strings.EqualFold(parts[0], azureResourceGroupsLowerKey) {
			return fmt.Errorf("invalid resource ID: %s", id)
		}
		return nil
	}
	if strings.EqualFold(parts[0], azureProvidersKey) && (len(parts) == 2 || strings.EqualFold(parts[2], azureProvidersKey)) {
		return appendNextAzureResourceIDValidation(parts[2:], id)
	}
	if len(parts) > 3 && strings.EqualFold(parts[0], azureProvidersKey) {
		return appendNextAzureResourceIDValidation(parts[4:], id)
	}
	if len(parts) > 1 && !strings.EqualFold(parts[0], azureProvidersKey) {
		return appendNextAzureResourceIDValidation(parts[2:], id)
	}
	return fmt.Errorf("invalid resource ID: %s", id)
}
