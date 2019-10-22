package operator

import (
	"fmt"
	"testing"
	"time"

	openshiftv1 "github.com/openshift/api/config/v1"
	fakeos "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
)

const (
	deploymentName   = "machine-api-controllers"
	targetNamespace  = "test-namespace"
	hcControllerName = "machine-healthcheck-controller"
)

func newFeatureGate(featureSet openshiftv1.FeatureSet) *openshiftv1.FeatureGate {
	return &openshiftv1.FeatureGate{
		ObjectMeta: metav1.ObjectMeta{
			Name: MachineAPIFeatureGateName,
		},
		Spec: openshiftv1.FeatureGateSpec{
			FeatureSet: featureSet,
		},
	}
}

func newOperatorConfig() *OperatorConfig {
	baremetalControllers := BaremetalControllers{}

	return &OperatorConfig{
		targetNamespace,
		Controllers{
			"docker.io/openshift/origin-aws-machine-controllers:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
		},
		baremetalControllers,
	}
}

func newFakeOperator(kubeObjects []runtime.Object, osObjects []runtime.Object, stopCh <-chan struct{}) *Operator {
	kubeClient := fakekube.NewSimpleClientset(kubeObjects...)
	osClient := fakeos.NewSimpleClientset(osObjects...)
	kubeNamespacedSharedInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 2*time.Minute, informers.WithNamespace(targetNamespace))
	configSharedInformer := configinformersv1.NewSharedInformerFactoryWithOptions(osClient, 2*time.Minute)
	featureGateInformer := configSharedInformer.Config().V1().FeatureGates()
	deployInformer := kubeNamespacedSharedInformer.Apps().V1().Deployments()

	optr := &Operator{
		kubeClient:             kubeClient,
		osClient:               osClient,
		featureGateLister:      featureGateInformer.Lister(),
		deployLister:           deployInformer.Lister(),
		imagesFile:             "fixtures/images.json",
		namespace:              targetNamespace,
		eventRecorder:          record.NewFakeRecorder(50),
		queue:                  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineapioperator"),
		deployListerSynced:     deployInformer.Informer().HasSynced,
		featureGateCacheSynced: featureGateInformer.Informer().HasSynced,
	}

	configSharedInformer.Start(stopCh)
	kubeNamespacedSharedInformer.Start(stopCh)

	optr.syncHandler = optr.sync
	deployInformer.Informer().AddEventHandler(optr.eventHandlerDeployments())
	featureGateInformer.Informer().AddEventHandler(optr.eventHandler())

	return optr
}

// TestOperatorSync_NoOp tests syncing to ensure that the mao reports available
// for platforms that are no-ops.
func TestOperatorSync_NoOp(t *testing.T) {
	cases := []struct {
		platform     openshiftv1.PlatformType
		expectedNoop bool
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
			expectedNoop: true,
		},
		{
			platform:     openshiftv1.NonePlatformType,
			expectedNoop: true,
		},
		{
			platform:     "bad-platform",
			expectedNoop: true,
		},
	}

	for _, tc := range cases {
		t.Run(string(tc.platform), func(t *testing.T) {
			infra := &openshiftv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: openshiftv1.InfrastructureStatus{
					Platform: tc.platform,
				},
			}

			stopCh := make(<-chan struct{})
			optr := newFakeOperator(nil, []runtime.Object{newFeatureGate(openshiftv1.TechPreviewNoUpgrade), infra}, stopCh)
			go optr.Run(2, stopCh)

			err := wait.PollImmediate(1*time.Second, 5*time.Second, func() (bool, error) {
				_, err := optr.deployLister.Deployments(targetNamespace).Get(deploymentName)
				if err != nil {
					t.Logf("Failed to get %q deployment: %v", deploymentName, err)
					return false, nil
				}
				return true, nil
			})

			if tc.expectedNoop != (err != nil) {
				t.Errorf("Failed to verify deployment %q with platform %s", deploymentName, tc.platform)
			}

			o, err := optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
			if !assert.NoError(t, err, "failed to get clusteroperator") {
				t.Fatal()
			}
			expectedConditions := map[openshiftv1.ClusterStatusConditionType]openshiftv1.ConditionStatus{
				openshiftv1.OperatorAvailable:   openshiftv1.ConditionTrue,
				openshiftv1.OperatorProgressing: openshiftv1.ConditionFalse,
				openshiftv1.OperatorDegraded:    openshiftv1.ConditionFalse,
				openshiftv1.OperatorUpgradeable: openshiftv1.ConditionTrue,
			}
			for _, c := range o.Status.Conditions {
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
