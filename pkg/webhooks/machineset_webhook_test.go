package webhooks

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/util/intstr"

	. "github.com/onsi/gomega"
	osconfigv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"
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
				Object: &machinev1beta1.AWSMachineProviderConfig{},
			},
			expectedError: "providerSpec.ami: Required value: expected providerSpec.ami.id to be populated",
		},
		{
			name:         "with AWS and an AMI ID",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    "aws-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &machinev1beta1.AWSMachineProviderConfig{
					AMI: machinev1beta1.AWSResourceReference{
						ID: ptr.To[string]("ami"),
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
				Object: &machinev1beta1.AzureMachineProviderSpec{},
			},
			expectedError: "providerSpec.osDisk.diskSizeGB: Invalid value: 0: diskSizeGB must be greater than zero and less than 32768",
		},
		{
			name:         "with Azure and a location and disk size set",
			platformType: osconfigv1.AzurePlatformType,
			clusterID:    "azure-cluster",
			providerSpecValue: &runtime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					Location: "location",
					OSDisk: machinev1beta1.OSDisk{
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
			providerSpecValue: &runtime.RawExtension{
				Object: &machinev1beta1.AzureMachineProviderSpec{
					OSDisk: machinev1beta1.OSDisk{
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
				Object: &machinev1beta1.GCPMachineProviderSpec{},
			},
			expectedError: "providerSpec.region: Required value: region is required",
		},
		{
			name:         "with GCP and the region and zone set",
			platformType: osconfigv1.GCPPlatformType,
			clusterID:    "gcp-cluster",
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
				Object: &machinev1beta1.VSphereMachineProviderSpec{},
			},
			expectedError: "[providerSpec.template: Required value: template must be provided, providerSpec.workspace: Required value: workspace must be provided, providerSpec.network.devices: Required value: at least 1 network device must be provided]",
		},
		{
			name:         "with vSphere and the template, workspace and network devices set",
			platformType: osconfigv1.VSpherePlatformType,
			clusterID:    "vsphere-cluster",
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
			providerSpecValue: &runtime.RawExtension{
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
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

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

			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			machineSetDefaulter := createMachineSetDefaulter(platformStatus, tc.clusterID)
			machineSetValidator := createMachineSetValidator(infra, c, dns, gate)
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

			ms := &machinev1beta1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-creation-",
					Namespace:    namespace.Name,
				},
				Spec: machinev1beta1.MachineSetSpec{
					Template: machinev1beta1.MachineTemplateSpec{
						Spec: machinev1beta1.MachineSpec{
							ProviderSpec: machinev1beta1.ProviderSpec{
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

				statusError := &apierrors.StatusError{}
				gs.Expect(errors.As(err, &statusError)).To(BeTrue())

				gs.Expect(statusError.Status().Message).To(ContainSubstring(tc.expectedError))
			} else {
				gs.Expect(err).To(BeNil())
			}
		})
	}
}

func TestMachineSetUpdate(t *testing.T) {
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
			DiskSizeGB: 128,
			OSType:     defaultAzureOSDiskOSType,
			ManagedDisk: machinev1beta1.OSDiskManagedDiskParameters{
				StorageAccountType: defaultAzureOSDiskStorageType,
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

	testCases := []struct {
		name                     string
		platformType             osconfigv1.PlatformType
		clusterID                string
		expectedError            string
		baseProviderSpecValue    *runtime.RawExtension
		updatedProviderSpecValue func() *runtime.RawExtension
		updateMachineSet         func(ms *machinev1beta1.MachineSet)
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
				object.Network = machinev1beta1.NetworkSpec{}
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.devices: Required value: at least 1 network device must be provided",
		},
		{
			name:         "with a modification to the selector",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updateMachineSet: func(ms *machinev1beta1.MachineSet) {
				ms.Spec.Selector.MatchLabels["foo"] = "bar"
			},
			expectedError: "[spec.selector: Forbidden: selector is immutable, spec.template.metadata.labels: Invalid value: map[string]string{\"machineset-name\":\"machineset-update-abcd\"}: `selector` does not match template `labels`]",
		},
		{
			name:         "with an incompatible template labels",
			platformType: osconfigv1.AWSPlatformType,
			clusterID:    vsphereClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultAWSProviderSpec.DeepCopy(),
			},
			updateMachineSet: func(ms *machinev1beta1.MachineSet) {
				ms.Spec.Template.ObjectMeta.Labels = map[string]string{
					"foo": "bar",
				}
			},
			expectedError: "spec.template.metadata.labels: Invalid value: map[string]string{\"foo\":\"bar\"}: `selector` does not match template `labels`",
		},
		{
			name:         "with a valid PowerVS ProviderSpec",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				return &runtime.RawExtension{
					Object: defaultPowerVSProviderSpec.DeepCopy(),
				}
			},
			expectedError: "",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the serviceInstanceID",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.ServiceInstance = machinev1.PowerVSResource{}
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.serviceInstance: Required value: serviceInstance identifier must be provided",
		},
		{
			name:         "with a PowerVS ProviderSpec, removing the UserDataSecret",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.UserDataSecret = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.userDataSecret: Required value: providerSpec.userDataSecret must be provided",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the CredentialsSecret",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.CredentialsSecret = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.credentialsSecret: Required value: providerSpec.credentialsSecret must be provided",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the keyPairName",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.KeyPairName = ""
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.keyPairName: Required value: providerSpec.keyPairName must be provided",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the Image Name",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.Image.Name = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.image.name: Required value: image identifier is specified as Name but the value is nil",
		},
		{
			name:         "with an PowerVS ProviderSpec, removing the Network Name",
			platformType: osconfigv1.PowerVSPlatformType,
			clusterID:    powerVSClusterID,
			baseProviderSpecValue: &runtime.RawExtension{
				Object: defaultPowerVSProviderSpec.DeepCopy(),
			},
			updatedProviderSpecValue: func() *runtime.RawExtension {
				object := defaultPowerVSProviderSpec.DeepCopy()
				object.Network.Name = nil
				return &runtime.RawExtension{
					Object: object,
				}
			},
			expectedError: "providerSpec.network.name: Required value: network identifier is specified as Name but the value is nil",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			gs := NewWithT(t)

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

			infra := plainInfra.DeepCopy()
			infra.Status.InfrastructureName = tc.clusterID
			infra.Status.PlatformStatus = platformStatus

			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			machineSetDefaulter := createMachineSetDefaulter(platformStatus, tc.clusterID)
			machineSetValidator := createMachineSetValidator(infra, c, plainDNS, gate)
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

			msLabel := "machineset-name"
			msLabelValue := "machineset-update-abcd"

			ms := &machinev1beta1.MachineSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "machineset-update-",
					Namespace:    namespace.Name,
				},
				Spec: machinev1beta1.MachineSetSpec{
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							msLabel: msLabelValue,
						},
					},
					Template: machinev1beta1.MachineTemplateSpec{
						ObjectMeta: machinev1beta1.ObjectMeta{
							Labels: map[string]string{
								msLabel: msLabelValue,
							},
						},
						Spec: machinev1beta1.MachineSpec{
							ProviderSpec: machinev1beta1.ProviderSpec{
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

			if tc.updatedProviderSpecValue != nil {
				ms.Spec.Template.Spec.ProviderSpec.Value = tc.updatedProviderSpecValue()
			}
			if tc.updateMachineSet != nil {
				tc.updateMachineSet(ms)
			}
			err = c.Update(ctx, ms)
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
