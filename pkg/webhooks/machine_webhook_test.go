package webhooks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/openshift/api/features"
	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
	"sigs.k8s.io/yaml"

	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
)

var (
	plainDNS   = &osconfigv1.DNS{Spec: osconfigv1.DNSSpec{}}
	plainInfra = &osconfigv1.Infrastructure{
		Status: osconfigv1.InfrastructureStatus{
			PlatformStatus: &osconfigv1.PlatformStatus{},
		},
	}
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

	awsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultAWSCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	vSphereSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultVSphereCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	GCPSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultGCPCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	azureSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultAzureCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
	}
	powerVSSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPowerVSCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
	}
	nutanixSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultNutanixCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
	}
	g.Expect(c.Create(ctx, awsSecret)).To(Succeed())
	g.Expect(c.Create(ctx, vSphereSecret)).To(Succeed())
	g.Expect(c.Create(ctx, GCPSecret)).To(Succeed())
	g.Expect(c.Create(ctx, azureSecret)).To(Succeed())
	g.Expect(c.Create(ctx, powerVSSecret)).To(Succeed())
	g.Expect(c.Create(ctx, nutanixSecret)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, awsSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, vSphereSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, GCPSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, azureSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, powerVSSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, nutanixSecret)).To(Succeed())
	}()

	testCases := []struct {
		name              string
		platformType      osconfigv1.PlatformType
		clusterID         string
		presetClusterID   bool
		expectedError     string
		disconnected      bool
		providerSpecValue *kruntime.RawExtension
	}{
		{
			name:              "with AWS and a nil provider spec value",
			platformType:      osconfigv1.AWSPlatformType,
			clusterID:         "aws-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with AWS and no fields set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{},
			},
			expectedError: "providerSpec.ami: Required value: expected providerSpec.ami.id to be populated",
		},
		{
			name:         "with AWS and an AMI ID set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with AWS and CapacityReservationID is empty",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					CapacityReservationID: "",
				},
			},
			expectedError: "",
		},
		{
			name:         "with AWS and CapacityReservationID is valid",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					CapacityReservationID: "cr-12345678901234567",
				},
			},
			expectedError: "",
		},
		{
			name:         "with AWS and CapacityReservationID is not valid",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					CapacityReservationID: "cr-123",
				},
			},
			expectedError: "admission webhook \"validation.machine.machine.openshift.io\" denied the request: providerSpec.capacityReservationId: Invalid value: \"cr-123\": invalid value for capacityReservationId: \"cr-123\", it must start with 'cr-' and be exactly 20 characters long with 17 hexadecimal characters",
		},
		{
			name:         "with invalid MarketType provided",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					MarketType: "invalid",
				},
			},
			expectedError: fmt.Sprintf("invalid marketType: invalid valid values are: %s, %s, %s and omitted", machinev1beta1.MarketTypeOnDemand, machinev1beta1.MarketTypeSpot, machinev1beta1.MarketTypeCapacityBlock),
		},
		{
			name:         "with MarketType empty value provided",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					MarketType: "",
				},
			},
			expectedError: "",
		},
		{
			name:         "with MarketType Spot and  CapacityReservationID value provided",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					MarketType:            machinev1beta1.MarketTypeSpot,
					CapacityReservationID: "cr-12345678901234567",
				},
			},
			expectedError: "invalid marketType: marketType set to Spot and capacityReservationID may not be used together",
		},
		{
			name:         "with CapacityReservationID and SpotMarketOptions are provided",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					SpotMarketOptions:     &machinev1beta1.SpotMarketOptions{},
					CapacityReservationID: "cr-12345678901234567",
				},
			},
			expectedError: "spotMarketOptions and capacityReservationID may not be used together",
		},
		{
			name:         "with MarketType set to MarketTypeCapacityBlock and spotMarketOptions are specified",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					CapacityReservationID: "cr-12345678901234567",
					MarketType:            machinev1beta1.MarketTypeCapacityBlock,
					SpotMarketOptions:     &machinev1beta1.SpotMarketOptions{},
				},
			},
			expectedError: "admission webhook \"validation.machine.machine.openshift.io\" denied the request: providerSpec.marketType: Invalid value: \"CapacityBlock\": invalid marketType: marketType set to CapacityBlock and spotMarketOptions cannot be used together",
		},
		{
			name:         "with MarketType set to MarketTypeOnDemand and spotMarketOptions are specified",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					MarketType:        machinev1beta1.MarketTypeOnDemand,
					SpotMarketOptions: &machinev1beta1.SpotMarketOptions{},
				},
			},
			expectedError: "admission webhook \"validation.machine.machine.openshift.io\" denied the request: providerSpec.marketType: Invalid value: \"OnDemand\": invalid marketType: setting marketType to OnDemand and spotMarketOptions cannot be used together",
		},
		{
			name:         "with MarketType set to MarketTypeCapacityBlock is specified and CapacityReservationId is not provided",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					MarketType: machinev1beta1.MarketTypeCapacityBlock,
				},
			},
			expectedError: "admission webhook \"validation.machine.machine.openshift.io\" denied the request: providerSpec.marketType: Invalid value: \"CapacityBlock\": capacityReservationID is required when CapacityBlock is provided",
		},
		{
			name:         "with MarketType set to MarketTypeCapacityBlock and CapacityReservationId are provided",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
					},
					CapacityReservationID: "cr-12345678901234567",
					MarketType:            machinev1beta1.MarketTypeCapacityBlock,
				},
			},
			expectedError: "",
		},
		{
			name:              "with Azure and a nil provider spec value",
			platformType:      osconfigv1.AzurePlatformType,
			clusterID:         "azure-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with Azure and no fields set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{},
			},
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero and less than 32768",
		},
		{
			name:         "with Azure and a disk size set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and correct ephemeral storage location",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB:  128,
						CachingType: "ReadOnly",
						DiskSettings: machinev1beta1.DiskSettings{
							EphemeralStorageLocation: "Local",
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and incorrect ephemeral storage location",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
						DiskSettings: machinev1beta1.DiskSettings{
							EphemeralStorageLocation: "INVALID",
						},
					},
				},
			},
			expectedError: "providerSpec.osDisk.diskSettings.ephemeralStorageLocation: Invalid value: \"INVALID\": osDisk.diskSettings.ephemeralStorageLocation can either be omitted or set to Local",
		},
		{
			name:         "with Azure and correct cachingType",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB:  128,
						CachingType: "ReadOnly",
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and incorrect cachingType",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB:  128,
						CachingType: "INVALID",
					},
				},
			},
			expectedError: "providerSpec.osDisk.cachingType: Invalid value: \"INVALID\": osDisk.cachingType can be only None, ReadOnly, ReadWrite or omitted",
		},
		{
			name:         "with Azure and ephemeral storage location with incorrect cachingType",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB:  128,
						CachingType: "ReadWrite",
						DiskSettings: machinev1beta1.DiskSettings{
							EphemeralStorageLocation: "Local",
						},
					},
				},
			},
			expectedError: "providerSpec.osDisk.cachingType: Invalid value: \"ReadWrite\": Instances using an ephemeral OS disk support only Readonly caching",
		},
		{
			name:         "with Azure disconnected installation request public IP",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					PublicIP: true,
				},
			},
			disconnected:  true,
			expectedError: "providerSpec.publicIP: Forbidden: publicIP is not allowed in Azure disconnected installation with publish strategy as internal",
		},
		{
			name:         "with Azure disconnected installation success",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
			disconnected: true,
		},
		{
			name:         "with Azure and a Data Disk set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and an Ultra Disk Data Disk set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix: "test",
							DiskSizeGB: 4,
							Lun:        0,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountUltraSSDLRS,
							},
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and CapacityReservationID is empty",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					CapacityReservationGroupID: "",
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and CapacityReservationID is valid",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					CapacityReservationGroupID: "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup",
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and CapacityReservationID is not valid",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					CapacityReservationGroupID: "/subscri/00000000-0000-0000-0000-000000000000/resour/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup",
				},
			},
			expectedError: "admission webhook \"validation.machine.machine.openshift.io\" denied the request: providerSpec.capacityReservationGroupID: Invalid value: \"/subscri/00000000-0000-0000-0000-000000000000/resour/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup\": invalid resource ID: /subscri/00000000-0000-0000-0000-000000000000/resour/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup",
		},
		{
			name:         "with Azure and a Premium Disk Data Disk set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix: "test",
							DiskSizeGB: 4,
							Lun:        0,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountPremiumLRS,
							},
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and and two Data Disks set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
						{
							NameSuffix:     "test-1",
							DiskSizeGB:     4,
							Lun:            1,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and a Data Disk with empty nameSuffix",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[0].nameSuffix: " +
				"Invalid value: \"\":" +
				" nameSuffix must be provided, must start and finish with an alphanumeric character and can only contain letters, numbers, underscores, periods or hyphens",
		},
		{
			name:         "with Azure and a Data Disks too long nameSuffix",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "qwkuid031j3x3fxktj9saez28zoo2843jkl35w3ner90i9wvwkqphau1l5y7j7k3750960btqljnlthoq",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[0].nameSuffix: " +
				"Invalid value: \"qwkuid031j3x3fxktj9saez28zoo2843jkl35w3ner90i9wvwkqphau1l5y7j7k3750960btqljnlthoq\":" +
				" too long, the overall disk name must not exceed 80 chars",
		},
		{
			name:         "with Azure and a Data Disks invalid chars",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "inv$alid",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[0].nameSuffix: " +
				"Invalid value: \"inv$alid\":" +
				" nameSuffix must be provided, must start and finish with an alphanumeric character and can only contain letters, numbers, underscores, periods or hyphens",
		},
		{
			name:         "with Azure and two Data Disks set with non unique nameSuffix",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            1,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[1].nameSuffix: Invalid value:" +
				" \"test\": each Data Disk must have a unique nameSuffix",
		},
		{
			name:         "with Azure and two Data Disks set with diskSizeGB too low",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     3,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[0].diskSizeGB: Invalid value: 3: diskSizeGB must be provided and at least 4GB in size",
		},
		{
			name:         "with Azure and two Data Disks set with non unique lun",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
						{
							NameSuffix:     "test-1",
							DiskSizeGB:     4,
							Lun:            0,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[1].lun: Invalid value: 0: each Data Disk must have a unique lun",
		},
		{
			name:         "with Azure and two Data Disks set with lun too low",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            -1,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[0].lun: Invalid value: -1: must be greater than or equal to 0 and less than 64",
		},
		{
			name:         "with Azure and two Data Disks set with lun too high",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:     "test",
							DiskSizeGB:     4,
							Lun:            64,
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "providerSpec.dataDisks[0].lun: Invalid value: 64: must be greater than or equal to 0 and less than 64",
		},
		{
			name:         "with Azure and Ultra Disk with forbidden non-None cachingType",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:  "test",
							DiskSizeGB:  4,
							Lun:         0,
							CachingType: machinev1beta1.CachingTypeReadOnly,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountUltraSSDLRS,
							},
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: fmt.Sprintf("providerSpec.dataDisks[0].cachingType:"+
				" Invalid value: \"%s\": must be \"None\" or omitted when storageAccountType is \"%s\"",
				machinev1beta1.CachingTypeReadOnly, machinev1beta1.StorageAccountUltraSSDLRS),
		},
		{
			name:         "with Azure and ultraSSDCapability Enabled",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					UltraSSDCapability: machinev1beta1.AzureUltraSSDCapabilityEnabled,
				},
			},
		},
		{
			name:         "with Azure and ultraSSDCapability Disabled",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					UltraSSDCapability: machinev1beta1.AzureUltraSSDCapabilityDisabled,
				},
			},
		},
		{
			name:         "with Azure and ultraSSDCapability omitted",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
		},
		{
			name:         "with Azure and ultraSSDCapability with wrong value",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					UltraSSDCapability: "hello",
				},
			},
			expectedError: fmt.Sprintf("providerSpec.ultraSSDCapability: Invalid value: \"hello\": ultraSSDCapability"+
				" can be only %s, %s or omitted", machinev1beta1.AzureUltraSSDCapabilityEnabled, machinev1beta1.AzureUltraSSDCapabilityDisabled),
		},
		{
			name:         "with Azure and deletionPolicy with wrong value",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:  "test",
							DiskSizeGB:  4,
							Lun:         0,
							CachingType: machinev1beta1.CachingTypeNone,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountUltraSSDLRS,
							},
							DeletionPolicy: "bla",
						},
					},
				},
			},
			expectedError: fmt.Sprintf("providerSpec.dataDisks[0].deletionPolicy:"+
				" Invalid value: \"%s\": must be either %s or %s",
				"bla", machinev1beta1.DiskDeletionPolicyTypeDelete, machinev1beta1.DiskDeletionPolicyTypeDetach),
		},
		{
			name:         "with Azure and deletionPolicy with omitted value",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:  "test",
							DiskSizeGB:  4,
							Lun:         0,
							CachingType: machinev1beta1.CachingTypeNone,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountUltraSSDLRS,
							},
						},
					},
				},
			},
			expectedError: fmt.Sprintf("providerSpec.dataDisks[0].deletionPolicy:"+
				" Required value: deletionPolicy must be provided and must be either %s or %s",
				machinev1beta1.DiskDeletionPolicyTypeDelete, machinev1beta1.DiskDeletionPolicyTypeDetach),
		},
		{
			name:         "with Azure and deletionPolicy with Detach value",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:  "test",
							DiskSizeGB:  4,
							Lun:         0,
							CachingType: machinev1beta1.CachingTypeNone,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountUltraSSDLRS,
							},
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDetach,
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure and deletionPolicy with Delete value",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
						DiskSizeGB: 128,
					},
					DataDisks: []machinev1beta1.DataDisk{
						{
							NameSuffix:  "test",
							DiskSizeGB:  4,
							Lun:         0,
							CachingType: machinev1beta1.CachingTypeNone,
							ManagedDisk: machinev1beta1.DataDiskManagedDiskParameters{
								StorageAccountType: machinev1beta1.StorageAccountUltraSSDLRS,
							},
							DeletionPolicy: machinev1beta1.DiskDeletionPolicyTypeDelete,
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:              "with GCP and a nil provider spec value",
			platformType:      osconfigv1.GCPPlatformType,
			clusterID:         "gcp-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with GCP and no fields set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.GCPMachineProviderSpec{},
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with GCP and the region and zone set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.GCPMachineProviderSpec{
					Region: "region",
					Zone:   "region-zone",
				},
			},
			expectedError: "",
		},
		{
			name:              "with vSphere and a nil provider spec value",
			platformType:      osconfigv1.VSpherePlatformType,
			clusterID:         "vsphere-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with vSphere and no fields set",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    "vsphere-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.VSphereMachineProviderSpec{},
			},
			expectedError: "[providerSpec.template: Required value: template must be provided," +
				" providerSpec.workspace: Required value: workspace must be provided," +
				" providerSpec.network.devices: Required value: at least 1 network device must be provided]",
		},
		{
			name:            "with vSphere and the template, workspace and network devices set",
			platformType:    osconfigv1.VSpherePlatformType,
			clusterID:       "vsphere-cluster",
			presetClusterID: true,
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1beta1.VSphereMachineProviderSpec{
					Template: "template",
					Workspace: &machinev1beta1.Workspace{
						Datacenter: "datacenter",
						Server:     "server",
					},
					Network: machinev1beta1.NetworkSpec{
						Devices: []machinev1beta1.NetworkDeviceSpec{
							{
								NetworkName: "networkName",
							},
						},
					},
				},
			},
			expectedError: "",
		},
		{
			name:              "with PowerVS and a nil provider spec value",
			platformType:      osconfigv1.PowerVSPlatformType,
			clusterID:         "powervs-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with PowerVS and no serviceInstanceID set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					KeyPairName: "TestKeyPair",
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "providerSpec.serviceInstance: Required value: serviceInstance identifier must be provided",
		},
		{
			name:         "with PowerVS and ServiceInstance ID set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeID,
						ID:   ptr.To[string]("TestServiceInstanceID"),
					},
					KeyPairName: "TestKeyPair",
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with PowerVS and ServiceInstance Name set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					KeyPairName: "TestKeyPair",
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with PowerVS and ServiceInstance Regex is set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					ServiceInstance: machinev1.PowerVSResource{
						Type:  machinev1.PowerVSResourceTypeRegEx,
						RegEx: ptr.To[string]("TestServiceInstanceID"),
					},
					KeyPairName: "TestKeyPair",
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "providerSpec.serviceInstance: Invalid value: \"RegEx\": serviceInstance identifier is specified as RegEx but only ID and Name are valid resource identifiers",
		},
		{
			name:         "with PowerVS and no keyPair set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "providerSpec.keyPairName: Required value: providerSpec.keyPairName must be provided",
		},
		{
			name:         "with PowerVS and no Image ID or Image Name set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					KeyPairName: "TestKeyPair",
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "providerSpec.image: Required value: image identifier must be provided",
		},
		{
			name:         "with PowerVS and no Network ID or Network Name or Regex set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					KeyPairName: "TestKeyPair",
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "providerSpec.network: Required value: network identifier must be provided",
		},
		{
			name:         "with PowerVS and with Network ID is set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					KeyPairName: "TestKeyPair",
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeID,
						ID:   ptr.To[string]("TestNetworkID"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with PowerVS and with Network Regex is set",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					KeyPairName: "TestKeyPair",
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type:  machinev1.PowerVSResourceTypeRegEx,
						RegEx: ptr.To[string]("DHCP"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with PowerVS and with correct data",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    "powervs-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.PowerVSMachineProviderConfig{
					KeyPairName: "TestKeyPair",
					ServiceInstance: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestServiceInstanceID"),
					},
					Image: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
					Network: machinev1.PowerVSResource{
						Type: machinev1.PowerVSResourceTypeName,
						Name: ptr.To[string]("TestNetworkName"),
					},
				},
			},
			expectedError: "",
		},
		{
			name:              "with nutanix and a nil provider spec value",
			platformType:      osconfigv1.NutanixPlatformType,
			clusterID:         "nutanix-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with nutanix and no fields set",
			platformType: osconfigv1.NutanixPlatformType,
			clusterID:    "nutanix-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.NutanixMachineProviderConfig{},
			},
			expectedError: "[providerSpec.cluster.type: Invalid value: \"\": cluster type must be one of name or uuid, providerSpec.image.type: Invalid value: \"\": image type must be one of name or uuid, providerSpec.subnets: Invalid value: \"null\": missing subnets: nodes may fail to start if no subnets are configured]",
		},
		{
			name:            "with nutanix and too many subnets set",
			platformType:    osconfigv1.NutanixPlatformType,
			clusterID:       "nutanix-cluster",
			presetClusterID: true,
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.NutanixMachineProviderConfig{
					VCPUSockets:    minNutanixCPUSockets,
					VCPUsPerSocket: minNutanixCPUPerSocket,
					MemorySize:     resource.MustParse(fmt.Sprintf("%dMi", minNutanixMemoryMiB)),
					SystemDiskSize: resource.MustParse(fmt.Sprintf("%dGi", minNutanixDiskGiB)),
					Subnets: []machinev1.NutanixResourceIdentifier{
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-1")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-2")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-3")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-4")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-5")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-6")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-7")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-8")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-9")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-10")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-11")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-12")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-13")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-14")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-15")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-16")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-17")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-18")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-19")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-20")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-21")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-22")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-23")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-24")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-25")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-26")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-27")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-28")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-29")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-30")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-31")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-32")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-33")},
					},
					Cluster: machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("cluster-1")},
					Image:   machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("image-1")},
				},
			},
			expectedError: "providerSpec.subnets: Too many: 33: must have at most 32 items",
		},
		{
			name:            "with nutanix and duplicate subnet set",
			platformType:    osconfigv1.NutanixPlatformType,
			clusterID:       "nutanix-cluster",
			presetClusterID: true,
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.NutanixMachineProviderConfig{
					VCPUSockets:    minNutanixCPUSockets,
					VCPUsPerSocket: minNutanixCPUPerSocket,
					MemorySize:     resource.MustParse(fmt.Sprintf("%dMi", minNutanixMemoryMiB)),
					SystemDiskSize: resource.MustParse(fmt.Sprintf("%dGi", minNutanixDiskGiB)),
					Subnets: []machinev1.NutanixResourceIdentifier{
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-1")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-1")},
					},
					Cluster: machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("cluster-1")},
					Image:   machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("image-1")},
				},
			},
			expectedError: `providerSpec.subnets: Invalid value: "{\"type\":\"name\",\"name\":\"subnet-1\"}": should not configure duplicate subnet value`,
		},
		{
			name:            "with nutanix and the required fields set",
			platformType:    osconfigv1.NutanixPlatformType,
			clusterID:       "nutanix-cluster",
			presetClusterID: true,
			providerSpecValue: &kruntime.RawExtension{
				Object: &machinev1.NutanixMachineProviderConfig{
					VCPUSockets:    minNutanixCPUSockets,
					VCPUsPerSocket: minNutanixCPUPerSocket,
					MemorySize:     resource.MustParse(fmt.Sprintf("%dMi", minNutanixMemoryMiB)),
					SystemDiskSize: resource.MustParse(fmt.Sprintf("%dGi", minNutanixDiskGiB)),
					Subnets: []machinev1.NutanixResourceIdentifier{
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-1")},
						{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-2")},
					},
					Cluster: machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("cluster-1")},
					Image:   machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("image-1")},
				},
			},
			expectedError: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			mgr, err := manager.New(cfg, manager.Options{
				Metrics: metricsserver.Options{
					BindAddress: "0",
				},
				WebhookServer: webhook.NewServer(webhook.Options{
					Port:    testEnv.WebhookInstallOptions.LocalServingPort,
					CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
				}),
			})
			gs.Expect(err).ToNot(HaveOccurred())

			platformStatus := &osconfigv1.PlatformStatus{
				Type: tc.platformType,
				GCP: &osconfigv1.GCPPlatformStatus{
					ProjectID: "gcp-project-id",
				},
				AWS: &osconfigv1.AWSPlatformStatus{
					Region: "region",
				},
			}
			infra := plainInfra.DeepCopy()
			infra.Status.InfrastructureName = tc.clusterID
			infra.Status.PlatformStatus = platformStatus

			dns := plainDNS.DeepCopy()
			if !tc.disconnected {
				dns.Spec.PublicZone = &osconfigv1.DNSZone{}
			}
			machineDefaulter := admission.WithCustomDefaulter(scheme.Scheme, &machinev1beta1.Machine{}, createMachineDefaulter(platformStatus, tc.clusterID))
			machineValidator := admission.WithCustomValidator(scheme.Scheme, &machinev1beta1.Machine{}, createMachineValidator(infra, c, dns, gate))
			mgr.GetWebhookServer().Register(DefaultMachineMutatingHookPath, &webhook.Admission{Handler: machineDefaulter})
			mgr.GetWebhookServer().Register(DefaultMachineValidatingHookPath, &webhook.Admission{Handler: machineValidator})

			mgrCtx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(mgrCtx)).To(Succeed())
				close(stopped)
			}()
			defer func() {
				cancel()
				<-stopped
			}()

			gs.Eventually(func() (bool, error) {
				resp, err := insecureHTTPClient.Get(fmt.Sprintf("https://127.0.0.1:%d", testEnv.WebhookInstallOptions.LocalServingPort))
				if err != nil {
					return false, err
				}
				return resp.StatusCode == 404, nil
			}).Should(BeTrue())

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
				},
				Spec: machinev1beta1.MachineSpec{
					ProviderSpec: machinev1beta1.ProviderSpec{
						Value: tc.providerSpecValue,
					},
				},
			}

			presetClusterID := "anything"
			if tc.presetClusterID {
				m.Labels = make(map[string]string)
				m.Labels[machinev1beta1.MachineClusterIDLabel] = presetClusterID
			}

			err = c.Create(ctx, m)
			if err == nil {
				defer func() {
					gs.Expect(c.Delete(ctx, m)).To(Succeed())
				}()
			}

			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())

				statusError := &apierrors.StatusError{}
				gs.Expect(errors.As(err, &statusError)).To(BeTrue())

				gs.Expect(statusError.Status().Message).To(ContainSubstring(tc.expectedError))
			} else {
				if tc.presetClusterID {
					gs.Expect(m.Labels[machinev1beta1.MachineClusterIDLabel]).To(BeIdenticalTo(presetClusterID))
				} else {
					gs.Expect(m.Labels[machinev1beta1.MachineClusterIDLabel]).To(BeIdenticalTo(tc.clusterID))
				}
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestMachineUpdate(t *testing.T) {
	awsClusterID := "aws-cluster"
	awsRegion := "region"
	warnings := make([]string, 0)

	defaultAWSProviderSpec := &machinev1beta1.AWSMachineProviderConfig{
		AMI: machinev1beta1.AWSResourceReference{
			ID: ptr.To[string]("ami"),
		},
		InstanceType:      defaultInstanceTypeForCloudProvider(osconfigv1.AWSPlatformType, arch, &warnings),
		UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
		CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
		Placement: machinev1beta1.Placement{
			Region: awsRegion,
		},
	}

	azureClusterID := "azure-cluster"
	defaultAzureProviderSpec := &machinev1beta1.AzureMachineProviderSpec{
		Location:             "location",
		VMSize:               defaultInstanceTypeForCloudProvider(osconfigv1.AzurePlatformType, arch, &warnings),
		Vnet:                 defaultAzureVnet(azureClusterID),
		Subnet:               defaultAzureSubnet(azureClusterID),
		NetworkResourceGroup: defaultAzureNetworkResourceGroup(azureClusterID),
		Image: machinev1beta1.Image{
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
		OSDisk: machinev1beta1.OSDisk{
			DiskSizeGB:  128,
			OSType:      defaultAzureOSDiskOSType,
			CachingType: "ReadOnly",
			ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
				StorageAccountType: defaultAzureOSDiskStorageType,
			},
			DiskSettings: machinev1beta1.DiskSettings{
				EphemeralStorageLocation: "Local",
			},
		},
	}

	gcpClusterID := "gcp-cluster"
	defaultGCPProviderSpec := &machinev1beta1.GCPMachineProviderSpec{
		Region:      "region",
		Zone:        "region-zone",
		MachineType: defaultInstanceTypeForCloudProvider(osconfigv1.GCPPlatformType, arch, &warnings),
		NetworkInterfaces: []*machinev1beta1.GCPNetworkInterface{
			{
				Network:    defaultGCPNetwork(gcpClusterID),
				Subnetwork: defaultGCPSubnetwork(gcpClusterID),
			},
		},
		Disks: []*machinev1beta1.GCPDisk{
			{
				AutoDelete: true,
				Boot:       true,
				SizeGB:     defaultGCPDiskSizeGb,
				Type:       defaultGCPDiskType,
				Image:      defaultGCPDiskImage(),
			},
		},
		Tags: defaultGCPTags(gcpClusterID),
		UserDataSecret: &corev1.LocalObjectReference{
			Name: defaultUserDataSecret,
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: defaultGCPCredentialsSecret,
		},
	}

	vsphereClusterID := "vsphere-cluster"
	defaultVSphereProviderSpec := &machinev1beta1.VSphereMachineProviderSpec{
		Template: "template",
		Workspace: &machinev1beta1.Workspace{
			Datacenter: "datacenter",
			Server:     "server",
		},
		Network: machinev1beta1.NetworkSpec{
			Devices: []machinev1beta1.NetworkDeviceSpec{
				{
					NetworkName: "networkName",
				},
			},
		},
		UserDataSecret: &corev1.LocalObjectReference{
			Name: defaultUserDataSecret,
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: defaultVSphereCredentialsSecret,
		},
	}
	powerVSClusterID := "powerVS-cluster"
	defaultPowerVSProviderSpec := &machinev1.PowerVSMachineProviderConfig{
		ServiceInstance: machinev1.PowerVSResource{
			Type: machinev1.PowerVSResourceTypeName,
			Name: ptr.To[string]("testServiceInstanceID"),
		},
		Image: machinev1.PowerVSResource{
			Type: machinev1.PowerVSResourceTypeName,
			Name: ptr.To[string]("testImageName"),
		},
		Network: machinev1.PowerVSResource{
			Type: machinev1.PowerVSResourceTypeName,
			Name: ptr.To[string]("testNetworkName"),
		},
		UserDataSecret: &machinev1.PowerVSSecretReference{
			Name: defaultUserDataSecret,
		},
		CredentialsSecret: &machinev1.PowerVSSecretReference{
			Name: defaultPowerVSCredentialsSecret,
		},
		SystemType:    defaultPowerVSSysType,
		ProcessorType: defaultPowerVSProcType,
		Processors:    intstr.FromString(defaultPowerVSProcessor),
		MemoryGiB:     defaultPowerVSMemory,
		KeyPairName:   "test-keypair",
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

	awsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultAWSCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	vSphereSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultVSphereCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	GCPSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultGCPCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	azureSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultAzureCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
	}
	powerVSSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPowerVSCredentialsSecret,
			Namespace: defaultSecretNamespace,
		},
	}
	g.Expect(c.Create(ctx, awsSecret)).To(Succeed())
	g.Expect(c.Create(ctx, vSphereSecret)).To(Succeed())
	g.Expect(c.Create(ctx, GCPSecret)).To(Succeed())
	g.Expect(c.Create(ctx, azureSecret)).To(Succeed())
	g.Expect(c.Create(ctx, powerVSSecret)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, awsSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, vSphereSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, GCPSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, azureSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, powerVSSecret)).To(Succeed())
	}()

	preDrainHook := machinev1beta1.LifecycleHook{
		Name:  "pre-drain",
		Owner: "pre-drain-owner",
	}

	testCases := []struct {
		name                      string
		platformType              osconfigv1.PlatformType
		clusterID                 string
		expectedError             string
		baseMachineLifecycleHooks machinev1beta1.LifecycleHooks
		baseProviderSpecValue     *kruntime.RawExtension
		updatedProviderSpecValue  func() *kruntime.RawExtension
		updateAfterDelete         bool
		updateMachine             func(m *machinev1beta1.Machine)
	}{
		{
			name:         "with a valid AWS ProviderSpec",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				return &kruntime.RawExtension{
					Object: defaultAWSProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an AWS ProviderSpec, removing the instance type",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.InstanceType = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.instanceType: Required value: expected providerSpec.instanceType to be populated",
		},
		{
			name:         "with an AWS ProviderSpec, removing the region",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.Placement.Region = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.placement.region: Required value: expected providerSpec.placement.region to be populated",
		},
		{
			name:         "with an AWS ProviderSpec, removing the user data secret",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.UserDataSecret = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.userDataSecret: Required value: expected providerSpec.userDataSecret to be populated",
		},
		{
			name:         "with a valid Azure ProviderSpec",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				return &kruntime.RawExtension{
					Object: defaultAzureProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an Azure ProviderSpec, removing the vm size",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.VMSize = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.vmSize: Required value: vmSize should be set to one of the supported Azure VM sizes",
		},
		{
			name:         "with an Azure ProviderSpec, removing the subnet",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.Subnet = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.subnet: Required value: must provide a subnet when a virtual network is specified",
		},
		{
			name:         "with an Azure ProviderSpec, removing the credentials secret",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.CredentialsSecret = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			name:         "with an Azure ProviderSpec, removing caching type ",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    azureClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAzureProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultAzureProviderSpec.DeepCopy()
				object.OSDisk.CachingType = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.osDisk.cachingType: Invalid value: \"\": Instances using an ephemeral OS disk support only Readonly caching",
		},
		{
			name:         "with a valid GCP ProviderSpec",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				return &kruntime.RawExtension{
					Object: defaultGCPProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with a GCP ProviderSpec, removing the region",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Region = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with a GCP ProviderSpec, and an invalid region",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Zone = "zone"
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.zone: Invalid value: \"zone\": zone not in configured region (region)",
		},
		{
			name:         "with a GCP ProviderSpec, removing the disks",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.Disks = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.disks: Required value: at least 1 disk is required",
		},
		{
			name:         "with a GCP ProviderSpec, removing the network interfaces",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.NetworkInterfaces = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.networkInterfaces: Required value: at least 1 network interface is required",
		},
		{
			name:         "with a valid VSphere ProviderSpec",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				return &kruntime.RawExtension{
					Object: defaultVSphereProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with a valid VSphere ProviderSpec with a tag ID",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.TagIDs = []string{"urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9578:GLOBAL"}

				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "",
		},
		{
			name:         "with a valid VSphere ProviderSpec with an invalid tag ID",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.TagIDs = []string{"bad:tag:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9578:GLOBAL"}

				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.tagIDs: Required value: tag ID must be in the format of urn:vmomi:InventoryServiceTag:<UUID>:GLOBAL",
		},
		{
			name:         "with a valid VSphere ProviderSpec with too many tag IDs",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()

				for i := 0; i != 12; i++ {
					object.TagIDs = append(object.TagIDs, fmt.Sprintf("urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc95%02d:GLOBAL", i))
				}
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: `providerSpec.tagIDs: Invalid value: []string{"urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9500:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9501:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9502:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9503:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9504:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9505:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9506:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9507:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9508:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9509:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9510:GLOBAL", "urn:vmomi:InventoryServiceTag:5736bf56-49f5-4667-b38c-b97e09dc9511:GLOBAL"}: a maximum of 10 tags are allowed`,
		},
		{
			name:         "with an VSphere ProviderSpec, removing the template",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Template = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.template: Required value: template must be provided",
		},
		{
			name:         "with an VSphere ProviderSpec, removing the workspace server",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Workspace.Server = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.workspace.server: Required value: server must be provided",
		},
		{
			name:         "with an VSphere ProviderSpec, removing the network devices",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Network = machinev1beta1.NetworkSpec{}
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
		},
		{
			name:         "with an VSphere ProviderSpec, having more than 10 network devices",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Network = machinev1beta1.NetworkSpec{
					Devices: []machinev1beta1.NetworkDeviceSpec{
						{
							NetworkName: "networkName1",
						},
						{
							NetworkName: "networkName2",
						},
						{
							NetworkName: "networkName3",
						},
						{
							NetworkName: "networkName4",
						},
						{
							NetworkName: "networkName5",
						},
						{
							NetworkName: "networkName6",
						},
						{
							NetworkName: "networkName7",
						},
						{
							NetworkName: "networkName8",
						},
						{
							NetworkName: "networkName9",
						},
						{
							NetworkName: "networkName10",
						},
						{
							NetworkName: "networkName11",
						},
					},
				}
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.devices: Too many: 11: must have at most 10 items",
		},
		{
			name:         "when adding a lifecycle hook",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updateMachine: func(m *machinev1beta1.Machine) {
				m.Spec.LifecycleHooks.PreDrain = []machinev1beta1.LifecycleHook{preDrainHook}
			},
		},
		{
			name:         "when adding a lifecycle hook after the machine has been deleted",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updateAfterDelete: true,
			updateMachine: func(m *machinev1beta1.Machine) {
				m.Spec.LifecycleHooks.PreDrain = []machinev1beta1.LifecycleHook{preDrainHook}
			},
			expectedError: "spec.lifecycleHooks.preDrain: Forbidden: pre-drain hooks are immutable when machine is marked for deletion: the following hooks are new or changed: [{Name:pre-drain Owner:pre-drain-owner}]",
		},
		{
			name:         "when removing a lifecycle hook after the machine has been deleted",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			baseMachineLifecycleHooks: machinev1beta1.LifecycleHooks{
				PreDrain: []machinev1beta1.LifecycleHook{preDrainHook},
			},
			updateAfterDelete: true,
			updateMachine: func(m *machinev1beta1.Machine) {
				m.Spec.LifecycleHooks = machinev1beta1.LifecycleHooks{}
			},
		},
		{
			name:         "when duplicating a lifecycle hook",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updateMachine: func(m *machinev1beta1.Machine) {
				m.Spec.LifecycleHooks.PreDrain = []machinev1beta1.LifecycleHook{preDrainHook, preDrainHook}
			},
			expectedError: "spec.lifecycleHooks.preDrain[1]: Duplicate value: map[string]interface {}{\"name\":\"pre-drain\"}", // This is an openapi error. As lifecycleHooks have list-type=map, the API server will prevent duplication
		},
		{
			name:         "with a valid PowerVS ProviderSpec",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				return &kruntime.RawExtension{
					Object: defaultPowerVSProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the serviceInstanceID",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.ServiceInstance = machinev1.PowerVSResource{}
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.serviceInstance: Required value: serviceInstance identifier must be provided",
		},
		{
			name:         "with a PowerVS ProviderSpec, removing the UserDataSecret",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.UserDataSecret = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.userDataSecret: Required value: providerSpec.userDataSecret must be provided",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the CredentialsSecret",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.CredentialsSecret = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.credentialsSecret: Required value: providerSpec.credentialsSecret must be provided",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the keyPairName",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.KeyPairName = ""
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.keyPairName: Required value: providerSpec.keyPairName must be provided",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the Image Name",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.Image.Name = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.image.name: Required value: image identifier is specified as Name but the value is nil",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the Network Name",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &kruntime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *kruntime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.Network.Name = nil
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.name: Required value: network identifier is specified as Name but the value is nil",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			mgr, err := manager.New(cfg, manager.Options{
				Metrics: metricsserver.Options{
					BindAddress: "0",
				},
				WebhookServer: webhook.NewServer(webhook.Options{
					Port:    testEnv.WebhookInstallOptions.LocalServingPort,
					CertDir: testEnv.WebhookInstallOptions.LocalServingCertDir,
				}),
			})
			gs.Expect(err).ToNot(HaveOccurred())

			platformStatus := &osconfigv1.PlatformStatus{
				Type: tc.platformType,
				AWS: &osconfigv1.AWSPlatformStatus{
					Region: awsRegion,
				},
			}

			infra := &osconfigv1.Infrastructure{
				Status: osconfigv1.InfrastructureStatus{
					InfrastructureName: tc.clusterID,
					PlatformStatus:     platformStatus,
				},
			}
			machineDefaulter := admission.WithCustomDefaulter(scheme.Scheme, &machinev1beta1.Machine{}, createMachineDefaulter(platformStatus, tc.clusterID))
			machineValidator := admission.WithCustomValidator(scheme.Scheme, &machinev1beta1.Machine{}, createMachineValidator(infra, c, plainDNS, gate))
			mgr.GetWebhookServer().Register(DefaultMachineMutatingHookPath, &webhook.Admission{Handler: machineDefaulter})
			mgr.GetWebhookServer().Register(DefaultMachineValidatingHookPath, &webhook.Admission{Handler: machineValidator})

			mgrCtx, cancel := context.WithCancel(context.Background())
			stopped := make(chan struct{})
			go func() {
				gs.Expect(mgr.Start(mgrCtx)).To(Succeed())
				close(stopped)
			}()
			defer func() {
				cancel()
				<-stopped
			}()

			gs.Eventually(func() (bool, error) {
				resp, err := insecureHTTPClient.Get(fmt.Sprintf("https://127.0.0.1:%d", testEnv.WebhookInstallOptions.LocalServingPort))
				if err != nil {
					return false, err
				}
				return resp.StatusCode == 404, nil
			}).Should(BeTrue())

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machine-creation-",
					Namespace:    namespace.Name,
					Finalizers: []string{
						"machine-test",
					},
				},
				Spec: machinev1beta1.MachineSpec{
					LifecycleHooks: tc.baseMachineLifecycleHooks,
					ProviderSpec: machinev1beta1.ProviderSpec{
						Value: tc.baseProviderSpecValue,
					},
				},
			}
			err = c.Create(ctx, m)
			gs.Expect(err).ToNot(HaveOccurred())
			if tc.updateAfterDelete {
				gs.Expect(c.Delete(ctx, m)).To(Succeed())
			} else {
				defer func() {
					gs.Expect(c.Delete(ctx, m)).To(Succeed())
				}()
			}

			key := client.ObjectKey{Namespace: m.Namespace, Name: m.Name}
			defer func() {
				mc := &machinev1beta1.Machine{}
				gs.Expect(c.Get(ctx, key, mc)).To(Succeed())
				mc.Finalizers = []string{}
				gs.Expect(c.Update(ctx, mc)).To(Succeed())
			}()

			gs.Expect(c.Get(ctx, key, m)).To(Succeed())
			if tc.updatedProviderSpecValue != nil {
				m.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			}
			if tc.updateMachine != nil {
				tc.updateMachine(m)
			}
			err = c.Update(ctx, m)
			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())

				statusError := &apierrors.StatusError{}
				gs.Expect(errors.As(err, &statusError)).To(BeTrue())

				gs.Expect(statusError.Status().Message).To(ContainSubstring(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestValidateAWSProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aws-validation-test",
		},
	}

	testCases := []struct {
		testCase         string
		modifySpec       func(*machinev1beta1.AWSMachineProviderConfig)
		overrideRawBytes []byte
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with no ami values it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.AMI = machinev1beta1.AWSResourceReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.ami: Required value: expected providerSpec.ami.id to be populated",
		},
		{
			testCase: "with no region values it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Placement.Region = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.placement.region: Required value: expected providerSpec.placement.region to be populated",
		},
		{
			testCase: "with no instanceType it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.InstanceType = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.instanceType: Required value: expected providerSpec.instanceType to be populated",
		},
		{
			testCase: "with no user data secret it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: expected providerSpec.userDataSecret to be populated",
		},
		{
			testCase: "with no user data secret name it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.UserDataSecret.Name = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: expected providerSpec.userDataSecret.name to be provided",
		},
		{
			testCase: "with no credentials secret it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no subnet values it fails",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Subnet = machinev1beta1.AWSResourceReference{}
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.subnet: No subnet has been provided. Instances may be created in an unexpected subnet and may not join the cluster."},
		},
		{
			testCase:      "with all required values it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with valid tenancy field",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Placement.Tenancy = machinev1beta1.DedicatedTenancy
			},
			expectedOk: true,
		},
		{
			testCase: "with empty tenancy field",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Placement.Tenancy = ""
			},
			expectedOk: true,
		},
		{
			testCase: "fail with invalid tenancy field",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Placement.Tenancy = "invalid"
			},
			expectedOk:    false,
			expectedError: "providerSpec.tenancy: Invalid value: \"invalid\": Invalid providerSpec.tenancy, the only allowed options are: default, dedicated, host",
		},
		{
			testCase: "fail if placementGroupPartition is set, but placementGroupName is empty",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.PlacementGroupName = ""
				partition := int32(2)
				p.PlacementGroupPartition = &partition
			},
			expectedOk:    false,
			expectedError: "providerSpec.placementGroupPartition: Invalid value: 2: providerSpec.placementGroupPartition is set but providerSpec.placementGroupName is empty",
		},
		{
			testCase: "fail if placementGroupPartition is outside 1-7 range (lower)",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.PlacementGroupName = "placement-group"
				partition := int32(0)
				p.PlacementGroupPartition = &partition
			},
			expectedOk:    false,
			expectedError: "providerSpec.placementGroupPartition: Invalid value: 0: providerSpec.placementGroupPartition must be between 1 and 7",
		},
		{
			testCase: "fail if placementGroupPartition is outside 1-7 range (upper)",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.PlacementGroupName = "placement-group"
				partition := int32(8)
				p.PlacementGroupPartition = &partition
			},
			expectedOk:    false,
			expectedError: "providerSpec.placementGroupPartition: Invalid value: 8: providerSpec.placementGroupPartition must be between 1 and 7",
		},
		{
			testCase: "allow if only placementGroupName is set",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.PlacementGroupName = "placement-group"
			},
			expectedOk: true,
		},
		{
			testCase: "allow if correct placementGroupName and placementGroupPartition are set",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.PlacementGroupName = "placement-group"
				partition := int32(2)
				p.PlacementGroupPartition = &partition
			},
			expectedOk: true,
		},
		{
			testCase: "with no iam instance profile",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.IAMInstanceProfile = nil
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.iamInstanceProfile: no IAM instance profile provided: nodes may be unable to join the cluster"},
		},
		{
			testCase: "with double tag names, lists duplicated tags",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Tags = []machinev1beta1.TagSpecification{
					{
						Name: "Tag-A",
					},
					{
						Name: "Tag-B",
					},
					{
						Name: "Tag-C",
					},
					{
						Name: "Tag-A",
					},
					{
						Name: "Tag-B",
					},
				}
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.tags: duplicated tag names (Tag-A,Tag-B): only the first value will be used."},
		},
		{
			testCase: "with triplicated tag names, lists duplicated tag",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Tags = []machinev1beta1.TagSpecification{
					{
						Name: "Tag-A",
					},
					{
						Name: "Tag-A",
					},
					{
						Name: "Tag-A",
					},
				}
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.tags: duplicated tag names (Tag-A): only the first value will be used."},
		},
		{
			testCase: "with alternately cased tag names, AWS tags are case sensitive, does not list duplicated tags",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Tags = []machinev1beta1.TagSpecification{
					{
						Name: "Tag-A",
					},
					{
						Name: "Tag-a",
					},
					{
						Name: "tag-a",
					},
				}
			},
			expectedOk: true,
		},
		{
			testCase: "with AMI ARN set",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.AMI = machinev1beta1.AWSResourceReference{
					ID:  ptr.To[string]("ami"),
					ARN: ptr.To[string]("arn"),
				}
			},
			expectedOk:       true,
			expectedWarnings: []string{"can't use providerSpec.ami.arn, only providerSpec.ami.id can be used to reference AMI"},
		},
		{
			testCase: "with AMI filters set",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.AMI = machinev1beta1.AWSResourceReference{
					ID: ptr.To[string]("ami"),
					Filters: []machinev1beta1.Filter{
						{
							Name: "filter",
						},
					},
				}
			},
			expectedOk:       true,
			expectedWarnings: []string{"can't use providerSpec.ami.filters, only providerSpec.ami.id can be used to reference AMI"},
		},
		{
			testCase: "with a valid NetworkInterfaceType",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.NetworkInterfaceType = machinev1beta1.AWSEFANetworkInterfaceType
			},
			expectedOk: true,
		},
		{
			testCase: "with an invalid NetworkInterfaceType",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.NetworkInterfaceType = "efa"
			},
			expectedOk:    false,
			expectedError: "providerSpec.networkInterfaceType: Invalid value: \"efa\": Valid values are: ENA, EFA and omitted",
		},
		{
			testCase: "valid metadataServiceOptions",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.MetadataServiceOptions.Authentication = "Required"
			},
			expectedOk: true,
		},
		{
			testCase: "with invalid metadataServiceOptions",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.MetadataServiceOptions.Authentication = "Boom"
			},
			expectedOk:    false,
			expectedError: "providerSpec.metadataServiceOptions.authentication: Invalid value: \"Boom\": Allowed values are either 'Optional' or 'Required'",
		},
		{
			testCase: "with cpuOptions empty",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CPUOptions = &machinev1beta1.CPUOptions{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.CPUOptions: Invalid value: \"{}\": At least one field must be set if cpuOptions is provided",
		},
		{
			testCase: "with confidentialCompute set to AMD SEV-SNP",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CPUOptions = &machinev1beta1.CPUOptions{
					ConfidentialCompute: ptr.To(machinev1beta1.AWSConfidentialComputePolicySEVSNP),
				}
			},
			expectedOk: true,
		},
		{
			testCase: "with confidentialCompute disabled",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CPUOptions = &machinev1beta1.CPUOptions{
					ConfidentialCompute: ptr.To(machinev1beta1.AWSConfidentialComputePolicyDisabled),
				}
			},
			expectedOk: true,
		},
		{
			testCase: "with confidentialCompute set to invalid value",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CPUOptions = &machinev1beta1.CPUOptions{
					ConfidentialCompute: ptr.To(machinev1beta1.AWSConfidentialComputePolicy("invalid")),
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.CPUOptions.ConfidentialCompute: Invalid value: \"invalid\": Allowed values are Disabled, AMDEncryptedVirtualizationNestedPaging and omitted",
		},
		{
			testCase: "with confidentialCompute empty",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.CPUOptions = &machinev1beta1.CPUOptions{
					ConfidentialCompute: ptr.To(machinev1beta1.AWSConfidentialComputePolicy("")),
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.CPUOptions.ConfidentialCompute: Invalid value: \"\": Allowed values are Disabled, AMDEncryptedVirtualizationNestedPaging and omitted",
		},
		{
			testCase: "with invalid GroupVersionKind",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Kind = "INVALID"
				p.APIVersion = "INVALID/v1"
			},
			expectedOk:       true,
			expectedWarnings: []string{"incorrect GroupVersionKind for AWSMachineProviderConfig object: INVALID/v1, Kind=INVALID"},
		},
		{
			testCase: "with machine.openshift.io API group",
			modifySpec: func(p *machinev1beta1.AWSMachineProviderConfig) {
				p.Kind = "AWSMachineProviderConfig"
				p.APIVersion = "machine.openshift.io/v1beta1"
			},
			expectedOk: true,
		},
		{
			testCase:         "with unknown fields in the providerSpec",
			overrideRawBytes: []byte(`{"kind":"AWSMachineProviderConfig","apiVersion":"machine.openshift.io/v1beta1","metadata":{"creationTimestamp":null},"ami":{"id":"ami"},"instanceType":"m5.large","iamInstanceProfile":{"id":"profileID"},"userDataSecret":{"name":"secret"},"credentialsSecret":{"name":"secret"},"deviceIndex":0,"securityGroups":[{"id":"sg"}],"subnet":{"id":"subnet"},"placement":{"region":"region"},"metadataServiceOptions":{},"randomField-1": "something"}`),
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.value: Unsupported value: \"randomField-1\": Unknown field (randomField-1) will be ignored"},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()

	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.AWSPlatformType

	gate, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Errorf("Unexpected error setting up feature gates: %v", err)
	}

	h := createMachineValidator(infra, c, plainDNS, gate)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &machinev1beta1.AWSMachineProviderConfig{
				AMI: machinev1beta1.AWSResourceReference{
					ID: ptr.To[string]("ami"),
				},
				Placement: machinev1beta1.Placement{
					Region: "region",
				},
				InstanceType: "m5.large",
				IAMInstanceProfile: &machinev1beta1.AWSResourceReference{
					ID: ptr.To[string]("profileID"),
				},
				UserDataSecret: &corev1.LocalObjectReference{
					Name: "secret",
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "secret",
				},
				SecurityGroups: []machinev1beta1.AWSResourceReference{
					{
						ID: ptr.To[string]("sg"),
					},
				},
				Subnet: machinev1beta1.AWSResourceReference{
					ID: ptr.To[string]("subnet"),
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "AWSMachineProviderConfig",
					APIVersion: "awsproviderconfig.openshift.io/v1beta1",
				},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
			if tc.overrideRawBytes != nil {
				m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: tc.overrideRawBytes}
			}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultAWSProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	region := "region"
	itWarnings := make([]string, 0)
	instanceType := defaultInstanceTypeForCloudProvider(osconfigv1.AWSPlatformType, arch, &itWarnings)
	testCases := []struct {
		testCase             string
		providerSpec         *machinev1beta1.AWSMachineProviderConfig
		expectedProviderSpec *machinev1beta1.AWSMachineProviderConfig
		expectedError        string
		expectedOk           bool
		expectedWarnings     []string
	}{
		{
			testCase: "it defaults Region, InstanceType, UserDataSecret and CredentialsSecret",
			providerSpec: &machinev1beta1.AWSMachineProviderConfig{
				AMI:               machinev1beta1.AWSResourceReference{},
				InstanceType:      "",
				UserDataSecret:    nil,
				CredentialsSecret: nil,
			},
			expectedProviderSpec: &machinev1beta1.AWSMachineProviderConfig{
				AMI:               machinev1beta1.AWSResourceReference{},
				InstanceType:      instanceType,
				UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
				CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
				Placement: machinev1beta1.Placement{
					Region: "region",
				},
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{
		Type: osconfigv1.AWSPlatformType,
		AWS: &osconfigv1.AWSPlatformStatus{
			Region: region,
		},
	}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			m := &machinev1beta1.Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(machinev1beta1.AWSMachineProviderConfig)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(tc.expectedProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", tc.expectedProviderSpec, gotProviderSpec)
			}
			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestValidateAzureProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "azure-validation-test",
		},
	}

	testCases := []struct {
		testCase            string
		modifySpec          func(providerSpec *machinev1beta1.AzureMachineProviderSpec)
		overrideRawBytes    []byte
		azurePlatformStatus *osconfigv1.AzurePlatformStatus
		expectedError       string
		expectedOk          bool
		expectedWarnings    []string
	}{
		{
			testCase: "with no vmsize it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.VMSize = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.vmSize: Required value: vmSize should be set to one of the supported Azure VM sizes",
		},
		{
			testCase: "with ephemeral storage but no caching type it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.OSDisk.CachingType = ""
				p.OSDisk.DiskSettings.EphemeralStorageLocation = "Local"
			},
			expectedOk:    false,
			expectedError: "providerSpec.osDisk.cachingType: Invalid value: \"\": Instances using an ephemeral OS disk support only Readonly caching",
		},
		{
			testCase: "with a vnet but no subnet it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Vnet = "vnet"
				p.Subnet = ""
				p.NetworkResourceGroup = "nrg"
			},
			expectedOk:    false,
			expectedError: "providerSpec.subnet: Required value: must provide a subnet when a virtual network is specified",
		},
		{
			testCase: "with a subnet but no vnet it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Vnet = ""
				p.Subnet = "subnet"
				p.NetworkResourceGroup = "nrg"
			},
			expectedOk:    false,
			expectedError: "providerSpec.vnet: Required value: must provide a virtual network when supplying subnets",
		},
		{
			testCase: "with no image it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image: Required value: an image reference must be provided",
		},
		{
			testCase: "with resourceId and other fields set it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					ResourceID: "rid",
					SKU:        "sku-rand",
					Offer:      "base-offer",
					Version:    "1",
					Publisher:  "test",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image.resourceID: Required value: resourceID is already specified, other fields such as [Offer, Publisher, SKU, Version] should not be set",
		},
		{
			testCase: "with no offer it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					Version:   "1",
					SKU:       "sku-rand",
					Publisher: "test",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image.Offer: Required value: Offer must be provided",
		},
		{
			testCase: "with no SKU it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					Offer:     "base-offer",
					Version:   "1",
					Publisher: "test",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image.SKU: Required value: SKU must be provided",
		},
		{
			testCase: "with no Version it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					SKU:       "sku-rand",
					Offer:     "base-offer",
					Publisher: "test",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image.Version: Required value: Version must be provided",
		},
		{
			testCase: "with no Publisher it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					SKU:     "sku-rand",
					Offer:   "base-offer",
					Version: "1",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image.Publisher: Required value: Publisher must be provided",
		},
		{
			testCase: "with resourceID in image it succeeds",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					ResourceID: "rid",
				}
			},
			expectedOk: true,
		},
		{
			testCase: "with no user data secret it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.UserDataSecret = &corev1.SecretReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials secret it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "with no credentials secret namespace it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.CredentialsSecret = &corev1.SecretReference{
					Name: "name",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret.namespace: Required value: namespace must be provided",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no credentials secret name it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.CredentialsSecret = &corev1.SecretReference{
					Namespace: namespace.Name,
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no os disk size it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.OSDisk = machinev1beta1.OSDisk{
					OSType: "osType",
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						StorageAccountType: "storageAccountType",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero and less than 32768",
		},
		{
			testCase: "with no securityProfile and osDisk.managedDisk.securityProfile.securityEncryptionType defined it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = nil
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						SecurityProfile: machinev1beta1.VMDiskSecurityProfile{
							SecurityEncryptionType: machinev1beta1.SecurityEncryptionTypesVMGuestStateOnly,
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.securityProfile: Required value: securityProfile should be set when osDisk.managedDisk.securityProfile.securityEncryptionType is defined.",
		},
		{
			testCase: "with securityType set to ConfidentialVM and no osDisk.managedDisk.securityProfile.securityEncryptionType it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						SecurityType: machinev1beta1.SecurityTypesConfidentialVM,
					},
				}
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.osDisk.managedDisk.securityProfile.securityEncryptionType: Required value: securityEncryptionType should be set when securityType is set to %s.", machinev1beta1.SecurityTypesConfidentialVM),
		},
		{
			testCase: "with securityType set to ConfidentialVM and no securityProfile.settings.confidentialVM it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						SecurityType: machinev1beta1.SecurityTypesConfidentialVM,
					},
				}
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						SecurityProfile: machinev1beta1.VMDiskSecurityProfile{
							SecurityEncryptionType: machinev1beta1.SecurityEncryptionTypesVMGuestStateOnly,
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.settings.confidentialVM: Required value: confidentialVM should be set when securityType is set to %s.", machinev1beta1.SecurityTypesConfidentialVM),
		},
		{
			testCase: "with securityType set to ConfidentialVM and virtualizedTrustedPlatformModule disabled it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						SecurityType: machinev1beta1.SecurityTypesConfidentialVM,
						ConfidentialVM: &machinev1beta1.ConfidentialVM{
							UEFISettings: machinev1beta1.UEFISettings{
								VirtualizedTrustedPlatformModule: machinev1beta1.VirtualizedTrustedPlatformModulePolicyDisabled,
							},
						},
					},
				}
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						SecurityProfile: machinev1beta1.VMDiskSecurityProfile{
							SecurityEncryptionType: machinev1beta1.SecurityEncryptionTypesVMGuestStateOnly,
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.settings.confidentialVM.uefiSettings.virtualizedTrustedPlatformModule: Invalid value: \"Disabled\": virtualizedTrustedPlatformModule should be enabled when securityType is set to %s.", machinev1beta1.SecurityTypesConfidentialVM),
		},
		{
			testCase: "with securityEncryptionType set to DiskWithVMGuestState and encryptionAtHost enabled it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					EncryptionAtHost: ptr.To[bool](true),
					Settings: machinev1beta1.SecuritySettings{
						SecurityType: machinev1beta1.SecurityTypesConfidentialVM,
						ConfidentialVM: &machinev1beta1.ConfidentialVM{
							UEFISettings: machinev1beta1.UEFISettings{
								VirtualizedTrustedPlatformModule: machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled,
							},
						},
					},
				}
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						SecurityProfile: machinev1beta1.VMDiskSecurityProfile{
							SecurityEncryptionType: machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState,
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.encryptionAtHost: Invalid value: true: encryptionAtHost cannot be set to true when securityEncryptionType is set to %s.", machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState),
		},
		{
			testCase: "with securityEncryptionType set to DiskWithVMGuestState and secureBoot disabled it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						SecurityType: machinev1beta1.SecurityTypesConfidentialVM,
						ConfidentialVM: &machinev1beta1.ConfidentialVM{
							UEFISettings: machinev1beta1.UEFISettings{
								VirtualizedTrustedPlatformModule: machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled,
								SecureBoot:                       machinev1beta1.SecureBootPolicyDisabled,
							},
						},
					},
				}
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						SecurityProfile: machinev1beta1.VMDiskSecurityProfile{
							SecurityEncryptionType: machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState,
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.settings.confidentialVM.uefiSettings.secureBoot: Invalid value: \"Disabled\": secureBoot should be enabled when securityEncryptionType is set to %s.", machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState),
		},
		{
			testCase: "with securityEncryptionType and securityType not ConfidentialVM it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						ConfidentialVM: &machinev1beta1.ConfidentialVM{
							UEFISettings: machinev1beta1.UEFISettings{
								VirtualizedTrustedPlatformModule: machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled,
							},
						},
					},
				}
				p.OSDisk = machinev1beta1.OSDisk{
					DiskSizeGB: 1,
					ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
						SecurityProfile: machinev1beta1.VMDiskSecurityProfile{
							SecurityEncryptionType: machinev1beta1.SecurityEncryptionTypesDiskWithVMGuestState,
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.settings.securityType: Invalid value: \"\": securityType should be set to %s when securityEncryptionType is defined.", machinev1beta1.SecurityTypesConfidentialVM),
		},
		{
			testCase: "with securityType set to TrustedLaunch and no securityProfile.settings.trustedLaunch it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						SecurityType: machinev1beta1.SecurityTypesTrustedLaunch,
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.settings.trustedLaunch: Required value: trustedLaunch should be set when securityType is set to %s.", machinev1beta1.SecurityTypesTrustedLaunch),
		},
		{
			testCase: "with secureBoot enabled and securityType not set to TrustedLaunch it fails",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SecurityProfile = &machinev1beta1.SecurityProfile{
					Settings: machinev1beta1.SecuritySettings{
						TrustedLaunch: &machinev1beta1.TrustedLaunch{
							UEFISettings: machinev1beta1.UEFISettings{
								SecureBoot: machinev1beta1.SecureBootPolicyEnabled,
							},
						},
					},
				}
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.securityProfile.settings.securityType: Invalid value: \"\": securityType should be set to %s when uefiSettings are enabled.", machinev1beta1.SecurityTypesTrustedLaunch),
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with government cloud and spot VMs enabled",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SpotVMOptions = &machinev1beta1.SpotVMOptions{}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzureUSGovernmentCloud,
			},
			expectedOk:       true,
			expectedWarnings: []string{"spot VMs may not be supported when using GovCloud region"},
		},
		{
			testCase: "with public cloud and spot VMs enabled",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.SpotVMOptions = &machinev1beta1.SpotVMOptions{}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk: true,
		},
		{
			testCase: "with Azure Managed boot diagnostics",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Diagnostics.Boot = &machinev1beta1.AzureBootDiagnostics{
					StorageAccountType: machinev1beta1.AzureManagedAzureDiagnosticsStorage,
				}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk: true,
		},
		{
			testCase: "with Customer Managed boot diagnostics",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Diagnostics.Boot = &machinev1beta1.AzureBootDiagnostics{
					StorageAccountType: machinev1beta1.CustomerManagedAzureDiagnosticsStorage,
					CustomerManaged: &machinev1beta1.AzureCustomerManagedBootDiagnostics{
						StorageAccountURI: "https://storageaccount.blob.core.windows.net/",
					},
				}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk: true,
		},
		{
			testCase: "with Azure Managed boot diagnostics, with a Customer Managed configuration",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Diagnostics.Boot = &machinev1beta1.AzureBootDiagnostics{
					StorageAccountType: machinev1beta1.AzureManagedAzureDiagnosticsStorage,
					CustomerManaged: &machinev1beta1.AzureCustomerManagedBootDiagnostics{
						StorageAccountURI: "https://storageaccount.blob.core.windows.net/",
					},
				}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk:    false,
			expectedError: "providerSpec.diagnostics.boot.customerManaged: Invalid value: v1beta1.AzureCustomerManagedBootDiagnostics{StorageAccountURI:\"https://storageaccount.blob.core.windows.net/\"}: customerManaged may not be set when type is AzureManaged",
		},
		{
			testCase: "with Customer Managed boot diagnostics, with a missing storage account URI",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Diagnostics.Boot = &machinev1beta1.AzureBootDiagnostics{
					StorageAccountType: machinev1beta1.CustomerManagedAzureDiagnosticsStorage,
					CustomerManaged:    &machinev1beta1.AzureCustomerManagedBootDiagnostics{},
				}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk:    false,
			expectedError: "providerSpec.diagnostics.boot.customerManaged.storageAccountURI: Required value: storageAccountURI must be provided",
		},
		{
			testCase: "with an invalid boot diagnostics storage account type",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Diagnostics.Boot = &machinev1beta1.AzureBootDiagnostics{
					StorageAccountType: machinev1beta1.AzureBootDiagnosticsStorageAccountType("invalid"),
				}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk:    false,
			expectedError: "providerSpec.diagnostics.boot.storageAccountType: Invalid value: \"invalid\": storageAccountType must be one of: AzureManaged, CustomerManaged",
		},
		{
			testCase: "with invalid GroupVersionKind",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Kind = "INVALID"
				p.APIVersion = "INVALID/v1"
			},
			expectedOk:       true,
			expectedWarnings: []string{"incorrect GroupVersionKind for AzureMachineProviderSpec object: INVALID/v1, Kind=INVALID"},
		},
		{
			testCase: "with machine.openshift.io API group",
			modifySpec: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Kind = "AzureMachineProviderSpec"
				p.APIVersion = "machine.openshift.io/v1beta1"
			},
			expectedOk: true,
		},
		{
			testCase:         "with unknown fields in the providerSpec",
			overrideRawBytes: []byte(`{"kind":"AzureMachineProviderSpec","apiVersion":"machine.openshift.io/v1beta1","metadata":{"creationTimestamp":null},"userDataSecret":{"name":"name"},"credentialsSecret":{"name":"name","namespace":"azure-validation-test"},"vmSize":"vmSize","image":{"publisher":"","offer":"","sku":"","version":"","resourceID":"resourceID"},"osDisk":{"osType":"","managedDisk":{"storageAccountType":""},"diskSizeGB":1,"diskSettings":{}},"publicIP":false,"subnet":"","diagnostics":{},"randomField-1": "something"}`),
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.value: Unsupported value: \"randomField-1\": Unknown field (randomField-1) will be ignored"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "name",
					Namespace: namespace.Name,
				},
			}
			c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()
			infra := plainInfra.DeepCopy()
			infra.Status.InfrastructureName = "clusterID"
			infra.Status.PlatformStatus.Type = osconfigv1.AzurePlatformType
			infra.Status.PlatformStatus.Azure = tc.azurePlatformStatus

			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			h := createMachineValidator(infra, c, plainDNS, gate)

			// create a valid spec that will then be 'broken' by modifySpec
			providerSpec := &machinev1beta1.AzureMachineProviderSpec{
				VMSize: "vmSize",
				Image: machinev1beta1.Image{
					ResourceID: "resourceID",
				},
				UserDataSecret: &corev1.SecretReference{
					Name: "name",
				},
				CredentialsSecret: &corev1.SecretReference{
					Name:      "name",
					Namespace: namespace.Name,
				},
				OSDisk: machinev1beta1.OSDisk{
					DiskSizeGB: 1,
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "AzureMachineProviderSpec",
					APIVersion: "azureproviderconfig.openshift.io/v1beta1",
				},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
			if tc.overrideRawBytes != nil {
				m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: tc.overrideRawBytes}
			}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultAzureProviderSpec(t *testing.T) {
	itWarnings := make([]string, 0)
	defaultInstanceType := defaultInstanceTypeForCloudProvider(osconfigv1.AzurePlatformType, arch, &itWarnings)
	clusterID := "clusterID"

	testCases := []struct {
		testCase         string
		providerSpec     *machinev1beta1.AzureMachineProviderSpec
		modifyDefault    func(*machinev1beta1.AzureMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:         "it defaults defaultable fields",
			providerSpec:     &machinev1beta1.AzureMachineProviderSpec{},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "it does not override azure image spec",
			providerSpec: &machinev1beta1.AzureMachineProviderSpec{
				Image: machinev1beta1.Image{
					Offer:     "test-offer",
					SKU:       "test-sku",
					Publisher: "base-publisher",
					Version:   "1",
				},
			},
			modifyDefault: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					Offer:     "test-offer",
					SKU:       "test-sku",
					Publisher: "base-publisher",
					Version:   "1",
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "it does not override azure image ResourceID",
			providerSpec: &machinev1beta1.AzureMachineProviderSpec{
				Image: machinev1beta1.Image{
					ResourceID: "rid",
				},
			},
			modifyDefault: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.Image = machinev1beta1.Image{
					ResourceID: "rid",
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "does not overwrite the network resource group if it already exists",
			providerSpec: &machinev1beta1.AzureMachineProviderSpec{
				NetworkResourceGroup: "nrg",
			},
			modifyDefault: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.NetworkResourceGroup = "nrg"
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "does not overwrite the credentials secret namespace if they already exist",
			providerSpec: &machinev1beta1.AzureMachineProviderSpec{
				CredentialsSecret: &corev1.SecretReference{
					Namespace: "foo",
				},
			},
			modifyDefault: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.CredentialsSecret.Namespace = "foo"
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "does not overwrite the secret names if they already exist",
			providerSpec: &machinev1beta1.AzureMachineProviderSpec{
				UserDataSecret: &corev1.SecretReference{
					Name: "foo",
				},
				CredentialsSecret: &corev1.SecretReference{
					Name: "foo",
				},
			},
			modifyDefault: func(p *machinev1beta1.AzureMachineProviderSpec) {
				p.UserDataSecret.Name = "foo"
				p.CredentialsSecret.Name = "foo"
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.AzurePlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)
	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			defaultProviderSpec := &machinev1beta1.AzureMachineProviderSpec{
				VMSize: defaultInstanceType,
				Vnet:   defaultAzureVnet(clusterID),
				Subnet: defaultAzureSubnet(clusterID),
				Image: machinev1beta1.Image{
					ResourceID: defaultAzureImageResourceID(clusterID),
				},
				UserDataSecret: &corev1.SecretReference{
					Name: defaultUserDataSecret,
				},
				CredentialsSecret: &corev1.SecretReference{
					Name:      defaultAzureCredentialsSecret,
					Namespace: defaultSecretNamespace,
				},
			}
			if tc.modifyDefault != nil {
				tc.modifyDefault(defaultProviderSpec)
			}

			m := &machinev1beta1.Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(machinev1beta1.AzureMachineProviderSpec)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestValidateGCPProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gcp-validation-test",
		},
	}

	testCases := []struct {
		testCase         string
		modifySpec       func(*machinev1beta1.GCPMachineProviderSpec)
		overrideRawBytes []byte
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with no region",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Region = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			testCase: "with no zone",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Zone = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.zone: Invalid value: \"\": zone not in configured region (region)",
		},
		{
			testCase: "with an invalid zone",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Zone = "zone"
			},
			expectedOk:    false,
			expectedError: "providerSpec.zone: Invalid value: \"zone\": zone not in configured region (region)",
		},
		{
			testCase: "with no machine type",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.MachineType = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.machineType: Required value: machineType should be set to one of the supported GCP machine types",
		},
		{
			testCase: "with no network interfaces",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.NetworkInterfaces = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.networkInterfaces: Required value: at least 1 network interface is required",
		},
		{
			testCase: "with a network interfaces is missing the network",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.NetworkInterfaces = []*machinev1beta1.GCPNetworkInterface{
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
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.NetworkInterfaces = []*machinev1beta1.GCPNetworkInterface{
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
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Disks = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks: Required value: at least 1 disk is required",
		},
		{
			testCase: "with a disk that is too small",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Disks = []*machinev1beta1.GCPDisk{
					{
						SizeGB: 1,
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks[0].sizeGb: Invalid value: 1: must be at least 16GB in size",
		},
		{
			testCase: "with a disk that is too large",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Disks = []*machinev1beta1.GCPDisk{
					{
						SizeGB: 100000,
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks[0].sizeGb: Invalid value: 100000: exceeding maximum GCP disk size limit, must be below 65536",
		},
		{
			testCase: "with a disk type that is not supported",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Disks = []*machinev1beta1.GCPDisk{
					{
						SizeGB: 16,
						Type:   "invalid",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.disks[0].type: Unsupported value: \"invalid\": supported values: \"hyperdisk-balanced\", \"pd-balanced\", \"pd-ssd\", \"pd-standard\"",
		},
		{
			testCase: "with a disk type that is supported",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Disks = []*machinev1beta1.GCPDisk{
					{
						SizeGB: 16,
						Type:   "pd-balanced",
					},
				}
			},
			expectedOk: true,
		},
		{
			testCase: "with no service accounts",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ServiceAccounts = nil
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.serviceAccounts: no service account provided: nodes may be unable to join the cluster"},
		},
		{
			testCase: "with multiple service accounts",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ServiceAccounts = []machinev1beta1.GCPServiceAccount{
					{},
					{},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.serviceAccounts: Invalid value: \"2 service accounts supplied\": exactly 1 service account must be supplied",
		},
		{
			testCase: "with the service account's email missing",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ServiceAccounts = []machinev1beta1.GCPServiceAccount{
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
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ServiceAccounts = []machinev1beta1.GCPServiceAccount{
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
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.UserDataSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials data secret",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no user data secret name",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
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
		{
			testCase: "with no Type",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.GPUs = []machinev1beta1.GCPGPUConfig{
					{
						Type: "",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.gpus.Type: Required value: Type is required",
		},
		{
			testCase: "with nvidia-tesla-A100 Type",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.GPUs = []machinev1beta1.GCPGPUConfig{
					{
						Type: "nvidia-tesla-a100",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.gpus.Type: Invalid value: \"nvidia-tesla-a100\":  nvidia-tesla-a100 gpus, are only attached to the A2 machine types",
		},
		{
			testCase: "with a2 machine family type",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.MachineType = "a2-highgpu-1g"
				p.GPUs = []machinev1beta1.GCPGPUConfig{
					{
						Type: "any-gpu",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.gpus: Invalid value: \"any-gpu\": A2 machine types have already attached gpus, additional gpus cannot be specified",
		},
		{
			testCase: "with more than one gpu type",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.GPUs = []machinev1beta1.GCPGPUConfig{
					{
						Type: "any-gpu",
					},
					{
						Type: "any-gpu",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.gpus: Too many: 2: must have at most 1 items",
		},
		{
			testCase: "with no gpus",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.GPUs = nil
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with invalid onHostMaintenance",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.OnHostMaintenance = "invalid-value"
			},
			expectedOk:    false,
			expectedError: "providerSpec.onHostMaintenance: Invalid value: \"invalid-value\": onHostMaintenance must be either Migrate or Terminate.",
		},
		{
			testCase: "with invalid restartPolicy",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.RestartPolicy = "invalid-value"
			},
			expectedOk:    false,
			expectedError: "providerSpec.restartPolicy: Invalid value: \"invalid-value\": restartPolicy must be either Never or Always.",
		},
		{
			testCase: "with valid shieldedInstanceConfig",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ShieldedInstanceConfig = machinev1beta1.GCPShieldedInstanceConfig{
					SecureBoot:                       machinev1beta1.SecureBootPolicyEnabled,
					IntegrityMonitoring:              machinev1beta1.IntegrityMonitoringPolicyEnabled,
					VirtualizedTrustedPlatformModule: machinev1beta1.VirtualizedTrustedPlatformModulePolicyEnabled,
				}
			},
			expectedOk: true,
		},
		{
			testCase: "with invalid secureBoot",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ShieldedInstanceConfig = machinev1beta1.GCPShieldedInstanceConfig{
					SecureBoot: "invalid-value",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.shieldedInstanceConfig.secureBoot: Invalid value: \"invalid-value\": secureBoot must be either Enabled or Disabled.",
		},
		{
			testCase: "with invalid integrityMonitoring",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ShieldedInstanceConfig = machinev1beta1.GCPShieldedInstanceConfig{
					IntegrityMonitoring: "invalid-value",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.shieldedInstanceConfig.integrityMonitoring: Invalid value: \"invalid-value\": integrityMonitoring must be either Enabled or Disabled.",
		},
		{
			testCase: "with invalid virtualizedTrustedPlatformModule",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ShieldedInstanceConfig = machinev1beta1.GCPShieldedInstanceConfig{
					VirtualizedTrustedPlatformModule: "invalid-value",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.shieldedInstanceConfig.virtualizedTrustedPlatformModule: Invalid value: \"invalid-value\": virtualizedTrustedPlatformModule must be either Enabled or Disabled.",
		},
		{
			testCase: "with virtualizedTrustedPlatformModule disabled while integrityMonitoring is enabled",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ShieldedInstanceConfig = machinev1beta1.GCPShieldedInstanceConfig{
					VirtualizedTrustedPlatformModule: machinev1beta1.VirtualizedTrustedPlatformModulePolicyDisabled,
					IntegrityMonitoring:              machinev1beta1.IntegrityMonitoringPolicyEnabled,
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.shieldedInstanceConfig.virtualizedTrustedPlatformModule: Invalid value: \"Disabled\": integrityMonitoring requires virtualizedTrustedPlatformModule Enabled.",
		},
		{
			testCase: "with ConfidentialCompute",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyEnabled
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "n2d-standard-4"
			},
			expectedOk: true,
		},
		{
			testCase: "with ConfidentialCompute invalid value",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = "invalid-value"
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
			},
			expectedOk:    false,
			expectedError: "providerSpec.confidentialCompute: Invalid value: \"invalid-value\": ConfidentialCompute must be Enabled, Disabled, AMDEncryptedVirtualization, AMDEncryptedVirtualizationNestedPaging, or IntelTrustedDomainExtensions",
		},
		{
			testCase: "with ConfidentialCompute enabled while onHostMaintenance is set to Migrate on n2d instances",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyEnabled
				p.OnHostMaintenance = machinev1beta1.MigrateHostMaintenanceType
				p.MachineType = "n2d-standard-4"
				p.GPUs = []machinev1beta1.GCPGPUConfig{}
			},
			expectedOk: true,
		},
		{
			testCase: "with ConfidentialCompute enabled while onHostMaintenance is set to Migrate on non n2d instances (c2d)",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyEnabled
				p.OnHostMaintenance = machinev1beta1.MigrateHostMaintenanceType
				p.MachineType = "c2d-standard-4"
				p.GPUs = []machinev1beta1.GCPGPUConfig{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.onHostMaintenance: Invalid value: \"Migrate\": ConfidentialCompute Enabled requires OnHostMaintenance to be set to Terminate, the current value is: Migrate",
		},
		{
			testCase: "with ConfidentialCompute enabled and unsupported machineType",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyEnabled
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "e2-standard-4"
			},
			expectedOk:    false,
			expectedError: "providerSpec.machineType: Invalid value: \"e2-standard-4\": ConfidentialCompute Enabled requires a machine type in the following series: n2d,c2d,c3d",
		},
		{
			testCase: "with ConfidentialCompute AMDEncryptedVirtualization and an unsupported machine",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicySEV
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "c3-standard-4"
			},
			expectedOk:    false,
			expectedError: "providerSpec.machineType: Invalid value: \"c3-standard-4\": ConfidentialCompute AMDEncryptedVirtualization requires a machine type in the following series: n2d,c2d,c3d",
		},
		{
			testCase: "with ConfidentialCompute AMDEncryptedVirtualization and a supported machine",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicySEV
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "c2d-standard-4"
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with ConfidentialCompute AMDEncryptedVirtualization and onHostMaintenance set to Migrate",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicySEV
				p.OnHostMaintenance = machinev1beta1.MigrateHostMaintenanceType
				p.MachineType = "c3d-standard-4"
				p.GPUs = []machinev1beta1.GCPGPUConfig{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.onHostMaintenance: Invalid value: \"Migrate\": ConfidentialCompute AMDEncryptedVirtualization requires OnHostMaintenance to be set to Terminate, the current value is: Migrate",
		},
		{
			testCase: "with ConfidentialCompute AMDEncryptedVirtualizationNestedPaging and an unsupported machine",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicySEVSNP
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "c3-standard-4"
			},
			expectedOk:    false,
			expectedError: "providerSpec.machineType: Invalid value: \"c3-standard-4\": ConfidentialCompute AMDEncryptedVirtualizationNestedPaging requires a machine type in the following series: n2d",
		},
		{
			testCase: "with ConfidentialCompute AMDEncryptedVirtualizationNestedPaging and a supported machine",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicySEVSNP
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "n2d-standard-4"
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with ConfidentialCompute AMDEncryptedVirtualizationNestedPaging and onHostMaintenance set to Migrate on n2d instances",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicySEVSNP
				p.OnHostMaintenance = machinev1beta1.MigrateHostMaintenanceType
				p.MachineType = "n2d-standard-4"
				p.GPUs = []machinev1beta1.GCPGPUConfig{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.onHostMaintenance: Invalid value: \"Migrate\": ConfidentialCompute AMDEncryptedVirtualizationNestedPaging requires OnHostMaintenance to be set to Terminate, the current value is: Migrate",
		},
		{
			testCase: "with ConfidentialCompute IntelTrustedDomainExtensions and an unsupported machine",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyTDX
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "c3d-standard-4"
			},
			expectedOk:    false,
			expectedError: "providerSpec.machineType: Invalid value: \"c3d-standard-4\": ConfidentialCompute IntelTrustedDomainExtensions requires a machine type in the following series: c3",
		},
		{
			testCase: "with ConfidentialCompute IntelTrustedDomainExtensions and a supported machine",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyTDX
				p.OnHostMaintenance = machinev1beta1.TerminateHostMaintenanceType
				p.MachineType = "c3-standard-4"
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with ConfidentialCompute IntelTrustedDomainExtensions and onHostMaintenance set to Migrate",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.ConfidentialCompute = machinev1beta1.ConfidentialComputePolicyTDX
				p.OnHostMaintenance = machinev1beta1.MigrateHostMaintenanceType
				p.MachineType = "c3-standard-4"
				p.GPUs = []machinev1beta1.GCPGPUConfig{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.onHostMaintenance: Invalid value: \"Migrate\": ConfidentialCompute IntelTrustedDomainExtensions requires OnHostMaintenance to be set to Terminate, the current value is: Migrate",
		},
		{
			testCase: "with GPUs and Migrate onHostMaintenance",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.OnHostMaintenance = machinev1beta1.MigrateHostMaintenanceType
				p.GPUs = []machinev1beta1.GCPGPUConfig{
					{
						Type: "any-gpu",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.onHostMaintenance: Forbidden: When GPUs are specified or using machineType with pre-attached GPUs(A2 machine family), onHostMaintenance must be set to Terminate.",
		},
		{
			testCase: "with invalid GroupVersionKind",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Kind = "INVALID"
				p.APIVersion = "INVALID/v1"
			},
			expectedOk:       true,
			expectedWarnings: []string{"incorrect GroupVersionKind for GCPMachineProviderSpec object: INVALID/v1, Kind=INVALID"},
		},
		{
			testCase: "with machine.openshift.io API group",
			modifySpec: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Kind = "GCPMachineProviderSpec"
				p.APIVersion = "machine.openshift.io/v1beta1"
			},
			expectedOk: true,
		},
		{
			testCase:         "with unknown fields in the providerSpec",
			overrideRawBytes: []byte(`{"kind":"GCPMachineProviderSpec","apiVersion":"gcpprovider.openshift.io/v1beta1","metadata":{"creationTimestamp":null},"userDataSecret":{"name":"name"},"credentialsSecret":{"name":"name"},"canIPForward":false,"deletionProtection":false,"disks":[{"autoDelete":false,"boot":false,"sizeGb":16,"type":"","image":"","labels":null}],"networkInterfaces":[{"network":"network","subnetwork":"subnetwork"}],"serviceAccounts":[{"email":"email","scopes":["scope"]}],"machineType":"machineType","region":"region","zone":"region-zone","projectID":"projectID","gpus":[{"count":0,"type":"type"}],"onHostMaintenance":"Terminate","randomField-1": "something"}`),
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.value: Unsupported value: \"randomField-1\": Unknown field (randomField-1) will be ignored"},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()
	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.GCPPlatformType

	gate, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Errorf("Unexpected error setting up feature gates: %v", err)
	}

	h := createMachineValidator(infra, c, plainDNS, gate)

	for _, tc := range testCases {
		providerSpec := &machinev1beta1.GCPMachineProviderSpec{
			Region:            "region",
			Zone:              "region-zone",
			ProjectID:         "projectID",
			MachineType:       "machineType",
			OnHostMaintenance: machinev1beta1.TerminateHostMaintenanceType,
			NetworkInterfaces: []*machinev1beta1.GCPNetworkInterface{
				{
					Network:    "network",
					Subnetwork: "subnetwork",
				},
			},
			Disks: []*machinev1beta1.GCPDisk{
				{
					SizeGB: 16,
				},
			},
			GPUs: []machinev1beta1.GCPGPUConfig{
				{
					Type: "type",
				},
			},
			ServiceAccounts: []machinev1beta1.GCPServiceAccount{
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
			TypeMeta: metav1.TypeMeta{
				Kind:       "GCPMachineProviderSpec",
				APIVersion: "gcpprovider.openshift.io/v1beta1",
			},
		}

		if tc.modifySpec != nil {
			tc.modifySpec(providerSpec)
		}

		t.Run(tc.testCase, func(t *testing.T) {
			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}
			if tc.overrideRawBytes != nil {
				m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: tc.overrideRawBytes}
			}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultGCPProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	projectID := "projectID"
	itWarnings := make([]string, 0)
	instanceType := defaultInstanceTypeForCloudProvider(osconfigv1.GCPPlatformType, arch, &itWarnings)

	testCases := []struct {
		testCase         string
		providerSpec     *machinev1beta1.GCPMachineProviderSpec
		modifyDefault    func(*machinev1beta1.GCPMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:         "it defaults defaultable fields",
			providerSpec:     &machinev1beta1.GCPMachineProviderSpec{},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "it does not overwrite disks which already have fields set",
			providerSpec: &machinev1beta1.GCPMachineProviderSpec{
				Disks: []*machinev1beta1.GCPDisk{
					{
						AutoDelete: false,
						Boot:       false,
						SizeGB:     32,
					},
				},
			},
			modifyDefault: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.Disks = []*machinev1beta1.GCPDisk{
					{
						AutoDelete: false,
						Boot:       false,
						SizeGB:     32,
						Type:       defaultGCPDiskType,
						Image:      defaultGCPDiskImage(),
					},
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
		},
		{
			testCase: "sets default gpu Count",
			providerSpec: &machinev1beta1.GCPMachineProviderSpec{
				GPUs: []machinev1beta1.GCPGPUConfig{
					{
						Type: "type",
					},
				},
			},
			modifyDefault: func(p *machinev1beta1.GCPMachineProviderSpec) {
				p.GPUs = []machinev1beta1.GCPGPUConfig{
					{
						Type:  "type",
						Count: defaultGCPGPUCount,
					},
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: itWarnings,
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
		defaultProviderSpec := &machinev1beta1.GCPMachineProviderSpec{
			MachineType: instanceType,
			NetworkInterfaces: []*machinev1beta1.GCPNetworkInterface{
				{
					Network:    defaultGCPNetwork(clusterID),
					Subnetwork: defaultGCPSubnetwork(clusterID),
				},
			},
			Disks: []*machinev1beta1.GCPDisk{
				{
					AutoDelete: true,
					Boot:       true,
					SizeGB:     defaultGCPDiskSizeGb,
					Type:       defaultGCPDiskType,
					Image:      defaultGCPDiskImage(),
				},
			},
			Tags: defaultGCPTags(clusterID),
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
			m := &machinev1beta1.Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(machinev1beta1.GCPMachineProviderSpec)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestValidateVSphereProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "vsphere-validation-test",
		},
	}

	testCases := []struct {
		testCase            string
		modifySpec          func(*machinev1beta1.VSphereMachineProviderSpec)
		expectedError       string
		expectedOk          bool
		expectedWarnings    []string
		featureGatesEnabled map[string]bool
	}{
		{
			testCase: "with no template provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Template = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.template: Required value: template must be provided",
		},
		{
			testCase: "with no workspace provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace: Required value: workspace must be provided",
		},
		{
			testCase: "with no workspace server provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace = &machinev1beta1.Workspace{
					Datacenter: "datacenter",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace.server: Required value: server must be provided",
		},
		{
			testCase: "with no workspace datacenter provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace = &machinev1beta1.Workspace{
					Server: "server",
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.workspace.datacenter: datacenter is unset: if more than one datacenter is present, VMs cannot be created"},
		},
		{
			testCase: "with a workspace folder outside of the current datacenter",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace = &machinev1beta1.Workspace{
					Server:     "server",
					Datacenter: "datacenter",
					Folder:     "/foo/vm/folder",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace.folder: Invalid value: \"/foo/vm/folder\": folder must be absolute path: expected prefix \"/datacenter/vm/\"",
		},
		{
			testCase: "with no network devices provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Network = machinev1beta1.NetworkSpec{
					Devices: []machinev1beta1.NetworkDeviceSpec{},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
		},
		{
			testCase: "with no network device name provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Network = machinev1beta1.NetworkSpec{
					Devices: []machinev1beta1.NetworkDeviceSpec{
						{
							NetworkName: "networkName",
						},
						{},
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.network.devices[1].networkName: Required value: networkName must be provided",
		},
		{
			testCase: "with too few CPUs provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.NumCPUs = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.numCPUs: 1 is missing or less than the minimum value (2): nodes may not boot correctly"},
		},
		{
			testCase: "with too little memory provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.MemoryMiB = 1024
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.memoryMiB: 1024 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with too little disk size provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DiskGiB = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.diskGiB: 1 is missing or less than the recommended minimum (120): nodes may fail to start if disk size is too low"},
		},
		{
			testCase: "with no user data secret provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.UserDataSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials secret provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no credentials secret name provided",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
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
		{
			testCase: "with numCPUs equal to 0",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.NumCPUs = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.numCPUs: 0 is missing or less than the minimum value (2): nodes may not boot correctly"},
		},
		{
			testCase: "with memoryMiB equal to 0",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.MemoryMiB = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.memoryMiB: 0 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with diskGiB equal to 0",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DiskGiB = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.diskGiB: 0 is missing or less than the recommended minimum (120): nodes may fail to start if disk size is too low"},
		},
		{
			testCase: "linked clone mode and disk size warning",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DiskGiB = 100500
				p.CloneMode = machinev1beta1.LinkedClone
			},
			expectedOk:       true,
			expectedWarnings: []string{"linkedClone clone mode is set. DiskGiB parameter will be ignored, disk size from template will be used."},
		},
		{
			testCase: "with invalid GroupVersionKind",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Kind = "INVALID"
				p.APIVersion = "INVALID/v1"
			},
			expectedOk:       true,
			expectedWarnings: []string{"incorrect GroupVersionKind for VSphereMachineProviderSpec object: INVALID/v1, Kind=INVALID"},
		},
		{
			testCase: "with machine.openshift.io API group",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Kind = "VSphereMachineProviderSpec"
				p.APIVersion = "machine.openshift.io/v1beta1"
			},
			expectedOk: true,
		},
		{
			testCase: "with vmGroup greater than 80 characters",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace.VMGroup = "thisvmgroupnameismorethaneightycharactersthisvmgroupnameismorethaneightycharactersthisvmgroupnameismorethaneightycharacters"
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereHostVMGroupZonal)] = true
				return fg
			}(),
			expectedError: "providerSpec.workspace.vmGroup: Invalid value: \"thisvmgroupnameismorethaneightycharactersthisvmgroupnameismorethaneightycharactersthisvmgroupnameismorethaneightycharacters\": vmGroup must be less than 80 characters in length",
		},
		{
			testCase: "with vmGroup configured without feature gate enabled",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace.VMGroup = "thisisavmgroup"
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereHostVMGroupZonal)] = false
				return fg
			}(),
			expectedError: "providerSpec.workspace.vmGroup: Invalid value: \"thisisavmgroup\": feature gate VSphereHostVMGroupZonal must be enabled to use vmGroup",
		},
		{
			testCase: "with vmGroup configured with feature gate enabled",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.Workspace.VMGroup = "thisisavmgroup"
			},
			expectedOk: true,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereHostVMGroupZonal)] = true
				return fg
			}(),
		},
		{
			testCase: "with data disk configured without feature gate enabled",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:    "Disk1",
						SizeGiB: 10,
					},
				}
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = false
				return fg
			}(),
			expectedError: "providerSpec.disks: Forbidden: this field is protected by the VSphereMultiDisk feature gate which must be enabled through either the TechPreviewNoUpgrade or CustomNoUpgrade feature set",
		},
		{
			testCase: "with data disk configured with feature gate enabled",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:    "Disk1",
						SizeGiB: 10,
					},
				}
			},
			expectedOk: true,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
		},
		{
			testCase: "with data disk configured with max size",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:    "Disk1",
						SizeGiB: 16384,
					},
				}
			},
			expectedOk: true,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
		},
		{
			testCase: "with data disk configured with size above max",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:    "Disk1",
						SizeGiB: 17000,
					},
				}
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
			expectedError: "providerSpec.disks[0].sizeGiB: Invalid value: 17000: data disk size (GiB) must not exceed 16384",
		},
		{
			testCase: "with data disk configured without a size",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name: "Disk1",
					},
				}
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
			expectedError: "providerSpec.disks[0].sizeGiB: Required value: data disk size must be set",
		},
		{
			testCase: "with data disk configured without a name",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						SizeGiB: 10,
					},
				}
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
			expectedError: "providerSpec.disks[0].name: Required value: data disk name must be set",
		},
		{
			testCase: "with data disk configured with invalid name",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:    "Bad #Name",
						SizeGiB: 10,
					},
				}
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
			expectedError: "providerSpec.disks[0].name: Invalid value: \"Bad #Name\": data disk name must consist only of alphanumeric characters, hyphens and underscores, and must start and end with an alphanumeric character.",
		},
		{
			testCase: "with data disk configured with provisioning mode",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:             "Disk1",
						SizeGiB:          10,
						ProvisioningMode: machinev1beta1.ProvisioningModeThin,
					},
				}
			},
			expectedOk: true,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
		},
		{
			testCase: "with data disk configured with invalid provisioning mode",
			modifySpec: func(p *machinev1beta1.VSphereMachineProviderSpec) {
				p.DataDisks = []machinev1beta1.VSphereDisk{
					{
						Name:             "Disk1",
						SizeGiB:          10,
						ProvisioningMode: "Fat",
					},
				}
			},
			expectedOk: false,
			featureGatesEnabled: func() map[string]bool {
				fg := make(map[string]bool)
				fg[string(features.FeatureGateVSphereMultiDisk)] = true
				return fg
			}(),
			expectedError: "providerSpec.disks[0]: Unsupported value: \"Fat\": supported values: \"EagerlyZeroed\", \"Thick\", \"Thin\"",
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()
	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.VSpherePlatformType

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			if len(tc.featureGatesEnabled) != 0 {
				if err := gate.SetFromMap(tc.featureGatesEnabled); err != nil {
					t.Errorf("Unexpected error setting up feature gates via map: %v", err)
				}
			}

			h := createMachineValidator(infra, c, plainDNS, gate)
			providerSpec := &machinev1beta1.VSphereMachineProviderSpec{
				Template: "template",
				Workspace: &machinev1beta1.Workspace{
					Datacenter: "datacenter",
					Server:     "server",
				},
				Network: machinev1beta1.NetworkSpec{
					Devices: []machinev1beta1.NetworkDeviceSpec{
						{
							NetworkName: "networkName",
						},
					},
				},
				UserDataSecret: &corev1.LocalObjectReference{
					Name: "name",
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "name",
				},
				NumCPUs:   minVSphereCPU,
				MemoryMiB: minVSphereMemoryMiB,
				DiskGiB:   minVSphereDiskGiB,
				TypeMeta: metav1.TypeMeta{
					Kind:       "VSphereMachineProviderSpec",
					APIVersion: "vsphereprovider.openshift.io/v1beta1",
				},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultVSphereProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	testCases := []struct {
		testCase         string
		providerSpec     *machinev1beta1.VSphereMachineProviderSpec
		modifyDefault    func(*machinev1beta1.VSphereMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &machinev1beta1.VSphereMachineProviderSpec{},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.VSpherePlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			defaultProviderSpec := &machinev1beta1.VSphereMachineProviderSpec{
				UserDataSecret: &corev1.LocalObjectReference{
					Name: defaultUserDataSecret,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: defaultVSphereCredentialsSecret,
				},
			}
			if tc.modifyDefault != nil {
				tc.modifyDefault(defaultProviderSpec)
			}

			m := &machinev1beta1.Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(machinev1beta1.VSphereMachineProviderSpec)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultNutanixProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	testCases := []struct {
		testCase         string
		providerSpec     *machinev1.NutanixMachineProviderConfig
		modifyDefault    func(providerConfig *machinev1.NutanixMachineProviderConfig)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &machinev1.NutanixMachineProviderConfig{},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.NutanixPlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			defaultProviderSpec := &machinev1.NutanixMachineProviderConfig{
				UserDataSecret: &corev1.LocalObjectReference{
					Name: defaultUserDataSecret,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: defaultNutanixCredentialsSecret,
				},
			}
			if tc.modifyDefault != nil {
				tc.modifyDefault(defaultProviderSpec)
			}

			m := &machinev1beta1.Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(machinev1.NutanixMachineProviderConfig)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestUpdateFinalizer(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "update-finalizer-test",
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()

	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.AWSPlatformType

	gate, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Errorf("Unexpected error setting up feature gates: %v", err)
	}

	h := createMachineValidator(infra, c, plainDNS, gate)

	providerSpec := &machinev1beta1.AWSMachineProviderConfig{
		AMI: machinev1beta1.AWSResourceReference{
			ID: ptr.To[string]("ami"),
		},
		Placement: machinev1beta1.Placement{
			Region: "region",
		},
		InstanceType: "m5.large",
		IAMInstanceProfile: &machinev1beta1.AWSResourceReference{
			ID: ptr.To[string]("profileID"),
		},
		UserDataSecret: &corev1.LocalObjectReference{
			Name: "secret",
		},
		CredentialsSecret: &corev1.LocalObjectReference{
			Name: "secret",
		},
		SecurityGroups: []machinev1beta1.AWSResourceReference{
			{
				ID: ptr.To[string]("sg"),
			},
		},
		Subnet: machinev1beta1.AWSResourceReference{
			ID: ptr.To[string]("subnet"),
		},
	}

	testCases := []struct {
		testCase              string
		modifyOldSpec         func(machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig
		oldMachineFinalizers  []string
		modifyNewSpec         func(machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig
		newMachineFinalizers  []string
		withDeletionTimestamp bool
		expectedError         string
		expectedOk            bool
	}{
		{
			testCase:   "no changes on valid machines without finalizers",
			expectedOk: true,
		},
		{
			testCase:             "adding the finalizer to a valid machine",
			newMachineFinalizers: []string{"machine-finalizer"},
			expectedOk:           true,
		},
		{
			testCase:             "no changes on valid machines with finalizers",
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: []string{"machine-finalizer"},
			expectedOk:           true,
		},
		{
			testCase:             "updating the finalizer on a valid machine",
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: []string{"new-machine-finalizer"},
			expectedOk:           true,
		},
		{
			testCase:             "deleting the finalizer from a valid machine",
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: nil,
			expectedOk:           true,
		},
		{
			testCase: "no changes on invalid machines without finalizers",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			expectedError: "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "adding the finalizer to an invalid machine",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			newMachineFinalizers: []string{"machine-finalizer"},
			expectedError:        "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "no changes on invalid machines with finalizers",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: []string{"machine-finalizer"},
			expectedError:        "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "updating the finalizer on an invalid machine",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: []string{"new-machine-finalizer"},
			expectedError:        "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "deleting the finalizer from an invalid machine",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: nil,
			expectedError:        "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "deleting the finalizer from an invalid machine with set deletion timestamp",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			oldMachineFinalizers:  []string{"machine-finalizer"},
			newMachineFinalizers:  nil,
			withDeletionTimestamp: true,
			expectedOk:            true,
		},
		{
			testCase: "deleting the finalizer and applying an invalid change",
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: nil,
			expectedError:        "providerSpec.credentialsSecret: Required value: expected providerSpec.credentialsSecret to be populated",
		},
		{
			testCase: "deleting the finalizer and fixing the machine",
			modifyOldSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = nil
				return p
			},
			modifyNewSpec: func(p machinev1beta1.AWSMachineProviderConfig) machinev1beta1.AWSMachineProviderConfig {
				p.CredentialsSecret = &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret}
				return p
			},
			oldMachineFinalizers: []string{"machine-finalizer"},
			newMachineFinalizers: nil,
			expectedOk:           true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			var rawBytes []byte
			var err error

			gs := NewWithT(t)

			oldM := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace.Name,
					Finalizers: tc.oldMachineFinalizers,
				},
			}

			if tc.modifyOldSpec != nil {
				rawBytes, err = json.Marshal(tc.modifyOldSpec(*providerSpec))
			} else {
				rawBytes, err = json.Marshal(providerSpec)
			}
			gs.Expect(err).ToNot(HaveOccurred())
			oldM.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			newM := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:  namespace.Name,
					Finalizers: tc.newMachineFinalizers,
				},
			}
			if tc.modifyNewSpec != nil {
				rawBytes, err = json.Marshal(tc.modifyNewSpec(*providerSpec))
			} else {
				rawBytes, err = json.Marshal(providerSpec)
			}
			gs.Expect(err).ToNot(HaveOccurred())
			newM.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			if tc.withDeletionTimestamp {
				deletionTimestamp := metav1.Now()
				oldM.SetDeletionTimestamp(&deletionTimestamp)
				newM.SetDeletionTimestamp(&deletionTimestamp)
			}

			ok, _, webhookErr := h.validateMachine(newM, oldM)
			gs.Expect(ok).To(Equal(tc.expectedOk))

			if webhookErr == nil {
				gs.Expect(tc.expectedError).To(BeEmpty())
			} else {
				gs.Expect(webhookErr.ToAggregate().Error()).To(Equal(tc.expectedError))
			}
		})
	}
}

func TestValidatePowerVSProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "powervs-validation-test",
		},
	}

	testCases := []struct {
		testCase         string
		modifySpec       func(providerConfig *machinev1.PowerVSMachineProviderConfig)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with no serviceInstanceID provided",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.ServiceInstance = machinev1.PowerVSResource{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.serviceInstance: Required value: serviceInstance identifier must be provided",
		},
		{
			testCase: "with regex for serviceInstanceID",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.ServiceInstance.Type = machinev1.PowerVSResourceTypeRegEx
				p.ServiceInstance.RegEx = ptr.To[string]("DHCP")
			},
			expectedOk:    false,
			expectedError: "providerSpec.serviceInstance: Invalid value: \"RegEx\": serviceInstance identifier is specified as RegEx but only ID and Name are valid resource identifiers",
		},
		{
			testCase: "with no UserDataSecret",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: providerSpec.userDataSecret must be provided",
		},
		{
			testCase: "with no CredentialsSecret",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: providerSpec.credentialsSecret must be provided",
		},
		{
			testCase: "with no keyPairName",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.KeyPairName = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.keyPairName: Required value: providerSpec.keyPairName must be provided",
		},
		{
			testCase: "with no Image",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Image = machinev1.PowerVSResource{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image: Required value: image identifier must be provided",
		},
		{
			testCase: "with regex for image",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Image.Type = machinev1.PowerVSResourceTypeRegEx
				p.Image.RegEx = ptr.To[string]("DHCP")
			},
			expectedOk:    false,
			expectedError: "providerSpec.image: Invalid value: \"RegEx\": image identifier is specified as RegEx but only ID and Name are valid resource identifiers",
		},
		{
			testCase: "with no Network",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Network = machinev1.PowerVSResource{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.network: Required value: network identifier must be provided",
		},
		{
			testCase: "with no user data secret name provided",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.UserDataSecret = &machinev1.PowerVSSecretReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: providerSpec.userDataSecret.name must be provided",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with not a known system type",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.SystemType = "testSystemType"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.SystemType: testSystemType is not known, Currently known system types are s922, e980 and e880"},
		},
		{
			testCase: "with a known system type",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.SystemType = defaultPowerVSSysType
			},
			expectedOk: true,
		},
		{
			testCase: "with memory greater than supported value",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.MemoryGiB = 950
			},
			expectedOk:    false,
			expectedError: "providerSpec.memoryGiB: Invalid value: 950: for s922 systemtype the maximum supported memory value is 942",
		},
		{
			testCase: "with memory less than minimum value",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.MemoryGiB = 30
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerspec.MemoryGiB 30 is less than the minimum value 32"},
		},
		{
			testCase: "with negative memory value",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.MemoryGiB = -10
			},
			expectedOk:    false,
			expectedError: "providerSpec.memoryGiB: Invalid value: -10: memory value cannot be negative",
		},
		{
			testCase: "with invalid processor value",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Processors = intstr.FromString("testProcessor")
			},
			expectedOk:    false,
			expectedError: "providerSpec.processor: Internal error: error while getting processor vlaue failed to convert Processors testProcessor to float64",
		},
		{
			testCase: "with processor greater than supported value for s922 systemtype",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Processors = intstr.FromInt(20)
			},
			expectedOk:    false,
			expectedError: "providerSpec.processor: Invalid value: 20: for s922 systemtype the maximum supported processor value is 15.000000",
		},
		{
			testCase: "with supported value processor for e880 systemtype",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.SystemType = powerVSSystemTypeE880
				p.Processors = intstr.FromInt(20)
			},
			expectedOk: true,
		},
		{
			testCase: "with processor less than minimum value for dedicated Processor type",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.ProcessorType = machinev1.PowerVSProcessorTypeDedicated
				p.Processors = intstr.FromString("0.9")
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerspec.Processor 0.900000 is less than the minimum value 1.000000 for providerSpec.ProcessorType: Dedicated"},
		},
		{
			testCase: "with processor less than minimum value for shared Processor type",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Processors = intstr.FromString("0.4")
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerspec.Processor 0.400000 is less than the minimum value 0.500000 for providerSpec.ProcessorType: Shared"},
		},
		{
			testCase: "with negative processor value",
			modifySpec: func(p *machinev1.PowerVSMachineProviderConfig) {
				p.Processors = intstr.FromInt(-2)
			},
			expectedOk:    false,
			expectedError: "providerSpec.processor: Invalid value: -2: processor value cannot be negative",
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultPowerVSCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()
	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.PowerVSPlatformType
	gate, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Errorf("Unexpected error setting up feature gates: %v", err)
	}

	h := createMachineValidator(infra, c, plainDNS, gate)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &machinev1.PowerVSMachineProviderConfig{
				ServiceInstance: machinev1.PowerVSResource{
					Type: machinev1.PowerVSResourceTypeName,
					Name: ptr.To[string]("testServiceInstanceID"),
				},
				Image: machinev1.PowerVSResource{
					Type: machinev1.PowerVSResourceTypeName,
					Name: ptr.To[string]("testImageName"),
				},
				Network: machinev1.PowerVSResource{
					Type: machinev1.PowerVSResourceTypeName,
					Name: ptr.To[string]("testNetworkName"),
				},
				UserDataSecret: &machinev1.PowerVSSecretReference{
					Name: defaultUserDataSecret,
				},
				CredentialsSecret: &machinev1.PowerVSSecretReference{
					Name: defaultPowerVSCredentialsSecret,
				},
				SystemType:    defaultPowerVSSysType,
				ProcessorType: defaultPowerVSProcType,
				Processors:    intstr.FromString(defaultPowerVSProcessor),
				MemoryGiB:     defaultPowerVSMemory,
				KeyPairName:   "test-keypair",
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultPowerVSProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	testCases := []struct {
		testCase         string
		providerSpec     *machinev1.PowerVSMachineProviderConfig
		modifyDefault    func(*machinev1.PowerVSMachineProviderConfig)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &machinev1.PowerVSMachineProviderConfig{},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "it does not override the set values",
			providerSpec: &machinev1.PowerVSMachineProviderConfig{
				Processors: intstr.FromString("1.5"),
				MemoryGiB:  35,
			},
			modifyDefault: func(providerConfig *machinev1.PowerVSMachineProviderConfig) {
				providerConfig.Processors = intstr.FromString("1.5")
				providerConfig.MemoryGiB = 35
			},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.PowerVSPlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			defaultProviderSpec := &machinev1.PowerVSMachineProviderConfig{
				UserDataSecret: &machinev1.PowerVSSecretReference{
					Name: defaultUserDataSecret,
				},
				CredentialsSecret: &machinev1.PowerVSSecretReference{
					Name: defaultPowerVSCredentialsSecret,
				},
				ProcessorType: defaultPowerVSProcType,
				SystemType:    defaultPowerVSSysType,
				Processors:    intstr.FromString(defaultPowerVSProcessor),
				MemoryGiB:     defaultPowerVSMemory,
			}
			if tc.modifyDefault != nil {
				tc.modifyDefault(defaultProviderSpec)
			}

			m := &machinev1beta1.Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(machinev1.PowerVSMachineProviderConfig)
			if err := yaml.Unmarshal(m.Spec.ProviderSpec.Value.Raw, &gotProviderSpec); err != nil {
				t.Fatal(err)
			}

			if !equality.Semantic.DeepEqual(defaultProviderSpec, gotProviderSpec) {
				t.Errorf("expected: %+v, got: %+v", defaultProviderSpec, gotProviderSpec)
			}
			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestValidateNutanixProviderSpec(t *testing.T) {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nutanix-validation-test",
		},
	}

	testCases := []struct {
		testCase         string
		modifySpec       func(*machinev1.NutanixMachineProviderConfig)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with too few CPU sockets provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.VCPUSockets = 0
				p.VCPUsPerSocket = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.vcpuSockets: 0 is missing or less than the minimum value (1): nodes may not boot correctly"},
		},
		{
			testCase: "with too few CPUs per socket provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.VCPUSockets = 1
				p.VCPUsPerSocket = 0
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.vcpusPerSocket: 0 is missing or less than the minimum value (1): nodes may not boot correctly"},
		},
		{
			testCase: "with too little memory provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.MemorySize = resource.MustParse("1024Mi")
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.memorySize: 1024 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with too little disk size provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.SystemDiskSize = resource.MustParse("10Gi")
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.systemDiskSize: 10 is missing or less than the recommended minimum (20): nodes may fail to start if disk size is too low"},
		},
		{
			testCase: "with no subnets provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.Subnets = make([]machinev1.NutanixResourceIdentifier, 0)
			},
			expectedOk:    false,
			expectedError: "providerSpec.subnets: Invalid value: \"[]\": missing subnets: nodes may fail to start if no subnets are configured",
			//expectedWarnings: []string{"providerSpec.subnets: missing subnets: nodes may fail to start if no subnets are configured"},
		},
		{
			testCase: "with no userDataSecret provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no credentialsSecret provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "with invalid bootType provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.BootType = "invalid"
			},
			expectedOk:    false,
			expectedError: fmt.Sprintf("providerSpec.bootType: Invalid value: \"invalid\": valid bootType values are: \"\", %q, %q, %q.", machinev1.NutanixLegacyBoot, machinev1.NutanixUEFIBoot, machinev1.NutanixSecureBoot),
		},
		{
			testCase: "with invalid categories provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.Categories = append(p.Categories, machinev1.NutanixCategory{Key: "key1",
					Value: "val0123456789012345678901234567890123456789012345678901234567890123456789"})
			},
			expectedOk:    false,
			expectedError: "providerSpec.categories.value: Invalid value: \"val0123456789012345678901234567890123456789012345678901234567890123456789\": value must be a string with length between 1 and 64.",
		},
		{
			testCase: "with invalid gpu reference type provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.GPUs = append(p.GPUs, machinev1.NutanixGPU{Type: "invalid"})
			},
			expectedOk:    false,
			expectedError: "providerSpec.gpus.type: Invalid value: \"invalid\": invalid gpu identifier type, the valid types: \"DeviceID\", \"Name\".",
		},
		{
			testCase: "with too small diskSize provided",
			modifySpec: func(p *machinev1.NutanixMachineProviderConfig) {
				p.DataDisks = append(p.DataDisks, machinev1.NutanixVMDisk{DiskSize: resource.MustParse("500Mi")})
			},
			expectedOk:    false,
			expectedError: "providerSpec.dataDisks.diskSize: Invalid value: \"524288000 bytes\": The minimum diskSize is 1Gi bytes.",
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaultNutanixCredentialsSecret,
			Namespace: namespace.Name,
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(secret).Build()
	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.NutanixPlatformType

	gate, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Errorf("Unexpected error setting up feature gates: %v", err)
	}

	h := createMachineValidator(infra, c, plainDNS, gate)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &machinev1.NutanixMachineProviderConfig{
				VCPUSockets:    minNutanixCPUSockets,
				VCPUsPerSocket: minNutanixCPUPerSocket,
				MemorySize:     resource.MustParse(fmt.Sprintf("%dMi", minNutanixMemoryMiB)),
				SystemDiskSize: resource.MustParse(fmt.Sprintf("%dGi", minNutanixDiskGiB)),
				Subnets: []machinev1.NutanixResourceIdentifier{
					{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("subnet-1")},
				},
				Cluster:           machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("cluster-1")},
				Image:             machinev1.NutanixResourceIdentifier{Type: machinev1.NutanixIdentifierName, Name: ptr.To[string]("image-1")},
				UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
				CredentialsSecret: &corev1.LocalObjectReference{Name: defaultNutanixCredentialsSecret},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &machinev1beta1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, webhookErr := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			if webhookErr == nil {
				if tc.expectedError != "" {
					t.Errorf("expected: %q, got: %v", tc.expectedError, webhookErr)
				}
			} else {
				if webhookErr.ToAggregate().Error() != tc.expectedError {
					t.Errorf("expected: %q, got: %q", tc.expectedError, webhookErr.ToAggregate().Error())
				}
			}

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestValidateAzureCapacityReservationGroupID(t *testing.T) {
	testCases := []struct {
		name        string
		inputID     string
		expectError bool
	}{
		{
			name:        "validation for capacityReservationGroupID should return nil error if field input is valid",
			inputID:     "/subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup",
			expectError: false,
		},
		{
			name:        "validation for capacityReservationGroupID should return error if field input does not start with '/'",
			inputID:     "subscriptions/00000000-0000-0000-0000-000000000000/resourceGroups/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup",
			expectError: true,
		},
		{
			name:        "validation for capacityReservationGroupID should return error if field input does not have field name subscriptions",
			inputID:     "/subscripti/00000000-0000-0000-0000-000000000000/resourceGroups/myResourceGroupName/providers/Microsoft.Compute/capacityReservationGroups/myCapacityReservationGroup",
			expectError: true,
		},
		{
			name:        "validation for capacityReservationGroupID should return error if field input is empty",
			inputID:     "",
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			err := validateAzureCapacityReservationGroupID(tc.inputID)
			if tc.expectError {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}
