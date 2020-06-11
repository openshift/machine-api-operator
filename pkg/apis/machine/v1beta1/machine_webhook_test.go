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
				UserDataSecret:    &corev1.LocalObjectReference{Name: defaultAWSUserDataSecret},
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
