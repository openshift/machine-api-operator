package v1beta1

import (
	"context"
	"fmt"
	"testing"

	. "github.com/onsi/gomega"
	osconfigv1 "github.com/openshift/api/config/v1"
	gcp "github.com/openshift/cluster-api-provider-gcp/pkg/apis/gcpprovider/v1beta1"
	vsphere "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/pointer"
	aws "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	azure "sigs.k8s.io/cluster-api-provider-azure/pkg/apis/azureprovider/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func TestMachineSetCreation(t *testing.T) {
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
			Name: "machineset-creation-test",
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
		expectedError     string
		disconnected      bool
		providerSpecValue *runtime.RawExtension
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
			providerSpecValue: &runtime.RawExtension{
				Object: &aws.AWSMachineProviderConfig{},
			},
			expectedError: "providerSpec.ami: Required value: expected providerSpec.ami.id to be populated",
		},
		{
			name:         "with AWS and AMI ARN set",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			name:         "with AWS and an AMI ID",
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
			name:              "with Azure and a nil provider spec value",
			platformType:      osconfigv1.AWSPlatformType,
			clusterID:         "azure-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
		},
		{
			name:         "with Azure and no fields set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &azure.AzureMachineProviderSpec{},
			},
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero and less than 32768",
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
			name:         "with Azure disconnected installation request public IP",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			platformType:      osconfigv1.AWSPlatformType,
			clusterID:         "gcp-cluster",
			providerSpecValue: nil,
			expectedError:     "providerSpec.value: Required value: a value must be provided",
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
			providerSpecValue: &runtime.RawExtension{
				Object: &vsphere.VSphereMachineProviderSpec{},
			},
			expectedError: "[providerSpec.template: Required value: template must be provided, providerSpec.workspace: Required value: workspace must be provided, providerSpec.network.devices: Required value: at least 1 network device must be provided]",
		},
		{
			name:         "with vSphere and the template, workspace and network devices set",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    "vsphere-cluster",
			providerSpecValue: &runtime.RawExtension{
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
			machineSetDefaulter := createMachineSetDefaulter(platformStatus, tc.clusterID)
			machineSetValidator := createMachineSetValidator(infra, c, dns)
			mgr.GetWebhookServer().Register(DefaultMachineSetMutatingHookPath, &webhook.Admission{Handler: machineSetDefaulter})
			mgr.GetWebhookServer().Register(DefaultMachineSetValidatingHookPath, &webhook.Admission{Handler: machineSetValidator})

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

			ms := &MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-creation-",
					Namespace:    namespace.Name,
				},
				Spec: MachineSetSpec{
					Template: MachineTemplateSpec{
						Spec: MachineSpec{
							ProviderSpec: ProviderSpec{
								Value: tc.providerSpecValue,
							},
						},
					},
				},
			}

			err = c.Create(ctx, ms)
			if err == nil {
				defer func() {
					gs.Expect(c.Delete(ctx, ms)).To(Succeed())
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

func TestMachineSetUpdate(t *testing.T) {
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
			Name: "machineset-update-test",
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
			name:         "with an AWS ProviderSpec, removing the region",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    awsClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultAWSProviderSpec.DeepCopy()
				object.Placement.Region = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.placement.region: Required value: expected providerSpec.placement.region to be populated",
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
			name:         "with a GCP ProviderSpec, removing the network interfaces",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    gcpClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultGCPProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultGCPProviderSpec.DeepCopy()
				object.NetworkInterfaces = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.networkInterfaces: Required value: at least 1 network interface is required",
		},
		{
			name:         "with a valid VSphere ProviderSpec",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultVSphereProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an VSphere ProviderSpec, removing the template",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Template = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.template: Required value: template must be provided",
		},
		{
			name:         "with an VSphere ProviderSpec, removing the workspace server",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Workspace.Server = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.workspace.server: Required value: server must be provided",
		},
		{
			name:         "with an VSphere ProviderSpec, removing the network devices",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultVSphereProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultVSphereProviderSpec.DeepCopy()
				object.Network = vsphere.NetworkSpec{}
				return &runtime.RawExtension{
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

			infra := plainInfra.DeepCopy()
			infra.Status.InfrastructureName = tc.clusterID
			infra.Status.PlatformStatus = platformStatus

			machineSetDefaulter := createMachineSetDefaulter(platformStatus, tc.clusterID)
			machineSetValidator := createMachineSetValidator(infra, c, plainDNS)
			mgr.GetWebhookServer().Register(DefaultMachineSetMutatingHookPath, &webhook.Admission{Handler: machineSetDefaulter})
			mgr.GetWebhookServer().Register(DefaultMachineSetValidatingHookPath, &webhook.Admission{Handler: machineSetValidator})

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

			ms := &MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-update-",
					Namespace:    namespace.Name,
				},
				Spec: MachineSetSpec{
					Template: MachineTemplateSpec{
						Spec: MachineSpec{
							ProviderSpec: ProviderSpec{
								Value: tc.baseProviderSpecValue,
							},
						},
					},
				},
			}
			err = c.Create(ctx, ms)
			gs.Expect(err).ToNot(HaveOccurred())
			defer func() {
				gs.Expect(c.Delete(ctx, ms)).To(Succeed())
			}()

			ms.Spec.Template.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			err = c.Update(ctx, ms)
			if tc.expectedError != "" {
				gs.Expect(err).ToNot(BeNil())
				gs.Expect(apierrors.ReasonForError(err)).To(BeEquivalentTo(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}
