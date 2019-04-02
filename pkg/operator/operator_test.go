package operator

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	fakeconfigclientset "github.com/openshift/client-go/config/clientset/versioned/fake"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"
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
			platform:     bareMetalPlatform,
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

			optr := Operator{
				eventRecorder: record.NewFakeRecorder(5),
				osClient:      fakeconfigclientset.NewSimpleClientset(infra),
				imagesFile:    imagesFilePath,
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
				configv1.OperatorFailing:     configv1.ConditionTrue,
			}
			if tc.expectedNoop {
				expectedConditions[configv1.OperatorFailing] = configv1.ConditionFalse
			}
			actualConditions := map[configv1.ClusterStatusConditionType]configv1.ConditionStatus{}
			for _, c := range o.Status.Conditions {
				actualConditions[c.Type] = c.Status
			}
			assert.Equal(t, expectedConditions, actualConditions, "unexpected clusteroperator conditions")
		})
	}
}
