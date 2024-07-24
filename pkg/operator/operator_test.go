package operator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	openshiftv1 "github.com/openshift/api/config/v1"
	apifeatures "github.com/openshift/api/features"
	fakeos "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions"
	fakemachine "github.com/openshift/client-go/machine/clientset/versioned/fake"
	"github.com/openshift/library-go/pkg/operator/configobserver/featuregates"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	deploymentName  = "machine-api-controllers"
	targetNamespace = "test-namespace"
	releaseVersion  = "0.0.0.test-unit"
)

func newFakeOperator(kubeObjects, osObjects, machineObjects []runtime.Object, imagesFile string, fg *openshiftv1.FeatureGate, stopCh <-chan struct{}) (*Operator, error) {
	kubeClient := fakekube.NewSimpleClientset(kubeObjects...)
	osClient := fakeos.NewSimpleClientset(osObjects...)
	machineClient := fakemachine.NewSimpleClientset(machineObjects...)
	dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme.Scheme, kubeObjects...)
	kubeNamespacedSharedInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 2*time.Minute, informers.WithNamespace(targetNamespace))
	configSharedInformer := configinformersv1.NewSharedInformerFactoryWithOptions(osClient, 2*time.Minute)
	deployInformer := kubeNamespacedSharedInformer.Apps().V1().Deployments()
	proxyInformer := configSharedInformer.Config().V1().Proxies()
	daemonsetInformer := kubeNamespacedSharedInformer.Apps().V1().DaemonSets()
	mutatingWebhookInformer := kubeNamespacedSharedInformer.Admissionregistration().V1().MutatingWebhookConfigurations()
	validatingWebhookInformer := kubeNamespacedSharedInformer.Admissionregistration().V1().ValidatingWebhookConfigurations()

	if fg == nil {
		fg = &openshiftv1.FeatureGate{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Status: openshiftv1.FeatureGateStatus{
				FeatureGates: []openshiftv1.FeatureGateDetails{
					{
						Version:  "",
						Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
						Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
					},
				},
			},
		}
	}
	featureGateAccessor, err := featuregates.NewHardcodedFeatureGateAccessFromFeatureGate(fg, "")
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to deployments informer: %v", err)
	}

	optr := &Operator{
		kubeClient:                    kubeClient,
		osClient:                      osClient,
		machineClient:                 machineClient,
		dynamicClient:                 dynamicClient,
		deployLister:                  deployInformer.Lister(),
		proxyLister:                   proxyInformer.Lister(),
		daemonsetLister:               daemonsetInformer.Lister(),
		mutatingWebhookLister:         mutatingWebhookInformer.Lister(),
		validatingWebhookLister:       validatingWebhookInformer.Lister(),
		featureGateAccessor:           featureGateAccessor,
		imagesFile:                    imagesFile,
		namespace:                     targetNamespace,
		eventRecorder:                 record.NewFakeRecorder(50),
		queue:                         workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineapioperator"),
		deployListerSynced:            deployInformer.Informer().HasSynced,
		proxyListerSynced:             proxyInformer.Informer().HasSynced,
		daemonsetListerSynced:         daemonsetInformer.Informer().HasSynced,
		cache:                         resourceapply.NewResourceCache(),
		mutatingWebhookListerSynced:   mutatingWebhookInformer.Informer().HasSynced,
		validatingWebhookListerSynced: validatingWebhookInformer.Informer().HasSynced,
	}

	configSharedInformer.Start(stopCh)
	kubeNamespacedSharedInformer.Start(stopCh)

	optr.syncHandler = optr.sync
	_, err = deployInformer.Informer().AddEventHandler(optr.eventHandlerDeployments())
	if err != nil {
		return nil, fmt.Errorf("error adding event handler to deployments informer: %v", err)
	}

	optr.operandVersions = []openshiftv1.OperandVersion{
		{Name: "operator", Version: releaseVersion},
	}

	return optr, nil
}

// TestOperatorSync_NoOp tests syncing to ensure that the mao reports available
// for platforms that are no-ops.
func TestOperatorSync_NoOp(t *testing.T) {
	cases := []struct {
		platform        openshiftv1.PlatformType
		expectedNoop    bool
		expectedMessage string
	}{
		{
			platform:     openshiftv1.AWSPlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.LibvirtPlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.OpenStackPlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.AzurePlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.BareMetalPlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.GCPPlatformType,
			expectedNoop: false,
		},
		{
			platform:     kubemarkPlatform,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.VSpherePlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.OvirtPlatformType,
			expectedNoop: false,
		},
		{
			platform:     openshiftv1.PowerVSPlatformType,
			expectedNoop: false,
		},
		{
			platform:        openshiftv1.NonePlatformType,
			expectedNoop:    true,
			expectedMessage: operatorStatusNoOpMessage,
		},
		{
			platform:        "bad-platform",
			expectedNoop:    true,
			expectedMessage: operatorStatusNoOpMessage,
		},
	}

	imagesJSONFile, err := createImagesJSONFromManifest()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	for _, tc := range cases {
		t.Run(string(tc.platform), func(t *testing.T) {
			infra := &openshiftv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.InfrastructureStatus{
					PlatformStatus: &openshiftv1.PlatformStatus{
						Type: tc.platform,
					},
				},
			}

			proxy := &openshiftv1.Proxy{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
			}

			stopCh := make(chan struct{})
			defer close(stopCh)
			optr, err := newFakeOperator(nil, []runtime.Object{infra, proxy}, nil, imagesJSONFile, nil, stopCh)
			if err != nil {
				t.Fatal(err)
			}
			optr.queue.Add("trigger")
			go optr.Run(1, stopCh)

			err = wait.PollUntilContextTimeout(context.Background(), 1*time.Second, 5*time.Second, true, func(_ context.Context) (bool, error) {
				_, err := optr.deployLister.Deployments(targetNamespace).Get(deploymentName)
				if err != nil {
					t.Logf("Failed to get %q deployment: %v", deploymentName, err)
					return false, nil
				}
				t.Logf("Found deployment: %q", deploymentName)
				return true, nil
			})

			var expectedConditions map[openshiftv1.ClusterStatusConditionType]openshiftv1.ConditionStatus

			if tc.expectedNoop {
				// The PollImmediate looking for the deployment above should
				// have failed in the case of a no-op platform.
				if err == nil {
					t.Error("Found deployment when expecting no-op sync")
				}

				// In this case, we expect to report available.
				expectedConditions = map[openshiftv1.ClusterStatusConditionType]openshiftv1.ConditionStatus{
					openshiftv1.OperatorAvailable:   openshiftv1.ConditionTrue,
					openshiftv1.OperatorProgressing: openshiftv1.ConditionFalse,
					openshiftv1.OperatorDegraded:    openshiftv1.ConditionFalse,
					openshiftv1.OperatorUpgradeable: openshiftv1.ConditionTrue,
				}

			} else {
				// If this wasn't a no-op, we expect to be progressing towards
				// the new version of the operands.
				expectedConditions = map[openshiftv1.ClusterStatusConditionType]openshiftv1.ConditionStatus{
					openshiftv1.OperatorAvailable:   openshiftv1.ConditionFalse,
					openshiftv1.OperatorProgressing: openshiftv1.ConditionTrue,
					openshiftv1.OperatorDegraded:    openshiftv1.ConditionFalse,
					openshiftv1.OperatorUpgradeable: openshiftv1.ConditionTrue,
				}
			}

			o, err := optr.osClient.ConfigV1().ClusterOperators().Get(context.Background(), clusterOperatorName, metav1.GetOptions{})
			if !assert.NoError(t, err, "failed to get clusteroperator") {
				t.Fatal()
			}

			for _, c := range o.Status.Conditions {
				// if expecting a Noop and the operator is available, then check to ensure that the proper message is displayed
				if tc.expectedNoop && c.Type == openshiftv1.OperatorAvailable && c.Status == openshiftv1.ConditionTrue {
					assert.Equal(t, tc.expectedMessage, c.Message)
				}
				assert.Equal(t, expectedConditions[c.Type], c.Status, fmt.Sprintf("unexpected clusteroperator condition %s status", c.Type))
			}
		})
	}
}

func TestIsOwned(t *testing.T) {
	testCases := []struct {
		testCase      string
		obj           interface{}
		expected      bool
		expectedError bool
	}{
		{
			testCase: "with maoOwnedAnnotation returns true",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						maoOwnedAnnotation: "",
					},
				},
			},
			expected: true,
		},
		{
			testCase: "with no maoOwnedAnnotation returns false",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"any": "",
					},
				},
			},
			expected: false,
		},
		{
			testCase:      "bad type object returns error",
			obj:           "bad object",
			expected:      false,
			expectedError: true,
		},
	}
	for _, tc := range testCases {
		t.Run(string(tc.testCase), func(t *testing.T) {
			got, err := isOwned(tc.obj)
			if got != tc.expected {
				t.Errorf("Expected: %v, got: %v", tc.expected, got)
			}
			if tc.expectedError != (err != nil) {
				t.Errorf("ExpectedError: %v, got: %v", tc.expectedError, err)
			}
		})
	}
}

// TestMAOConfigFromInfrastructure tests that the expected config comes back
// for the given infrastructure
func TestMAOConfigFromInfrastructure(t *testing.T) {
	g := NewWithT(t)

	imagesJSONData, err := extractImagesJSONFromManifest()
	g.Expect(err).NotTo(HaveOccurred())

	images := &Images{}
	g.Expect(json.Unmarshal(imagesJSONData, images)).To(Succeed())
	// Make sure the images struct has been populated
	g.Expect(images.MachineAPIOperator).ToNot(BeEmpty())

	infra := &openshiftv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}

	proxy := &openshiftv1.Proxy{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}

	testCases := []struct {
		name           string
		platform       openshiftv1.PlatformType
		infra          *openshiftv1.Infrastructure
		featureGate    *openshiftv1.FeatureGate
		proxy          *openshiftv1.Proxy
		imagesFile     string
		expectedConfig *OperatorConfig
		expectedError  error
	}{
		{
			name:     string(openshiftv1.AWSPlatformType),
			platform: openshiftv1.AWSPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerAWS,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: images.ClusterAPIControllerAWS,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.AWSPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.LibvirtPlatformType),
			platform: openshiftv1.LibvirtPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerLibvirt,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.LibvirtPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.OpenStackPlatformType),
			platform: openshiftv1.OpenStackPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerOpenStack,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.OpenStackPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.AzurePlatformType),
			platform: openshiftv1.AzurePlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerAzure,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: images.ClusterAPIControllerAzure,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.AzurePlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.BareMetalPlatformType),
			platform: openshiftv1.BareMetalPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerBareMetal,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.BareMetalPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.GCPPlatformType),
			platform: openshiftv1.GCPPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerGCP,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: images.ClusterAPIControllerGCP,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.GCPPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(kubemarkPlatform),
			platform: kubemarkPlatform,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           clusterAPIControllerKubemark,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: kubemarkPlatform,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.VSpherePlatformType),
			platform: openshiftv1.VSpherePlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerVSphere,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.VSpherePlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.OvirtPlatformType),
			platform: openshiftv1.OvirtPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerOvirt,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.OvirtPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     string(openshiftv1.NonePlatformType),
			platform: openshiftv1.NonePlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           clusterAPIControllerNoOp,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.NonePlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			// MHC controller being enabled is the default for now (which is covered by all other tests),
			// but this test ensures that enabling works once the default changes
			name:     "mhc-controller-enabled",
			platform: openshiftv1.BareMetalPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerBareMetal,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.BareMetalPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:     "mhc-controller-disabled",
			platform: openshiftv1.BareMetalPlatformType,
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}, {Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           images.ClusterAPIControllerBareMetal,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: "",
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: openshiftv1.BareMetalPlatformType,
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": true,
				},
			},
		},
		{
			name:     "bad-platform",
			platform: "bad-platform",
			infra:    infra,
			featureGate: &openshiftv1.FeatureGate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.FeatureGateStatus{
					FeatureGates: []openshiftv1.FeatureGateDetails{
						{
							Version:  "",
							Enabled:  []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIMigration}},
							Disabled: []openshiftv1.FeatureGateAttributes{{Name: apifeatures.FeatureGateMachineAPIOperatorDisableMachineHealthCheckController}},
						},
					},
				},
			},
			proxy: proxy,
			expectedConfig: &OperatorConfig{
				TargetNamespace: targetNamespace,
				Proxy:           proxy,
				Controllers: Controllers{
					Provider:           clusterAPIControllerNoOp,
					MachineSet:         images.MachineAPIOperator,
					NodeLink:           images.MachineAPIOperator,
					MachineHealthCheck: images.MachineAPIOperator,
					TerminationHandler: clusterAPIControllerNoOp,
					KubeRBACProxy:      images.KubeRBACProxy,
				},
				PlatformType: "bad-platform",
				Features: map[string]bool{
					"MachineAPIMigration": true,
					"MachineAPIOperatorDisableMachineHealthCheckController": false,
				},
			},
		},
		{
			name:           "no-infra",
			platform:       "no-infra",
			infra:          nil,
			proxy:          proxy,
			expectedConfig: nil,
			expectedError:  kerrors.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster"),
		},
		{
			name:           "no-proxy",
			platform:       "no-proxy",
			infra:          infra,
			proxy:          nil,
			expectedConfig: nil,
			expectedError:  kerrors.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "proxies"}, "cluster"),
		},
		{
			name:           "no-platform",
			platform:       "",
			infra:          infra,
			proxy:          proxy,
			expectedConfig: nil,
			expectedError:  errors.New("no platform provider found on install config"),
		},
		{
			name:           "no-images-file",
			platform:       openshiftv1.NonePlatformType,
			infra:          infra,
			proxy:          proxy,
			imagesFile:     "fixtures/not-found.json",
			expectedConfig: nil,
			expectedError:  &os.PathError{Op: "open", Path: "fixtures/not-found.json", Err: syscall.ENOENT},
		},
	}

	imagesJSONFile, err := createImagesJSONFromManifest()
	g.Expect(err).ToNot(HaveOccurred())
	defer func() {
		if err := os.Remove(imagesJSONFile); err != nil {
			t.Fatal(err)
		}
	}()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			objects := []runtime.Object{}
			if tc.infra != nil {
				inf := tc.infra.DeepCopy()
				// Ensure platform is correct on infrastructure
				inf.Status.PlatformStatus = &openshiftv1.PlatformStatus{Type: tc.platform}
				objects = append(objects, inf)
			}
			if tc.proxy != nil {
				proxy := tc.proxy.DeepCopy()
				objects = append(objects, proxy)
			}

			stopCh := make(chan struct{})
			defer close(stopCh)
			optr, err := newFakeOperator(nil, objects, nil, imagesJSONFile, tc.featureGate, stopCh)
			if err != nil {
				t.Fatal(err)
			}
			optr.queue.Add("trigger")

			if tc.imagesFile != "" {
				optr.imagesFile = tc.imagesFile
			}

			go optr.Run(1, stopCh)

			config, err := optr.maoConfigFromInfrastructure()

			if tc.expectedError != nil {
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).To(BeNil())
			}

			g.Expect(config).To(Equal(tc.expectedConfig))
		})
	}
}
