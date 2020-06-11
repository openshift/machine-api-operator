package v1beta1

import (
	"encoding/json"
	"testing"

	. "github.com/onsi/gomega"
	osconfigv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	azure "sigs.k8s.io/cluster-api-provider-azure/pkg/apis/azureprovider/v1beta1"
	gcp "sigs.k8s.io/cluster-api-provider-gcp/pkg/apis/gcpprovider/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	yaml "sigs.k8s.io/yaml"
)

func TestMachineCreation(t *testing.T) {
	g := NewWithT(t)

	// Override config getter
	ctrl.GetConfig = func() (*rest.Config, error) {
		return cfg, nil
	}
	defer func() {
		ctrl.GetConfig = config.GetConfig
	}()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machine-creation-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	infra := &osconfigv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
	g.Expect(c.Create(ctx, infra)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, infra)).To(Succeed())
	}()

	testCases := []struct {
		name              string
		platformType      osconfigv1.PlatformType
		clusterID         string
		expectedError     string
		providerSpecValue *runtime.RawExtension
	}{
		{
			name:         "with AWS and no fields set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{},
			},
			expectedError: "providerSpec.ami: Required value: expected either providerSpec.ami.arn or providerSpec.ami.filters or providerSpec.ami.id to be populated",
		},
		{
			name:         "with AWS and an AMI ID set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{
					AMI: aws.AWSResourceReference{
						ID: pointer.StringPtr("ami"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and no fields set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{},
			},
			expectedError: "[providerSpec.location: Required value: location should be set to one of the supported Azure regions, providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero]",
		},
		{
			name:         "with Azure and a location and disk size set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{
					Location: "location",
					OSDisk: azure.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with GCP and no fields set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &gcp.GCPMachineProviderSpec{},
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with GCP and the region and zone set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &gcp.GCPMachineProviderSpec{
					Region: "region",
					Zone:   "region-zone",
				},
			},
			expectedError: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			mgr, err := manager.New(cfg, manager.Options{
				MetricsBindAddress: "0",
				Port:               testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			done := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(done)).To(Succeed())
			}()
			defer close(done)

			infra.Status = osconfigv1.InfrastructureStatus{
				InfrastructureName: tc.clusterID,
				PlatformStatus: &osconfigv1.PlatformStatus{
					Type: tc.platformType,
					GCP: &osconfigv1.GCPPlatformStatus{
						ProjectID: "gcp-project-id",
					},
				},
			}
			gs.Expect(c.Status().Update(ctx, infra)).To(Succeed())

			machineDefaulter, err := NewMachineDefaulter()
			gs.Expect(err).ToNot(HaveOccurred())
			machineValidator, err := NewMachineValidator()
			gs.Expect(err).ToNot(HaveOccurred())
			mgr.GetWebhookServer().Register("/mutate-machine-openshift-io-v1beta1-machine", &webhook.Admission{Handler: machineDefaulter})
			mgr.GetWebhookServer().Register("/validate-machine-openshift-io-v1beta1-machine", &webhook.Admission{Handler: machineValidator})

			m := &Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
				},
				Spec: MachineSpec{
					ProviderSpec: ProviderSpec{
						Value: tc.providerSpecValue,
					},
				},
			}
			err = c.Create(ctx, m)
			if err == nil {
				defer func() {
					gs.Expect(c.Delete(ctx, m)).To(Succeed())
				}()
			}

			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestMachineUpdate(t *testing.T) {
	awsClusterID := "aws-cluster"
	defaultAWSProviderSpec := &aws.AWSMachineProviderConfig{
		AMI: aws.AWSResourceReference{
			ID: pointer.StringPtr("ami"),
		},
		InstanceType: defaultAWSInstanceType,
		IAMInstanceProfile: &aws.AWSResourceReference{
			ID: defaultAWSIAMInstanceProfile(awsClusterID),
		},
		UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
		CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
		SecurityGroups: []aws.AWSResourceReference{
			{
				Filters: []aws.Filter{
					{
						Name:   "tag:Name",
						Values: []string{defaultAWSSecurityGroup(awsClusterID)},
					},
				},
			},
		},
		Placement: aws.Placement{
			Region:           "region",
			AvailabilityZone: "zone",
		},
		Subnet: aws.AWSResourceReference{
			Filters: []aws.Filter{
				{
					Name:   "tag:Name",
					Values: []string{defaultAWSSubnet(awsClusterID, "zone")},
				},
			},
		},
	}

	azureClusterID := "azure-cluster"
	defaultAzureProviderSpec := &azure.AzureMachineProviderSpec{
		Location:             "location",
		VMSize:               defaultAzureVMSize,
		Vnet:                 defaultAzureVnet(azureClusterID),
		Subnet:               defaultAzureSubnet(azureClusterID),
		NetworkResourceGroup: defaultAzureNetworkResourceGroup(azureClusterID),
		Image: azure.Image{
			ResourceID: defaultAzureImageResourceID(azureClusterID),
		},
		ManagedIdentity: defaultAzureManagedIdentiy(azureClusterID),
		ResourceGroup:   defaultAzureResourceGroup(azureClusterID),
		UserDataSecret: &corev1.SecretReference{
			Name:      defaultUserDataSecret,
			Namespace: defaultSecretNamespace,
		},
		CredentialsSecret: &corev1.SecretReference{
			Name:      defaultAzureCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
		OSDisk: azure.OSDisk{
			DiskSizeGB: 128,
			OSType:     defaultAzureOSDiskOSType,
			ManagedDisk: azure.ManagedDisk{
				StorageAccountType: defaultAzureOSDiskStorageType,
			},
		},
	}

	gcpClusterID := "gcp-cluster"
	gcpProjectID := "gcp-project-id"
	defaultGCPProviderSpec := &gcp.GCPMachineProviderSpec{
		Region:      "region",
		Zone:        "region-zone",
		MachineType: defaultGCPMachineType,
		NetworkInterfaces: []*gcp.GCPNetworkInterface{
			{
				Network:    defaultGCPNetwork(gcpClusterID),
				Subnetwork: defaultGCPSubnetwork(gcpClusterID),
			},
		},
		Disks: []*gcp.GCPDisk{
			{
				AutoDelete: true,
				Boot:       true,
				SizeGb:     defaultGCPDiskSizeGb,
				Type:       defaultGCPDiskType,
				Image:      defaultGCPDiskImage(gcpClusterID),
			},
		},
		ServiceAccounts: defaultGCPServiceAccounts(gcpClusterID, gcpProjectID),
		Tags:            defaultGCPTags(gcpClusterID),
		UserDataSecret: &corev1.LocalObjectReference{
			Name: defaultUserDataSecret,
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: defaultGCPCredentialsSecret,
		},
	}

	g := NewWithT(t)

	// Override config getter
	ctrl.GetConfig = func() (*rest.Config, error) {
		return cfg, nil
	}
	defer func() {
		ctrl.GetConfig = config.GetConfig
	}()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "machine-update-test",
		},
	}
	g.Expect(c.Create(ctx, namespace)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, namespace)).To(Succeed())
	}()

	infra := &osconfigv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
	g.Expect(c.Create(ctx, infra)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, infra)).To(Succeed())
	}()

	testCases := []struct {
		name                     string
		platformType             osconfigv1.PlatformType
		clusterID                string
		expectedError            string
		baseProviderSpecValue    *runtime.RawExtension
		updatedProviderSpecValue func() *runtime.RawExtension
	}{
		{
			name:         "with a valid AWS ProviderSpec",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultAWSProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an AWS ProviderSpec, removing the instance type",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.InstanceType = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.instanceType: Required value: expected providerSpec.instanceType to be populated",
		},
		{
			name:         "with an AWS ProviderSpec, removing the instance profile",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.IAMInstanceProfile = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.iamInstanceProfile: Required value: expected providerSpec.iamInstanceProfile to be populated",
		},
		{
			name:         "with an AWS ProviderSpec, removing the user data secret",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.UserDataSecret = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.userDataSecret: Required value: expected providerSpec.userDataSecret to be populated",
		},
		{
			name:         "with a valid Azure ProviderSpec",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultAzureProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an Azure ProviderSpec, removing the vm size",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.VMSize = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.vmSize: Required value: vmSize should be set to one of the supported Azure VM sizes",
		},
		{
			name:         "with an Azure ProviderSpec, removing the subnet",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.Subnet = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.subnet: Required value: must provide a subnet when a virtual network is specified",
		},
		{
			name:         "with an Azure ProviderSpec, removing the credentials secret",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.CredentialsSecret = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			name:         "with a valid GCP ProviderSpec",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultGCPProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with a GCP ProviderSpec, removing the region",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Region = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with a GCP ProviderSpec, and an invalid region",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Zone = "zone"
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.zone: Invalid value: \"zone\": zone not in configured region (region)",
		},
		{
			name:         "with a GCP ProviderSpec, removing the disks",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Disks = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.disks: Required value: at least 1 disk is required",
		},
		{
			name:         "with a GCP ProviderSpec, removing the service accounts",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.ServiceAccounts = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.serviceAccounts: Invalid value: \"0 service accounts supplied\": exactly 1 service account must be supplied",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			mgr, err := manager.New(cfg, manager.Options{
				MetricsBindAddress: "0",
				Port:               testEnv.WebhookInstallOptions.LocalServingPort,
				CertDir:            testEnv.WebhookInstallOptions.LocalServingCertDir,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			done := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(done)).To(Succeed())
			}()
			defer close(done)

			infra.Status = osconfigv1.InfrastructureStatus{
				InfrastructureName: tc.clusterID,
				PlatformStatus: &osconfigv1.PlatformStatus{
					Type: tc.platformType,
					GCP: &osconfigv1.GCPPlatformStatus{
						ProjectID: gcpProjectID,
					},
				},
			}
			gs.Expect(c.Status().Update(ctx, infra)).To(Succeed())

			machineDefaulter, err := NewMachineDefaulter()
			gs.Expect(err).ToNot(HaveOccurred())
			machineValidator, err := NewMachineValidator()
			gs.Expect(err).ToNot(HaveOccurred())
			mgr.GetWebhookServer().Register("/mutate-machine-openshift-io-v1beta1-machine", &webhook.Admission{Handler: machineDefaulter})
			mgr.GetWebhookServer().Register("/validate-machine-openshift-io-v1beta1-machine", &webhook.Admission{Handler: machineValidator})

			m := &Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
				},
				Spec: MachineSpec{
					ProviderSpec: ProviderSpec{
						Value: tc.baseProviderSpecValue,
					},
				},
			}
			err = c.Create(ctx, m)
			gs.Expect(err).ToNot(HaveOccurred())
			defer func() {
				gs.Expect(c.Delete(ctx, m)).To(Succeed())
			}()

			m.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			err = c.Update(ctx, m)
			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestValidateAWSProviderSpec(t *testing.T) {

	testCases := []struct {
		testCase      string
		modifySpec    func(*aws.AWSMachineProviderConfig)
		expectedError string
		expectedOk    bool
	}{
		{
			testCase: "with no ami values it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.AMI = aws.AWSResourceReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.ami: Required value: expected either providerSpec.ami.arn or providerSpec.ami.filters or providerSpec.ami.id to be populated",
		},
		{
			testCase: "with no instanceType it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.InstanceType = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.instanceType: Required value: expected providerSpec.instanceType to be populated",
		},
		{
			testCase: "with no iam instance profile it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.IAMInstanceProfile = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.iamInstanceProfile: Required value: expected providerSpec.iamInstanceProfile to be populated",
		},
		{
			testCase: "with no user data secret it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: expected providerSpec.userDataSecret to be populated",
		},
		{
			testCase: "with no credentials secret it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "with no security groups it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.SecurityGroups = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.securityGroups: Required value: expected providerSpec.securityGroups to be populated",
		},
		{
			testCase: "with no subnet values it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.Subnet = aws.AWSResourceReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.subnet: Required value: expected either providerSpec.subnet.arn or providerSpec.subnet.id or providerSpec.subnet.filters or providerSpec.placement.availabilityZone to be populated",
		},
		{
			testCase:      "with all required values it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
	}

	h := createMachineValidator(osconfigv1.AWSPlatformType, "clusterID")

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &aws.AWSMachineProviderConfig{
				AMI: aws.AWSResourceReference{
					ID: pointer.StringPtr("ami"),
				},
				InstanceType: "m4.large",
				IAMInstanceProfile: &aws.AWSResourceReference{
					ID: pointer.StringPtr("profileID"),
				},
				UserDataSecret: &corev1.LocalObjectReference{
					Name: "secret",
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "secret",
				},
				SecurityGroups: []aws.AWSResourceReference{
					{
						ID: pointer.StringPtr("sg"),
					},
				},
				Subnet: aws.AWSResourceReference{
					ID: pointer.StringPtr("subnet"),
				},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &Machine{}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(m, h.clusterID)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}
		})
	}
}

func TestDefaultAWSProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	testCases := []struct {
		testCase             string
		providerSpec         *aws.AWSMachineProviderConfig
		expectedProviderSpec *aws.AWSMachineProviderConfig
		expectedError        string
		expectedOk           bool
	}{
		{
			testCase: "it defaults InstanceType, IAMInstanceProfile, UserDataSecret, CredentialsSecret, SecurityGroups and Subnet",
			providerSpec: &aws.AWSMachineProviderConfig{
				AMI:                aws.AWSResourceReference{},
				InstanceType:       "",
				IAMInstanceProfile: nil,
				UserDataSecret:     nil,
				CredentialsSecret:  nil,
				SecurityGroups:     []aws.AWSResourceReference{},
				Subnet:             aws.AWSResourceReference{},
				Placement: aws.Placement{
					Region:           "region",
					AvailabilityZone: "zone",
				},
			},
			expectedProviderSpec: &aws.AWSMachineProviderConfig{
				AMI:          aws.AWSResourceReference{},
				InstanceType: defaultAWSInstanceType,
				IAMInstanceProfile: &aws.AWSResourceReference{
					ID: defaultAWSIAMInstanceProfile(clusterID),
				},
				UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
				CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
				SecurityGroups: []aws.AWSResourceReference{
					{
						Filters: []aws.Filter{
							{
								Name:   "tag:Name",
								Values: []string{defaultAWSSecurityGroup(clusterID)},
							},
						},
					},
				},
				Placement: aws.Placement{
					Region:           "region",
					AvailabilityZone: "zone",
				},
				Subnet: aws.AWSResourceReference{
					Filters: []aws.Filter{
						{
							Name:   "tag:Name",
							Values: []string{defaultAWSSubnet(clusterID, "zone")},
						},
					},
				},
			},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.AWSPlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(m, h.clusterID)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(aws.AWSMachineProviderConfig)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(tc.expectedProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", tc.expectedProviderSpec, gotProviderSpec)
			}
			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}
		})
	}
}

func TestValidateAzureProviderSpec(t *testing.T) {

	testCases := []struct {
		testCase      string
		modifySpec    func(providerSpec *azure.AzureMachineProviderSpec)
		expectedError string
		expectedOk    bool
	}{
		{
			testCase: "with no location it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Location = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.location: Required value: location should be set to one of the supported Azure regions",
		},
		{
			testCase: "with no vmsize it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.VMSize = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.vmSize: Required value: vmSize should be set to one of the supported Azure VM sizes",
		},
		{
			testCase: "with a vnet but no subnet it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Vnet = "vnet"
				p.Subnet = ""
				p.NetworkResourceGroup = "nrg"
			},
			expectedOk:    false,
			expectedError: "providerSpec.subnet: Required value: must provide a subnet when a virtual network is specified",
		},
		{
			testCase: "with a subnet but no vnet it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Vnet = ""
				p.Subnet = "subnet"
				p.NetworkResourceGroup = "nrg"
			},
			expectedOk:    false,
			expectedError: "providerSpec.vnet: Required value: must provide a virtual network when supplying subnets",
		},
		{
			testCase: "with a vnet and subnet but no resource group it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Vnet = "vnet"
				p.Subnet = "subnet"
				p.NetworkResourceGroup = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.networkResourceGroup: Required value: must provide a network resource group when a virtual network or subnet is specified",
		},
		{
			testCase: "with no image it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image.resourceID: Required value: resourceID must be provided",
		},
		{
			testCase: "with no managed identity it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.ManagedIdentity = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.managedIdentity: Required value: managedIdentity must be provided",
		},
		{
			testCase: "with no resource group it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.ResourceGroup = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.resourceGropu: Required value: resourceGroup must be provided",
		},
		{
			testCase: "with no user data secret it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.UserDataSecret = &corev1.SecretReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials secret it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "with no credentials secret namespace it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.CredentialsSecret = &corev1.SecretReference{
					Name: "name",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret.namespace: Required value: namespace must be provided",
		},
		{
			testCase: "with no credentials secret name it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.CredentialsSecret = &corev1.SecretReference{
					Namespace: "namespace",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no os disk size it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.OSDisk = azure.OSDisk{
					OSType: "osType",
					ManagedDisk: azure.ManagedDisk{
						StorageAccountType: "storageAccountType",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero",
		},
		{
			testCase: "with no os disk type it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.OSDisk = azure.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: azure.ManagedDisk{
						StorageAccountType: "storageAccountType",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.osDisk.osType: Required value: osType must be provided",
		},
		{
			testCase: "with no os disk storage account type it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.OSDisk = azure.OSDisk{
					DiskSizeGB: 1,
					OSType:     "osType",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.osDisk.managedDisk.storageAccountType: Required value: storageAccountType must be provided",
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
	}

	h := createMachineValidator(osconfigv1.AzurePlatformType, "clusterID")

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			// create a valid spec that will then be 'broken' by modifySpec
			providerSpec := &azure.AzureMachineProviderSpec{
				Location: "location",
				VMSize:   "vmSize",
				Image: azure.Image{
					ResourceID: "resourceID",
				},
				ManagedIdentity: "managedIdentity",
				ResourceGroup:   "resourceGroup",
				UserDataSecret: &corev1.SecretReference{
					Name: "name",
				},
				CredentialsSecret: &corev1.SecretReference{
					Name:      "name",
					Namespace: "namespace",
				},
				OSDisk: azure.OSDisk{
					DiskSizeGB: 1,
					OSType:     "osType",
					ManagedDisk: azure.ManagedDisk{
						StorageAccountType: "storageAccountType",
					},
				},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &Machine{}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(m, h.clusterID)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}
		})
	}
}

func TestDefaultAzureProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	testCases := []struct {
		testCase      string
		providerSpec  *azure.AzureMachineProviderSpec
		modifyDefault func(*azure.AzureMachineProviderSpec)
		expectedError string
		expectedOk    bool
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &azure.AzureMachineProviderSpec{},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "does not overwrite the network resource group if it already exists",
			providerSpec: &azure.AzureMachineProviderSpec{
				NetworkResourceGroup: "nrg",
			},
			modifyDefault: func(p *azure.AzureMachineProviderSpec) {
				p.NetworkResourceGroup = "nrg"
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "does not overwrite the credentials secret namespace if they already exist",
			providerSpec: &azure.AzureMachineProviderSpec{
				CredentialsSecret: &corev1.SecretReference{
					Namespace: "foo",
				},
			},
			modifyDefault: func(p *azure.AzureMachineProviderSpec) {
				p.CredentialsSecret.Namespace = "foo"
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "does not overwrite the secret names if they already exist",
			providerSpec: &azure.AzureMachineProviderSpec{
				UserDataSecret: &corev1.SecretReference{
					Name: "foo",
				},
				CredentialsSecret: &corev1.SecretReference{
					Name: "foo",
				},
			},
			modifyDefault: func(p *azure.AzureMachineProviderSpec) {
				p.UserDataSecret.Name = "foo"
				p.CredentialsSecret.Name = "foo"
			},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.AzurePlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			defaultProviderSpec := &azure.AzureMachineProviderSpec{
				VMSize:               defaultAzureVMSize,
				Vnet:                 defaultAzureVnet(clusterID),
				Subnet:               defaultAzureSubnet(clusterID),
				NetworkResourceGroup: defaultAzureNetworkResourceGroup(clusterID),
				Image: azure.Image{
					ResourceID: defaultAzureImageResourceID(clusterID),
				},
				ManagedIdentity: defaultAzureManagedIdentiy(clusterID),
				ResourceGroup:   defaultAzureResourceGroup(clusterID),
				UserDataSecret: &corev1.SecretReference{
					Name: defaultUserDataSecret,
				},
				CredentialsSecret: &corev1.SecretReference{
					Name:      defaultAzureCredentialsSecret,
					Namespace: defaultSecretNamespace,
				},
				OSDisk: azure.OSDisk{
					OSType: defaultAzureOSDiskOSType,
					ManagedDisk: azure.ManagedDisk{
						StorageAccountType: defaultAzureOSDiskStorageType,
					},
				},
			}
			if tc.modifyDefault != nil {
				tc.modifyDefault(defaultProviderSpec)
			}

			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(m, h.clusterID)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(azure.AzureMachineProviderSpec)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}
		})
	}
}

func TestValidateGCPProviderSpec(t *testing.T) {

	testCases := []struct {
		testCase      string
		modifySpec    func(*gcp.GCPMachineProviderSpec)
		expectedError string
		expectedOk    bool
	}{
		{
			testCase: "with no region",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Region = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			testCase: "with no zone",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Zone = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.zone: Invalid value: \"\": zone not in configured region (region)",
		},
		{
			testCase: "with an invalid zone",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Zone = "zone"
			},
			expectedOk:    false,
			expectedError: "providerSpec.zone: Invalid value: \"zone\": zone not in configured region (region)",
		},
		{
			testCase: "with no machine type",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.MachineType = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.machineType: Required value: machineType should be set to one of the supported GCP machine types",
		},
		{
			testCase: "with no network interfaces",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.NetworkInterfaces = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.networkInterfaces: Required value: at least 1 network interface is required",
		},
		{
			testCase: "with a network interfaces is missing the network",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.NetworkInterfaces = []*gcp.GCPNetworkInterface{
					{
						Network:    "network",
						Subnetwork: "subnetwork",
					},
					{
						Subnetwork: "subnetwork",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.networkInterfaces[1].network: Required value: network is required",
		},
		{
			testCase: "with a network interfaces is missing the subnetwork",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.NetworkInterfaces = []*gcp.GCPNetworkInterface{
					{
						Network:    "network",
						Subnetwork: "subnetwork",
					},
					{
						Network: "network",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.networkInterfaces[1].subnetwork: Required value: subnetwork is required",
		},
		{
			testCase: "with no disks",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Disks = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks: Required value: at least 1 disk is required",
		},
		{
			testCase: "with a disk that is too small",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Disks = []*gcp.GCPDisk{
					{
						SizeGb: 1,
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks[0].sizeGb: Invalid value: 1: must be at least 16GB in size",
		},
		{
			testCase: "with a disk that is too large",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Disks = []*gcp.GCPDisk{
					{
						SizeGb: 100000,
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks[0].sizeGb: Invalid value: 100000: exceeding maximum GCP disk size limit, must be below 65536",
		},
		{
			testCase: "with a disk type that is not supported",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.Disks = []*gcp.GCPDisk{
					{
						SizeGb: 16,
						Type:   "invalid",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks[0].type: Unsupported value: \"invalid\": supported values: \"pd-ssd\", \"pd-standard\"",
		},
		{
			testCase: "with multiple service accounts",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.ServiceAccounts = []gcp.GCPServiceAccount{
					{},
					{},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.serviceAccounts: Invalid value: \"2 service accounts supplied\": exactly 1 service account must be supplied",
		},
		{
			testCase: "with the service account's email missing",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.ServiceAccounts = []gcp.GCPServiceAccount{
					{
						Scopes: []string{"scope"},
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.serviceAccounts[0].email: Required value: email is required",
		},
		{
			testCase: "with the service account's with no scopes",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.ServiceAccounts = []gcp.GCPServiceAccount{
					{
						Email: "email",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.serviceAccounts[0].scopes: Required value: at least 1 scope is required",
		},
		{
			testCase: "with no user data secret",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.UserDataSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials data secret",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "with no user data secret name",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.CredentialsSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret.name: Required value: name must be provided",
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
	}

	h := createMachineValidator(osconfigv1.GCPPlatformType, "clusterID")

	for _, tc := range testCases {
		providerSpec := &gcp.GCPMachineProviderSpec{
			Region:      "region",
			Zone:        "region-zone",
			ProjectID:   "projectID",
			MachineType: "machineType",
			NetworkInterfaces: []*gcp.GCPNetworkInterface{
				{
					Network:    "network",
					Subnetwork: "subnetwork",
				},
			},
			Disks: []*gcp.GCPDisk{
				{
					SizeGb: 16,
				},
			},
			ServiceAccounts: []gcp.GCPServiceAccount{
				{
					Email:  "email",
					Scopes: []string{"scope"},
				},
			},
			UserDataSecret: &corev1.LocalObjectReference{
				Name: "name",
			},
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: "name",
			},
		}
		if tc.modifySpec != nil {
			tc.modifySpec(providerSpec)
		}

		t.Run(tc.testCase, func(t *testing.T) {
			m := &Machine{}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(m, h.clusterID)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}
		})
	}
}

func TestDefaultGCPProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	projectID := "projectID"
	testCases := []struct {
		testCase      string
		providerSpec  *gcp.GCPMachineProviderSpec
		modifyDefault func(*gcp.GCPMachineProviderSpec)
		expectedError string
		expectedOk    bool
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &gcp.GCPMachineProviderSpec{},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "it does not overwrite disks which already have fields set",
			providerSpec: &gcp.GCPMachineProviderSpec{
				Disks: []*gcp.GCPDisk{
					{
						AutoDelete: false,
						Boot:       false,
						SizeGb:     32,
					},
				},
			},
			modifyDefault: func(p *gcp.GCPMachineProviderSpec) {
				p.Disks = []*gcp.GCPDisk{
					{
						AutoDelete: false,
						Boot:       false,
						SizeGb:     32,
						Type:       defaultGCPDiskType,
						Image:      defaultGCPDiskImage(clusterID),
					},
				}
			},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{
		Type: osconfigv1.GCPPlatformType,
		GCP: &osconfigv1.GCPPlatformStatus{
			ProjectID: projectID,
		},
	}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		defaultProviderSpec := &gcp.GCPMachineProviderSpec{
			MachineType: defaultGCPMachineType,
			NetworkInterfaces: []*gcp.GCPNetworkInterface{
				{
					Network:    defaultGCPNetwork(clusterID),
					Subnetwork: defaultGCPSubnetwork(clusterID),
				},
			},
			Disks: []*gcp.GCPDisk{
				{
					AutoDelete: true,
					Boot:       true,
					SizeGb:     defaultGCPDiskSizeGb,
					Type:       defaultGCPDiskType,
					Image:      defaultGCPDiskImage(clusterID),
				},
			},
			ServiceAccounts: defaultGCPServiceAccounts(clusterID, projectID),
			Tags:            defaultGCPTags(clusterID),
			UserDataSecret: &corev1.LocalObjectReference{
				Name: defaultUserDataSecret,
			},
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: defaultGCPCredentialsSecret,
			},
		}
		if tc.modifyDefault != nil {
			tc.modifyDefault(defaultProviderSpec)
		}

		t.Run(tc.testCase, func(t *testing.T) {
			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(m, h.clusterID)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(gcp.GCPMachineProviderSpec)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if err == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, err)
				}
			} else {
				if err.Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, err.Error())
				}
			}
		})
	}
}
