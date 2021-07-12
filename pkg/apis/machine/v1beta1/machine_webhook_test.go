package v1beta1

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"testing"

	. "github.com/onsi/gomega"
	osconfigv1 "github.com/openshift/api/config/v1"
	gcp "github.com/openshift/cluster-api-provider-gcp/pkg/apis/gcpprovider/v1beta1"
	vsphere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	azure "sigs.k8s.io/cluster-api-provider-azure/pkg/apis/azureprovider/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	yaml "sigs.k8s.io/yaml"
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
	g.Expect(c.Create(ctx, awsSecret)).To(Succeed())
	g.Expect(c.Create(ctx, vSphereSecret)).To(Succeed())
	g.Expect(c.Create(ctx, GCPSecret)).To(Succeed())
	g.Expect(c.Create(ctx, azureSecret)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, awsSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, vSphereSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, GCPSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, azureSecret)).To(Succeed())
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
				Object: &aws.AWSMachineProviderConfig{},
			},
			expectedError: "providerSpec.ami: Required value: expected providerSpec.ami.id to be populated",
		},
		{
			name:         "with AWS and AMI ARN set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{
					AMI: aws.AWSResourceReference{
						ID:  pointer.StringPtr("ami"),
						ARN: pointer.StringPtr("arn"),
					},
				},
			},
			expectedError: "providerSpec.ami.arn: Invalid value: \"arn\": only providerSpec.ami.id can be used to reference AMI",
		},
		{
			name:         "with AWS and AMI Filters set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{
					AMI: aws.AWSResourceReference{
						ID: pointer.StringPtr("ami"),
						Filters: []aws.Filter{
							{
								Name: "filter",
							},
						},
					},
				},
			},
			expectedError: "providerSpec.ami.filters: Invalid value: []v1beta1.Filter{v1beta1.Filter{Name:\"filter\", Values:[]string(nil)}}: only providerSpec.ami.id can be used to reference AMI",
		},
		{
			name:         "with AWS and an AMI ID set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{
					AMI: aws.AWSResourceReference{
						ID: pointer.StringPtr("ami"),
					},
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
				Object: &azure.AzureMachineProviderSpec{},
			},
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero and less than 32768",
		},
		{
			name:         "with Azure and a disk size set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{
					OSDisk: azure.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
			expectedError: "",
		},
		{
			name:         "with Azure disconnected installation request public IP",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{
					OSDisk: azure.OSDisk{
						DiskSizeGB: 128,
					},
					PublicIP: true,
				},
			},
			disconnected:  true,
			expectedError: "providerSpec.publicIP: Forbidden: publicIP is not allowed in Azure disconnected installation",
		},
		{
			name:         "with Azure disconnected installation success",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{
					OSDisk: azure.OSDisk{
						DiskSizeGB: 128,
					},
				},
			},
			disconnected: true,
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
				Object: &gcp.GCPMachineProviderSpec{},
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with GCP and the region and zone set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &kruntime.RawExtension{
				Object: &gcp.GCPMachineProviderSpec{
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
				Object: &vsphere.VSphereMachineProviderSpec{},
			},
			expectedError: "[providerSpec.template: Required value: template must be provided, providerSpec.workspace: Required value: workspace must be provided, providerSpec.network.devices: Required value: at least 1 network device must be provided]",
		},
		{
			name:            "with vSphere and the template, workspace and network devices set",
			platformType:    osconfigv1.VSpherePlatformType,
			clusterID:       "vsphere-cluster",
			presetClusterID: true,
			providerSpecValue: &kruntime.RawExtension{
				Object: &vsphere.VSphereMachineProviderSpec{
					Template: "template",
					Workspace: &vsphere.Workspace{
						Datacenter: "datacenter",
						Server:     "server",
					},
					Network: vsphere.NetworkSpec{
						Devices: []vsphere.NetworkDeviceSpec{
							{
								NetworkName: "networkName",
							},
						},
					},
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
			machineDefaulter := createMachineDefaulter(platformStatus, tc.clusterID)
			machineValidator := createMachineValidator(infra, c, dns)
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

			presetClusterID := "anything"
			if tc.presetClusterID {
				m.Labels = make(map[string]string)
				m.Labels[MachineClusterIDLabel] = presetClusterID
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
				if tc.presetClusterID {
					gs.Expect(m.Labels[MachineClusterIDLabel]).To(BeIdenticalTo(presetClusterID))
				} else {
					gs.Expect(m.Labels[MachineClusterIDLabel]).To(BeIdenticalTo(tc.clusterID))
				}
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestMachineUpdate(t *testing.T) {
	awsClusterID := "aws-cluster"
	awsRegion := "region"
	defaultAWSProviderSpec := &aws.AWSMachineProviderConfig{
		AMI: aws.AWSResourceReference{
			ID: pointer.StringPtr("ami"),
		},
		InstanceType:      defaultAWSX86InstanceType,
		UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
		CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
		Placement: aws.Placement{
			Region: awsRegion,
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
			ManagedDisk: azure.ManagedDiskParameters{
				StorageAccountType: defaultAzureOSDiskStorageType,
			},
		},
	}

	gcpClusterID := "gcp-cluster"
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
				Image:      defaultGCPDiskImage,
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
	defaultVSphereProviderSpec := &vsphere.VSphereMachineProviderSpec{
		Template: "template",
		Workspace: &vsphere.Workspace{
			Datacenter: "datacenter",
			Server:     "server",
		},
		Network: vsphere.NetworkSpec{
			Devices: []vsphere.NetworkDeviceSpec{
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
	g.Expect(c.Create(ctx, awsSecret)).To(Succeed())
	g.Expect(c.Create(ctx, vSphereSecret)).To(Succeed())
	g.Expect(c.Create(ctx, GCPSecret)).To(Succeed())
	g.Expect(c.Create(ctx, azureSecret)).To(Succeed())
	defer func() {
		g.Expect(c.Delete(ctx, awsSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, vSphereSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, GCPSecret)).To(Succeed())
		g.Expect(c.Delete(ctx, azureSecret)).To(Succeed())
	}()

	testCases := []struct {
		name                     string
		platformType             osconfigv1.PlatformType
		clusterID                string
		expectedError            string
		baseProviderSpecValue    *kruntime.RawExtension
		updatedProviderSpecValue func() *kruntime.RawExtension
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
				object.Network = vsphere.NetworkSpec{}
				return &kruntime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
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
			machineDefaulter := createMachineDefaulter(platformStatus, tc.clusterID)
			machineValidator := createMachineValidator(infra, c, plainDNS)
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
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "aws-validation-test",
		},
	}

	testCases := []struct {
		testCase         string
		modifySpec       func(*aws.AWSMachineProviderConfig)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with no ami values it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.AMI = aws.AWSResourceReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.ami: Required value: expected providerSpec.ami.id to be populated",
		},
		{
			testCase: "with no region values it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.Placement.Region = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.placement.region: Required value: expected providerSpec.placement.region to be populated",
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
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no subnet values it fails",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.Subnet = aws.AWSResourceReference{}
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
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.Placement.Tenancy = aws.DedicatedTenancy
			},
			expectedOk: true,
		},
		{
			testCase: "with empty tenancy field",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.Placement.Tenancy = ""
			},
			expectedOk: true,
		},
		{
			testCase: "fail with invalid tenancy field",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.Placement.Tenancy = "invalid"
			},
			expectedOk:    false,
			expectedError: "providerSpec.tenancy: Invalid value: \"invalid\": Invalid providerSpec.tenancy, the only allowed options are: default, dedicated, host",
		},
		{
			testCase: "with no iam instance profile",
			modifySpec: func(p *aws.AWSMachineProviderConfig) {
				p.IAMInstanceProfile = nil
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.iamInstanceProfile: no IAM instance profile provided: nodes may be unable to join the cluster"},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "secret",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewFakeClientWithScheme(scheme.Scheme, secret)

	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.AWSPlatformType
	h := createMachineValidator(infra, c, plainDNS)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &aws.AWSMachineProviderConfig{
				AMI: aws.AWSResourceReference{
					ID: pointer.StringPtr("ami"),
				},
				Placement: aws.Placement{
					Region: "region",
				},
				InstanceType: "m5.large",
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

			m := &Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultAWSProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	region := "region"
	arch := defaultAWSX86InstanceType
	if runtime.GOARCH == "arm64" {
		arch = defaultAWSARMInstanceType
	}
	testCases := []struct {
		testCase             string
		providerSpec         *aws.AWSMachineProviderConfig
		expectedProviderSpec *aws.AWSMachineProviderConfig
		expectedError        string
		expectedOk           bool
		expectedWarnings     []string
	}{
		{
			testCase: "it defaults Region, InstanceType, UserDataSecret and CredentialsSecret",
			providerSpec: &aws.AWSMachineProviderConfig{
				AMI:               aws.AWSResourceReference{},
				InstanceType:      "",
				UserDataSecret:    nil,
				CredentialsSecret: nil,
			},
			expectedProviderSpec: &aws.AWSMachineProviderConfig{
				AMI:               aws.AWSResourceReference{},
				InstanceType:      arch,
				UserDataSecret:    &corev1.LocalObjectReference{Name: defaultUserDataSecret},
				CredentialsSecret: &corev1.LocalObjectReference{Name: defaultAWSCredentialsSecret},
				Placement: aws.Placement{
					Region: "region",
				},
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: nil,
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
			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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
		modifySpec          func(providerSpec *azure.AzureMachineProviderSpec)
		azurePlatformStatus *osconfigv1.AzurePlatformStatus
		expectedError       string
		expectedOk          bool
		expectedWarnings    []string
	}{
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
			testCase: "with no image it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.image: Required value: an image reference must be provided",
		},
		{
			testCase: "with resourceId and other fields set it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
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
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
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
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
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
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
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
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
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
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
					ResourceID: "rid",
				}
			},
			expectedOk: true,
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
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no credentials secret name it fails",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.CredentialsSecret = &corev1.SecretReference{
					Namespace: namespace.Name,
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
					ManagedDisk: azure.ManagedDiskParameters{
						StorageAccountType: "storageAccountType",
					},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero and less than 32768",
		},
		{
			testCase:      "with all required fields it succeeds",
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "with government cloud and spot VMs enabled",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.SpotVMOptions = &azure.SpotVMOptions{}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzureUSGovernmentCloud,
			},
			expectedOk:       true,
			expectedWarnings: []string{"spot VMs may not be supported when using GovCloud region"},
		},
		{
			testCase: "with public cloud and spot VMs enabled",
			modifySpec: func(p *azure.AzureMachineProviderSpec) {
				p.SpotVMOptions = &azure.SpotVMOptions{}
			},
			azurePlatformStatus: &osconfigv1.AzurePlatformStatus{
				CloudName: osconfigv1.AzurePublicCloud,
			},
			expectedOk: true,
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
			c := fake.NewFakeClientWithScheme(scheme.Scheme, secret)
			infra := plainInfra.DeepCopy()
			infra.Status.InfrastructureName = "clusterID"
			infra.Status.PlatformStatus.Type = osconfigv1.AzurePlatformType
			infra.Status.PlatformStatus.Azure = tc.azurePlatformStatus

			h := createMachineValidator(infra, c, plainDNS)

			// create a valid spec that will then be 'broken' by modifySpec
			providerSpec := &azure.AzureMachineProviderSpec{
				VMSize: "vmSize",
				Image: azure.Image{
					ResourceID: "resourceID",
				},
				UserDataSecret: &corev1.SecretReference{
					Name: "name",
				},
				CredentialsSecret: &corev1.SecretReference{
					Name:      "name",
					Namespace: namespace.Name,
				},
				OSDisk: azure.OSDisk{
					DiskSizeGB: 1,
				},
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultAzureProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	testCases := []struct {
		testCase         string
		providerSpec     *azure.AzureMachineProviderSpec
		modifyDefault    func(*azure.AzureMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &azure.AzureMachineProviderSpec{},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "it does not override azure image spec",
			providerSpec: &azure.AzureMachineProviderSpec{
				Image: azure.Image{
					Offer:     "test-offer",
					SKU:       "test-sku",
					Publisher: "base-publisher",
					Version:   "1",
				},
			},
			modifyDefault: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
					Offer:     "test-offer",
					SKU:       "test-sku",
					Publisher: "base-publisher",
					Version:   "1",
				}
			},
			expectedOk:    true,
			expectedError: "",
		},
		{
			testCase: "it does not override azure image ResourceID",
			providerSpec: &azure.AzureMachineProviderSpec{
				Image: azure.Image{
					ResourceID: "rid",
				},
			},
			modifyDefault: func(p *azure.AzureMachineProviderSpec) {
				p.Image = azure.Image{
					ResourceID: "rid",
				}
			},
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
				VMSize: defaultAzureVMSize,
				Vnet:   defaultAzureVnet(clusterID),
				Subnet: defaultAzureSubnet(clusterID),
				Image: azure.Image{
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

			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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
		modifySpec       func(*gcp.GCPMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
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
			testCase: "with no service accounts",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.ServiceAccounts = nil
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.serviceAccounts: no service account provided: nodes may be unable to join the cluster"},
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
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *gcp.GCPMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
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

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewFakeClientWithScheme(scheme.Scheme, secret)
	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.GCPPlatformType
	h := createMachineValidator(infra, c, plainDNS)

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
			m := &Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}

func TestDefaultGCPProviderSpec(t *testing.T) {

	clusterID := "clusterID"
	projectID := "projectID"
	testCases := []struct {
		testCase         string
		providerSpec     *gcp.GCPMachineProviderSpec
		modifyDefault    func(*gcp.GCPMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
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
						Image:      defaultGCPDiskImage,
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
					Image:      defaultGCPDiskImage,
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
			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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
		testCase         string
		modifySpec       func(*vsphere.VSphereMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase: "with no template provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Template = ""
			},
			expectedOk:    false,
			expectedError: "providerSpec.template: Required value: template must be provided",
		},
		{
			testCase: "with no workspace provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Workspace = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace: Required value: workspace must be provided",
		},
		{
			testCase: "with no workspace server provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Workspace = &vsphere.Workspace{
					Datacenter: "datacenter",
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.workspace.server: Required value: server must be provided",
		},
		{
			testCase: "with no workspace datacenter provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Workspace = &vsphere.Workspace{
					Server: "server",
				}
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.workspace.datacenter: datacenter is unset: if more than one datacenter is present, VMs cannot be created"},
		},
		{
			testCase: "with a workspace folder outside of the current datacenter",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Workspace = &vsphere.Workspace{
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
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Network = vsphere.NetworkSpec{
					Devices: []vsphere.NetworkDeviceSpec{},
				}
			},
			expectedOk:    false,
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
		},
		{
			testCase: "with no network device name provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.Network = vsphere.NetworkSpec{
					Devices: []vsphere.NetworkDeviceSpec{
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
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.NumCPUs = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.numCPUs: 1 is missing or less than the minimum value (2): nodes may not boot correctly"},
		},
		{
			testCase: "with too little memory provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.MemoryMiB = 1024
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.memoryMiB: 1024 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with too little disk size provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.DiskGiB = 1
			},
			expectedOk:       true,
			expectedError:    "",
			expectedWarnings: []string{"providerSpec.diskGiB: 1 is missing or less than the recommended minimum (120): nodes may fail to start if disk size is too low"},
		},
		{
			testCase: "with no user data secret provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.UserDataSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret: Required value: userDataSecret must be provided",
		},
		{
			testCase: "with no user data secret name provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.UserDataSecret = &corev1.LocalObjectReference{}
			},
			expectedOk:    false,
			expectedError: "providerSpec.userDataSecret.name: Required value: name must be provided",
		},
		{
			testCase: "with no credentials secret provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.CredentialsSecret = nil
			},
			expectedOk:    false,
			expectedError: "providerSpec.credentialsSecret: Required value: credentialsSecret must be provided",
		},
		{
			testCase: "when the credentials secret does not exist",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.CredentialsSecret.Name = "does-not-exist"
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.credentialsSecret: Invalid value: \"does-not-exist\": not found. Expected CredentialsSecret to exist"},
		},
		{
			testCase: "with no credentials secret name provided",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
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
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.NumCPUs = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.numCPUs: 0 is missing or less than the minimum value (2): nodes may not boot correctly"},
		},
		{
			testCase: "with memoryMiB equal to 0",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.MemoryMiB = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.memoryMiB: 0 is missing or less than the recommended minimum value (2048): nodes may not boot correctly"},
		},
		{
			testCase: "with diskGiB equal to 0",
			modifySpec: func(p *vsphere.VSphereMachineProviderSpec) {
				p.DiskGiB = 0
			},
			expectedOk:       true,
			expectedWarnings: []string{"providerSpec.diskGiB: 0 is missing or less than the recommended minimum (120): nodes may fail to start if disk size is too low"},
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "name",
			Namespace: namespace.Name,
		},
	}
	c := fake.NewFakeClientWithScheme(scheme.Scheme, secret)
	infra := plainInfra.DeepCopy()
	infra.Status.InfrastructureName = "clusterID"
	infra.Status.PlatformStatus.Type = osconfigv1.VSpherePlatformType
	h := createMachineValidator(infra, c, plainDNS)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			providerSpec := &vsphere.VSphereMachineProviderSpec{
				Template: "template",
				Workspace: &vsphere.Workspace{
					Datacenter: "datacenter",
					Server:     "server",
				},
				Network: vsphere.NetworkSpec{
					Devices: []vsphere.NetworkDeviceSpec{
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
			}
			if tc.modifySpec != nil {
				tc.modifySpec(providerSpec)
			}

			m := &Machine{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: namespace.Name,
				},
			}
			rawBytes, err := json.Marshal(providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
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
		providerSpec     *vsphere.VSphereMachineProviderSpec
		modifyDefault    func(*vsphere.VSphereMachineProviderSpec)
		expectedError    string
		expectedOk       bool
		expectedWarnings []string
	}{
		{
			testCase:      "it defaults defaultable fields",
			providerSpec:  &vsphere.VSphereMachineProviderSpec{},
			expectedOk:    true,
			expectedError: "",
		},
	}

	platformStatus := &osconfigv1.PlatformStatus{Type: osconfigv1.VSpherePlatformType}
	h := createMachineDefaulter(platformStatus, clusterID)

	for _, tc := range testCases {
		t.Run(tc.testCase, func(t *testing.T) {
			defaultProviderSpec := &vsphere.VSphereMachineProviderSpec{
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

			m := &Machine{}
			rawBytes, err := json.Marshal(tc.providerSpec)
			if err != nil {
				t.Fatal(err)
			}
			m.Spec.ProviderSpec.Value = &kruntime.RawExtension{Raw: rawBytes}

			ok, warnings, err := h.webhookOperations(m, h.admissionConfig)
			if ok != tc.expectedOk {
				t.Errorf("expected: %v, got: %v", tc.expectedOk, ok)
			}

			gotProviderSpec := new(vsphere.VSphereMachineProviderSpec)
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

			if !reflect.DeepEqual(warnings, tc.expectedWarnings) {
				t.Errorf("expected: %q, got: %q", tc.expectedWarnings, warnings)
			}
		})
	}
}
