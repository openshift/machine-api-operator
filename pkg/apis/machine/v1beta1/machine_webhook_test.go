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
		providerSpec  *aws.AWSMachineProviderConfig
		expectedError string
		expectedOk    bool
	}{
		{
			testCase: "with no ami values it fails",
			providerSpec: &aws.AWSMachineProviderConfig{
				AMI:          aws.AWSResourceReference{},
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
			},
			expectedOk:    false,
			expectedError: "providerSpec.ami: Required value: expected either providerSpec.ami.arn or providerSpec.ami.filters or providerSpec.ami.id to be populated",
		},
		{
			testCase: "with no ami values and no instanceType it fails",
			providerSpec: &aws.AWSMachineProviderConfig{
				AMI:          aws.AWSResourceReference{},
				InstanceType: "",
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
			},
			expectedOk:    false,
			expectedError: "[providerSpec.ami: Required value: expected either providerSpec.ami.arn or providerSpec.ami.filters or providerSpec.ami.id to be populated, providerSpec.instanceType: Required value: expected providerSpec.instanceType to be populated]",
		},
		{
			testCase: "with all required values it succeeds",
			providerSpec: &aws.AWSMachineProviderConfig{
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
			},
			expectedOk:    true,
			expectedError: "",
		},
	}

	h := createMachineValidator(osconfigv1.AWSPlatformType, "clusterID")

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
