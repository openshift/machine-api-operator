package operator

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"

	v1 "github.com/openshift/api/config/v1"
	fakeos "github.com/openshift/client-go/config/clientset/versioned/fake"
	configinformersv1 "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/stretchr/testify/assert"
)

const (
	deploymentName   = "machine-api-controllers"
	targetNamespace  = "test-namespace"
	hcControllerName = "machine-healthcheck-controller"
)

func newFeatureGate(featureSet v1.FeatureSet) *v1.FeatureGate {
	return &v1.FeatureGate{
		ObjectMeta: metav1.ObjectMeta{
			Name: MachineAPIFeatureGateName,
		},
		Spec: v1.FeatureGateSpec{
			FeatureSet: featureSet,
		},
	}
}

func newOperatorConfig() *OperatorConfig {
	return &OperatorConfig{
		targetNamespace,
		Controllers{
			"docker.io/openshift/origin-aws-machine-controllers:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
			"docker.io/openshift/origin-machine-api-operator:v4.0.0",
		},
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
	deployInformer.Informer().AddEventHandler(optr.eventHandler())
	featureGateInformer.Informer().AddEventHandler(optr.eventHandler())

	return optr
}

// TestOperatorSync_NoOp tests syncing to ensure that the mao reports available
// for platforms that are no-ops.
func TestOperatorSync_NoOp(t *testing.T) {
	cases := []struct {
		platform     v1.PlatformType
		expectedNoop bool
	}{
		{
			platform:     v1.AWSPlatformType,
			expectedNoop: false,
		},
		{
			platform:     v1.LibvirtPlatformType,
			expectedNoop: false,
		},
		{
			platform:     v1.OpenStackPlatformType,
			expectedNoop: false,
		},
		{
			platform:     v1.AzurePlatformType,
			expectedNoop: false,
		},
		{
			platform:     v1.BareMetalPlatformType,
			expectedNoop: false,
		},
		{
			platform:     kubemarkPlatform,
			expectedNoop: false,
		},
		{
			platform:     v1.VSpherePlatformType,
			expectedNoop: true,
		},
		{
			platform:     v1.NonePlatformType,
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
			infra := &v1.Infrastructure{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster",
				},
				Status: v1.InfrastructureStatus{
					Platform: tc.platform,
				},
			}

			stopCh := make(<-chan struct{})
			optr := newFakeOperator(nil, []runtime.Object{infra}, stopCh)
			optr.imagesFile = imagesFilePath

			err = optr.sync("test-key")
			if !assert.NoError(t, err, "unexpected sync failure") {
				t.Fatal()
			}

			err = wait.PollImmediate(1*time.Second, 5*time.Second, func() (bool, error) {
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
			expectedConditions := map[v1.ClusterStatusConditionType]v1.ConditionStatus{
				v1.OperatorAvailable:   v1.ConditionTrue,
				v1.OperatorProgressing: v1.ConditionFalse,
				v1.OperatorDegraded:    v1.ConditionFalse,
			}
			for _, c := range o.Status.Conditions {
				assert.Equal(t, expectedConditions[c.Type], c.Status, fmt.Sprintf("unexpected clusteroperator condition %s status", c.Type))
			}
		})
	}
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
		featureGate:                          newFeatureGate(v1.Default),
		expectedMachineHealthCheckController: false,
	}, {
		featureGate:                          &v1.FeatureGate{},
		expectedMachineHealthCheckController: false,
	}, {
		featureGate:                          newFeatureGate(v1.TechPreviewNoUpgrade),
		expectedMachineHealthCheckController: true,
	}}

	for _, tc := range tests {
		infra := &v1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: "cluster",
			},
			Status: v1.InfrastructureStatus{
				Platform: v1.AWSPlatformType,
			},
		}

		stopCh := make(<-chan struct{})
		optr := newFakeOperator(nil, []runtime.Object{tc.featureGate, infra}, stopCh)
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
