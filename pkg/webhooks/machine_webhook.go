package webhooks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"runtime"
	"strings"

	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	"github.com/openshift/machine-api-operator/pkg/util/lifecyclehooks"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	yaml "sigs.k8s.io/yaml"
)

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
		return fmt.Sprintf("/resourceGroups/%s/providers/Microsoft.Compute/images/%s", clusterID+"-rg", clusterID)
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
)

const (
	DefaultMachineMutatingHookPath      = "/mutate-machine-openshift-io-v1beta1-machine"
	DefaultMachineValidatingHookPath    = "/validate-machine-openshift-io-v1beta1-machine"
	DefaultMachineSetMutatingHookPath   = "/mutate-machine-openshift-io-v1beta1-machineset"
	DefaultMachineSetValidatingHookPath = "/validate-machine-openshift-io-v1beta1-machineset"

	defaultWebhookConfigurationName = "machine-api"
	defaultWebhookServiceName       = "machine-api-operator-webhook"
	defaultWebhookServiceNamespace  = "openshift-machine-api"
	defaultWebhookServicePort       = 443

	defaultUserDataSecret  = "worker-user-data"
	defaultSecretNamespace = "openshift-machine-api"

	// AWS Defaults
	defaultAWSCredentialsSecret = "aws-cloud-credentials"
	defaultAWSX86InstanceType   = "m5.large"
	defaultAWSARMInstanceType   = "m6g.large"

	// Azure Defaults
	defaultAzureVMSize            = "Standard_D4s_V3"
	defaultAzureCredentialsSecret = "azure-cloud-credentials"
	defaultAzureOSDiskOSType      = "Linux"
	defaultAzureOSDiskStorageType = "Premium_LRS"

	// Azure OSDisk constants
	azureMaxDiskSizeGB                 = 32768
	azureEphemeralStorageLocationLocal = "Local"
	azureCachingTypeNone               = "None"
	azureCachingTypeReadOnly           = "ReadOnly"
	azureCachingTypeReadWrite          = "ReadWrite"

	// GCP Defaults
	defaultGCPMachineType       = "n1-standard-4"
	defaultGCPCredentialsSecret = "gcp-cloud-credentials"
	defaultGCPDiskSizeGb        = 128
	defaultGCPDiskType          = "pd-standard"
	// https://releases-art-rhcos.svc.ci.openshift.org/art/storage/releases/rhcos-4.8/48.83.202103122318-0/x86_64/meta.json
	// https://github.com/openshift/installer/blob/796a99049d3b7489b6c08ec5bd7c7983731afbcf/data/data/rhcos.json#L90-L94
	defaultGCPDiskImage = "projects/rhcos-cloud/global/images/rhcos-48-83-202103221318-0-gcp-x86-64"
	defaultGCPGPUCount  = 1

	// vSphere Defaults
	defaultVSphereCredentialsSecret = "vsphere-cloud-credentials"
	// Minimum vSphere values taken from vSphere reconciler
	minVSphereCPU       = 2
	minVSphereMemoryMiB = 2048
	// https://docs.openshift.com/container-platform/4.1/installing/installing_vsphere/installing-vsphere.html#minimum-resource-requirements_installing-vsphere
	minVSphereDiskGiB = 120
)

var (
	// webhookFailurePolicy is ignore so we don't want to block machine lifecycle on the webhook operational aspects.
	// This would be particularly problematic for chicken egg issues when bootstrapping a cluster.
	webhookFailurePolicy = admissionregistrationv1.Ignore
	webhookSideEffects   = admissionregistrationv1.SideEffectClassNone
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

type machineAdmissionFn func(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate)

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
func NewMachineValidator(client client.Client) (*machineValidatorHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	dns, err := getDNS()
	if err != nil {
		return nil, err
	}

	return createMachineValidator(infra, client, dns), nil
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
	default:
		// just no-op
		return func(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
			return true, []string{}, nil
		}
	}
}

// NewDefaulter returns a new machineDefaulterHandler.
func NewMachineDefaulter() (*machineDefaulterHandler, error) {
	infra, err := getInfra()
	if err != nil {
		return nil, err
	}

	return createMachineDefaulter(infra.Status.PlatformStatus, infra.Status.InfrastructureName), nil
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
		arch := runtime.GOARCH
		return awsDefaulter{region: region, arch: arch}.defaultAWS
	case osconfigv1.AzurePlatformType:
		return defaultAzure
	case osconfigv1.GCPPlatformType:
		return defaultGCP
	case osconfigv1.VSpherePlatformType:
		return defaultVSphere
	default:
		// just no-op
		return func(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
			return true, []string{}, nil
		}
	}
}

// NewValidatingWebhookConfiguration creates a validation webhook configuration with configured Machine and MachineSet webhooks
func NewValidatingWebhookConfiguration() *admissionregistrationv1.ValidatingWebhookConfiguration {
	validatingWebhookConfiguration := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultWebhookConfigurationName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{
			MachineValidatingWebhook(),
			MachineSetValidatingWebhook(),
		},
	}

	// Setting group version is required for testEnv to create unstructured objects, as the new structure sets it on empty strings
	// Usual way to populate those values, is to create the resource in the cluster first, which we can't yet do.
	validatingWebhookConfiguration.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("ValidatingWebhookConfiguration"))
	return validatingWebhookConfiguration
}

// MachineValidatingWebhook returns validating webhooks for machine to populate the configuration
func MachineValidatingWebhook() admissionregistrationv1.ValidatingWebhook {
	serviceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineValidatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "validation.machine.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &serviceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machines"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// MachineSetValidatingWebhook returns validating webhooks for machineSet to populate the configuration
func MachineSetValidatingWebhook() admissionregistrationv1.ValidatingWebhook {
	machinesetServiceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineSetValidatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.ValidatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "validation.machineset.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &machinesetServiceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machinesets"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
					admissionregistrationv1.Update,
				},
			},
		},
	}
}

// NewMutatingWebhookConfiguration creates a mutating webhook configuration with configured Machine and MachineSet webhooks
func NewMutatingWebhookConfiguration() *admissionregistrationv1.MutatingWebhookConfiguration {
	mutatingWebhookConfiguration := &admissionregistrationv1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultWebhookConfigurationName,
			Annotations: map[string]string{
				"service.beta.openshift.io/inject-cabundle": "true",
			},
		},
		Webhooks: []admissionregistrationv1.MutatingWebhook{
			MachineMutatingWebhook(),
			MachineSetMutatingWebhook(),
		},
	}

	// Setting group version is required for testEnv to create unstructured objects, as the new structure sets it on empty strings
	// Usual way to populate those values, is to create the resource in the cluster first, which we can't yet do.
	mutatingWebhookConfiguration.SetGroupVersionKind(admissionregistrationv1.SchemeGroupVersion.WithKind("MutatingWebhookConfiguration"))
	return mutatingWebhookConfiguration
}

// MachineMutatingWebhook returns mutating webhooks for machine to apply in configuration
func MachineMutatingWebhook() admissionregistrationv1.MutatingWebhook {
	machineServiceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineMutatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.MutatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "default.machine.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &machineServiceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machines"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
			},
		},
	}
}

// MachineSetMutatingWebhook returns mutating webhook for machineSet to apply in configuration
func MachineSetMutatingWebhook() admissionregistrationv1.MutatingWebhook {
	machineSetServiceReference := admissionregistrationv1.ServiceReference{
		Namespace: defaultWebhookServiceNamespace,
		Name:      defaultWebhookServiceName,
		Path:      pointer.StringPtr(DefaultMachineSetMutatingHookPath),
		Port:      pointer.Int32Ptr(defaultWebhookServicePort),
	}
	return admissionregistrationv1.MutatingWebhook{
		AdmissionReviewVersions: []string{"v1"},
		Name:                    "default.machineset.machine.openshift.io",
		FailurePolicy:           &webhookFailurePolicy,
		SideEffects:             &webhookSideEffects,
		ClientConfig: admissionregistrationv1.WebhookClientConfig{
			Service: &machineSetServiceReference,
		},
		Rules: []admissionregistrationv1.RuleWithOperations{
			{
				Rule: admissionregistrationv1.Rule{
					APIGroups:   []string{machinev1beta1.GroupName},
					APIVersions: []string{machinev1beta1.SchemeGroupVersion.Version},
					Resources:   []string{"machinesets"},
				},
				Operations: []admissionregistrationv1.OperationType{
					admissionregistrationv1.Create,
				},
			},
		},
	}
}

func (h *machineValidatorHandler) validateMachine(m, oldM *machinev1beta1.Machine) (bool, []string, utilerrors.Aggregate) {
	// Skip validation if we just remove the finalizer.
	// For more information: https://issues.redhat.com/browse/OCPCLOUD-1426
	if !m.DeletionTimestamp.IsZero() {
		isFinalizerOnly, err := isFinalizerOnlyRemoval(m, oldM)
		if err != nil {
			return false, nil, utilerrors.NewAggregate([]error{err})
		}
		if isFinalizerOnly {
			return true, nil, nil
		}
	}

	errs := validateMachineLifecycleHooks(m, oldM)

	ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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
	m := &machinev1beta1.Machine{}

	if err := h.decoder.Decode(req, m); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	var oldM *machinev1beta1.Machine
	if len(req.OldObject.Raw) > 0 {
		// oldM must only be initialised if there is an old object (ie on UPDATE or DELETE).
		// It should be nil otherwise to allow skipping certain validations that rely on
		// the presence of the old object.
		oldM = &machinev1beta1.Machine{}
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
	m := &machinev1beta1.Machine{}

	if err := h.decoder.Decode(req, m); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
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

	ok, warnings, errs := h.webhookOperations(m, h.admissionConfig)
	if !ok {
		return admission.Denied(errs.Error()).WithWarnings(warnings...)
	}

	marshaledMachine, err := json.Marshal(m)
	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err).WithWarnings(warnings...)
	}
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaledMachine).WithWarnings(warnings...)
}

type awsDefaulter struct {
	region string
	arch   string
}

func (a awsDefaulter) defaultAWS(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting AWS providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.AWSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.InstanceType == "" {
		if a.arch == "arm64" {
			providerSpec.InstanceType = defaultAWSARMInstanceType
		} else {
			providerSpec.InstanceType = defaultAWSX86InstanceType
		}
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
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func unmarshalInto(m *machinev1beta1.Machine, providerSpec interface{}) error {
	if m.Spec.ProviderSpec.Value == nil {
		return field.Required(field.NewPath("providerSpec", "value"), "a value must be provided")
	}

	if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &providerSpec); err != nil {
		return field.Invalid(field.NewPath("providerSpec", "value"), providerSpec, err.Error())
	}
	return nil
}

func validateAWS(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating AWS providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.AWSMachineProviderConfig)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
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
		return false, warnings, utilerrors.NewAggregate(errs)
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

func defaultAzure(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting Azure providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.AzureMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.VMSize == "" {
		providerSpec.VMSize = defaultAzureVMSize
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
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func validateAzure(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating Azure providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.AzureMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.VMSize == "" {
		errs = append(errs, field.Required(field.NewPath("providerSpec", "vmSize"), "vmSize should be set to one of the supported Azure VM sizes"))
	}

	if providerSpec.PublicIP && config.dnsDisconnected {
		errs = append(errs, field.Forbidden(field.NewPath("providerSpec", "publicIP"), "publicIP is not allowed in Azure disconnected installation"))
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

	switch providerSpec.UltraSSDCapability {
	case machinev1beta1.AzureUltraSSDCapabilityEnabled, machinev1beta1.AzureUltraSSDCapabilityDisabled, "":
		// Valid scenarios, do nothing
	default:
		errs = append(errs, field.Invalid(field.NewPath("providerSpec", "ultraSSDCapability"), providerSpec.UltraSSDCapability,
			fmt.Sprintf("ultraSSDCapability can be only %s, %s or omitted", machinev1beta1.AzureUltraSSDCapabilityEnabled, machinev1beta1.AzureUltraSSDCapabilityDisabled)))
	}

	errs = append(errs, validateAzureDataDisks(m.Name, providerSpec, field.NewPath("providerSpec", "dataDisks"))...)

	if isAzureGovCloud(config.platformStatus) && providerSpec.SpotVMOptions != nil {
		warnings = append(warnings, "spot VMs may not be supported when using GovCloud region")
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}
	return true, warnings, nil
}

func validateAzureImage(image machinev1beta1.Image) []error {
	errors := []error{}
	if image == (machinev1beta1.Image{}) {
		return append(errors, field.Required(field.NewPath("providerSpec", "image"), "an image reference must be provided"))
	}

	if image.ResourceID != "" {
		if image != (machinev1beta1.Image{ResourceID: image.ResourceID}) {
			return append(errors, field.Required(field.NewPath("providerSpec", "image", "resourceID"), "resourceID is already specified, other fields such as [Offer, Publisher, SKU, Version] should not be set"))
		}
		return errors
	}

	// Resource ID not provided, so Offer, Publisher, SKU and Version are required
	if image.Offer == "" {
		errors = append(errors, field.Required(field.NewPath("providerSpec", "image", "Offer"), "Offer must be provided"))
	}
	if image.Publisher == "" {
		errors = append(errors, field.Required(field.NewPath("providerSpec", "image", "Publisher"), "Publisher must be provided"))
	}
	if image.SKU == "" {
		errors = append(errors, field.Required(field.NewPath("providerSpec", "image", "SKU"), "SKU must be provided"))
	}
	if image.Version == "" {
		errors = append(errors, field.Required(field.NewPath("providerSpec", "image", "Version"), "Version must be provided"))
	}

	return errors
}

func defaultGCP(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting GCP providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.GCPMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.MachineType == "" {
		providerSpec.MachineType = defaultGCPMachineType
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
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
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
				Image:      defaultGCPDiskImage,
			},
		}
	}

	for _, disk := range disks {
		if disk.Type == "" {
			disk.Type = defaultGCPDiskType
		}

		if disk.Image == "" {
			disk.Image = defaultGCPDiskImage
		}
	}

	return disks
}

func validateGCP(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating GCP providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.GCPMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
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
		return false, warnings, utilerrors.NewAggregate(errs)
	}
	return true, warnings, nil
}

func validateGCPNetworkInterfaces(networkInterfaces []*machinev1beta1.GCPNetworkInterface, parentPath *field.Path) []error {
	if len(networkInterfaces) == 0 {
		return []error{field.Required(parentPath, "at least 1 network interface is required")}
	}

	var errs []error
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

func validateGCPDisks(disks []*machinev1beta1.GCPDisk, parentPath *field.Path) []error {
	if len(disks) == 0 {
		return []error{field.Required(parentPath, "at least 1 disk is required")}
	}

	var errs []error
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

func validateGCPGPUs(guestAccelerators []machinev1beta1.GCPGPUConfig, parentPath *field.Path, machineType string) []error {
	var errs []error
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

func validateGCPServiceAccounts(serviceAccounts []machinev1beta1.GCPServiceAccount, parentPath *field.Path) []error {
	if len(serviceAccounts) != 1 {
		return []error{field.Invalid(parentPath, fmt.Sprintf("%d service accounts supplied", len(serviceAccounts)), "exactly 1 service account must be supplied")}
	}

	var errs []error
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

func defaultVSphere(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Defaulting vSphere providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.VSphereMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
		errs = append(errs, err)
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	if providerSpec.UserDataSecret == nil {
		providerSpec.UserDataSecret = &corev1.LocalObjectReference{Name: defaultUserDataSecret}
	}

	if providerSpec.CredentialsSecret == nil {
		providerSpec.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultVSphereCredentialsSecret}
	}

	rawBytes, err := json.Marshal(providerSpec)
	if err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return false, warnings, utilerrors.NewAggregate(errs)
	}

	m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
	return true, warnings, nil
}

func validateVSphere(m *machinev1beta1.Machine, config *admissionConfig) (bool, []string, utilerrors.Aggregate) {
	klog.V(3).Infof("Validating vSphere providerSpec")

	var errs []error
	var warnings []string
	providerSpec := new(machinev1beta1.VSphereMachineProviderSpec)
	if err := unmarshalInto(m, providerSpec); err != nil {
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
		return false, warnings, utilerrors.NewAggregate(errs)
	}
	return true, warnings, nil
}

func validateVSphereWorkspace(workspace *machinev1beta1.Workspace, parentPath *field.Path) ([]string, []error) {
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

func validateVSphereNetwork(network machinev1beta1.NetworkSpec, parentPath *field.Path) []error {
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

func isAzureGovCloud(platformStatus *osconfigv1.PlatformStatus) bool {
	return platformStatus != nil && platformStatus.Azure != nil &&
		platformStatus.Azure.CloudName != osconfigv1.AzurePublicCloud
}

func validateMachineLifecycleHooks(m, oldM *machinev1beta1.Machine) []error {
	var errs []error

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

func validateAzureDataDisks(machineName string, spec *machinev1beta1.AzureMachineProviderSpec, parentPath *field.Path) []error {

	var errs []error
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

func isDeleting(obj metav1.Object) bool {
	return obj.GetDeletionTimestamp() != nil
}

// isFinalizerOnlyRemoval checks if the machine update only removes finalizers.
func isFinalizerOnlyRemoval(m, oldM *machinev1beta1.Machine) (bool, error) {
	patchBase := client.MergeFrom(oldM)
	data, err := patchBase.Data(m)
	if err != nil {
		return false, fmt.Errorf("cannot calculate patch data from machine object: %w", err)
	}

	return string(data) == `{"metadata":{"finalizers":[""]}}`, nil
}
