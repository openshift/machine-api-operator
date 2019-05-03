package operator

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	osclientset "github.com/openshift/client-go/config/clientset/versioned"
	fakeconfigclientset "github.com/openshift/client-go/config/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	v1 "github.com/openshift/api/config/v1"
	fakeos "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions"
	appsv1 "k8s.io/api/apps/v1"
	fakekube "k8s.io/client-go/kubernetes/fake"
)

// TestOperatorSync_NoOp tests syncing to ensure that the mao reports available
// for platforms that are no-ops.
func TestOperatorSync_NoOp(t *testing.T) {
	cases := []struct {
		platform     configv1.PlatformType
		expectedNoop bool
	}{
		{
			platform:     configv1.AWSPlatformType,
			expectedNoop: false,
		},
		{
			platform:     configv1.LibvirtPlatformType,
			expectedNoop: false,
		},
		{
			platform:     configv1.OpenStackPlatformType,
			expectedNoop: false,
		},
		{
			platform:     configv1.AzurePlatformType,
			expectedNoop: false,
		},
		{
			platform:     configv1.BareMetalPlatformType,
			expectedNoop: false,
		},
		{
			platform:     kubemarkPlatform,
			expectedNoop: false,
		},
		{
			platform:     configv1.VSpherePlatformType,
			expectedNoop: true,
		},
		{
			platform:     configv1.NonePlatformType,
			expectedNoop: true,
		},
		{
			platform:     "bad-platform",
			expectedNoop: true,
		},
	}

	tempDir, err := ioutil.TempDir("", "TestOperatorSync")
	if err != nil {
		t.Fatalf("could not create the temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	images := Images{
		MachineAPIOperator: "test-mao",
	}
	imagesAsJSON, err := json.Marshal(images)
	if err != nil {
		t.Fatalf("failed to marshal images: %v", err)
	}
	imagesFilePath := filepath.Join(tempDir, "test-images.json")
	if err := ioutil.WriteFile(imagesFilePath, imagesAsJSON, 0666); err != nil {
		t.Fatalf("could not write the images file: %v", err)
	}

	for _, tc := range cases {
		t.Run(string(tc.platform), func(t *testing.T) {
			infra := &configv1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: configv1.InfrastructureStatus{
					Platform: tc.platform,
				},
			}

			configClient := fakeos.NewSimpleClientset()
			configSharedInformer := configinformersv1.NewSharedInformerFactoryWithOptions(configClient, 10*time.Minute)
			featureGateInformer := configSharedInformer.Config().V1().FeatureGates()

			optr := Operator{
				eventRecorder:     record.NewFakeRecorder(5),
				osClient:          fakeconfigclientset.NewSimpleClientset(infra),
				imagesFile:        imagesFilePath,
				featureGateLister: featureGateInformer.Lister(),
			}

			err = optr.sync("test-key")
			if !tc.expectedNoop {
				if !assert.Error(t, err, "unexpected sync success") {
					t.Fatal()
				}
			} else {
				if !assert.NoError(t, err, "unexpected sync failure") {
					t.Fatal()
				}
			}

			o, err := optr.osClient.ConfigV1().ClusterOperators().Get(clusterOperatorName, metav1.GetOptions{})
			if !assert.NoError(t, err, "failed to get clusteroperator") {
				t.Fatal()
			}
			expectedConditions := map[configv1.ClusterStatusConditionType]configv1.ConditionStatus{
				configv1.OperatorAvailable:   configv1.ConditionTrue,
				configv1.OperatorProgressing: configv1.ConditionFalse,
				configv1.OperatorDegraded:    configv1.ConditionTrue,
			}
			if tc.expectedNoop {
				expectedConditions[configv1.OperatorDegraded] = configv1.ConditionFalse
			}
			actualConditions := map[configv1.ClusterStatusConditionType]configv1.ConditionStatus{}
			for _, c := range o.Status.Conditions {
				actualConditions[c.Type] = c.Status
			}
			assert.Equal(t, expectedConditions, actualConditions, "unexpected clusteroperator conditions")
		})
	}
}

const (
	deploymentName   = "machine-api-controllers"
	targetNamespace  = "test-namespace"
	hcControllerName = "machine-healthcheck-controller"
)

func newFakeOperator(
	kubeClient kubernetes.Interface,
	osClient osclientset.Interface,
	stopCh <-chan struct{},
) *Operator {

	kubeNamespacedSharedInformer := informers.NewSharedInformerFactoryWithOptions(kubeClient, 2*time.Minute, informers.WithNamespace(targetNamespace))
	configSharedInformer := configinformersv1.NewSharedInformerFactoryWithOptions(osClient, 2*time.Minute)
	featureGateInformer := configSharedInformer.Config().V1().FeatureGates()
	deployInformer := kubeNamespacedSharedInformer.Apps().V1().Deployments()

	optr := &Operator{
		kubeClient:             kubeClient,
		osClient:               osClient,
		featureGateLister:      featureGateInformer.Lister(),
		deployLister:           deployInformer.Lister(),
		ownedManifestsDir:      "../../owned-manifests",
		imagesFile:             "fixtures/images.json",
		namespace:              targetNamespace,
		queue:                  workqueue.NewNamedRateLimitingQueue(workqueue.DefaultControllerRateLimiter(), "machineapioperator"),
		deployListerSynced:     deployInformer.Informer().HasSynced,
		featureGateCacheSynced: featureGateInformer.Informer().HasSynced,
	}

	optr.syncHandler = optr.sync
	deployInformer.Informer().AddEventHandler(optr.eventHandler())
	featureGateInformer.Informer().AddEventHandler(optr.eventHandler())

	configSharedInformer.Start(stopCh)
	kubeNamespacedSharedInformer.Start(stopCh)

	return optr
}

func deploymentHasContainer(d *appsv1.Deployment, containerName string) bool {
	for _, container := range d.Spec.Template.Spec.Containers {
		if container.Name == containerName {
			return true
		}
	}
	return false
}

func TestOperatorSyncClusterAPIControllerHealthCheckController(t *testing.T) {
	tests := []struct {
		featureGate                          *v1.FeatureGate
		expectedMachineHealthCheckController bool
	}{{
		featureGate: &v1.FeatureGate{
			ObjectMeta: metav1.ObjectMeta{
				Name: MachineAPIFeatureGateName,
			},
			Spec: v1.FeatureGateSpec{
				FeatureSet: configv1.Default,
			},
		},
		expectedMachineHealthCheckController: false,
	}, {
		featureGate:                          &v1.FeatureGate{},
		expectedMachineHealthCheckController: false,
	}, {
		featureGate: &v1.FeatureGate{
			ObjectMeta: metav1.ObjectMeta{
				Name: MachineAPIFeatureGateName,
			},
			Spec: v1.FeatureGateSpec{
				FeatureSet: configv1.TechPreviewNoUpgrade,
			},
		},
		expectedMachineHealthCheckController: true,
	}}

	for _, tc := range tests {
		infra := &configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Status: configv1.InfrastructureStatus{
				Platform: configv1.AWSPlatformType,
			},
		}

		kubeclientSet := fakekube.NewSimpleClientset()
		configClient := fakeos.NewSimpleClientset(tc.featureGate, infra)

		stopCh := make(<-chan struct{})
		optr := newFakeOperator(kubeclientSet, configClient, stopCh)
		go optr.Run(2, stopCh)

		if err := wait.PollImmediate(1*time.Second, 5*time.Second, func() (bool, error) {
			d, err := optr.deployLister.Deployments(targetNamespace).Get(deploymentName)
			if err != nil {
				t.Logf("Failed to get %q deployment: %v", deploymentName, err)
				return false, nil
			}
			if deploymentHasContainer(d, hcControllerName) != tc.expectedMachineHealthCheckController {
				t.Logf("Expected deploymentHasContainer for %q container to be %t", hcControllerName, tc.expectedMachineHealthCheckController)
				return false, nil
			}
			return true, nil
		}); err != nil {
			t.Errorf("Failed to verify %q deployment", deploymentName)
		}
	}
}
