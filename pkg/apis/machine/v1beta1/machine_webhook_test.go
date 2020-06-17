package v1beta1

import (
	"encoding/json"
	"testing"

	osconfigv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	azure "sigs.k8s.io/cluster-api-provider-azure/pkg/apis/azureprovider/v1beta1"
	yaml "sigs.k8s.io/yaml"
)

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

			ok, err := h.webhookOperations(h, m)
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

	h := createMachineDefaulter(osconfigv1.AWSPlatformType, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &runtime.RawExtension{Raw: rawBytes}

			ok, err := h.webhookOperations(h, m)
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

			ok, err := h.webhookOperations(h, m)
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

	h := createMachineDefaulter(osconfigv1.AzurePlatformType, clusterID)

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

			ok, err := h.webhookOperations(h, m)
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
