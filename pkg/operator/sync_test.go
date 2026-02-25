package operator

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1beta1 "github.com/openshift/api/machine/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/diff"
)

func TestCheckDeploymentRolloutStatus(t *testing.T) {
	testCases := []struct {
		name                 string
		deployment           *appsv1.Deployment
		expectedError        error
		expectedRequeueAfter time.Duration
	}{
		{
			name: "Deployment is available for more than deploymentMinimumAvailabilityTime min",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-deploymentMinimumAvailabilityTime - 1*time.Second)},
						},
					},
				},
			},
			expectedError:        nil,
			expectedRequeueAfter: 0,
		},
		{
			name: "Deployment is available for less than deploymentMinimumAvailabilityTime min",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 0,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Second)},
						},
					},
				},
			},
			expectedError:        nil,
			expectedRequeueAfter: 170 * time.Second,
		},
		{
			name: "Deployment has unavailable replicas",
			deployment: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "apps/v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: targetNamespace,
				},
				Status: appsv1.DeploymentStatus{
					Replicas:            1,
					UpdatedReplicas:     1,
					ReadyReplicas:       1,
					AvailableReplicas:   1,
					UnavailableReplicas: 1,
					Conditions: []appsv1.DeploymentCondition{
						{
							Type:               appsv1.DeploymentAvailable,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: time.Now().Add(-10 * time.Second)},
						},
					},
				},
			},
			expectedError:        nil,
			expectedRequeueAfter: 5 * time.Second,
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			optr, err := newFakeOperator([]runtime.Object{tc.deployment}, nil, nil, imagesJSONFile, nil, stopCh)
			if err != nil {
				t.Fatal(err)
			}

			result, gotErr := optr.checkDeploymentRolloutStatus(tc.deployment)
			if tc.expectedError != nil && gotErr != nil {
				if tc.expectedError.Error() != gotErr.Error() {
					t.Errorf("Got error: %v, expected: %v", gotErr, tc.expectedError)
				}
			} else if tc.expectedError != gotErr {
				t.Errorf("Got error: %v, expected: %v", gotErr, tc.expectedError)
			}

			if tc.expectedRequeueAfter != result.RequeueAfter.Round(time.Second) {
				t.Errorf("Got requeueAfter: %v, expected: %v", result.RequeueAfter.Round(time.Second), tc.expectedRequeueAfter)
			}
		})
	}
}

func Test_ensureDependecyAnnotations(t *testing.T) {
	cases := []struct {
		name string

		input       *appsv1.Deployment
		inputHashes map[string]string

		expected *appsv1.Deployment
	}{{
		name:        "no previous hash tag",
		input:       &appsv1.Deployment{},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-1"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
	}, {
		name: "changed in on of the dependencies",
		input: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-2"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-2",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-2",
						},
					},
				},
			},
		},
	}, {
		name: "no change in dependencies",
		input: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
		inputHashes: map[string]string{"dep-1": "dep-1-state-1", "dep-2": "dep-2-state-1"},
		expected: &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"operator.openshift.io/dep-dep-1": "dep-1-state-1",
					"operator.openshift.io/dep-dep-2": "dep-2-state-1",
				},
			},
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							"operator.openshift.io/dep-dep-1": "dep-1-state-1",
							"operator.openshift.io/dep-dep-2": "dep-2-state-1",
						},
					},
				},
			},
		},
	}}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			input := test.input.DeepCopy()
			ensureDependecyAnnotations(test.inputHashes, input)
			if !equality.Semantic.DeepEqual(test.expected, input) {
				t.Fatalf("unexpected: %s", diff.Diff(test.expected, input))
			}
		})
	}
}

func TestCheckMinimumWorkerMachines(t *testing.T) {
	workerLabels := map[string]string{
		"role": "worker",
	}

	workerSelector := metav1.LabelSelector{
		MatchLabels: workerLabels,
	}

	infraLabels := map[string]string{
		"role": "infra",
	}

	infraSelector := metav1.LabelSelector{
		MatchLabels: infraLabels,
	}

	newMachineSet := func(name string, replicas int32, selector metav1.LabelSelector) *machinev1beta1.MachineSet {
		return &machinev1beta1.MachineSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: targetNamespace,
			},
			Spec: machinev1beta1.MachineSetSpec{
				Replicas: &replicas,
				Selector: selector,
			},
		}
	}

	newMachine := func(name string, labels map[string]string, phase string) *machinev1beta1.Machine {
		return &machinev1beta1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: targetNamespace,
				Labels:    labels,
			},
			Status: machinev1beta1.MachineStatus{
				Phase: &phase,
			},
		}
	}

	testCases := []struct {
		name          string
		machineSets   []runtime.Object
		machines      []runtime.Object
		expectedError error
	}{
		{
			name:          "with no MachineSets",
			expectedError: nil,
		},
		{
			name: "with a MachineSet with no Machines",
			machineSets: []runtime.Object{
				newMachineSet("no-machines", 3, workerSelector),
			},
			expectedError: errors.New("could not determine running Machines in MachineSet \"no-machines\": replicas not satisfied for MachineSet: expected 3 replicas, got 0 current replicas"),
		},
		{
			name: "with a single-machine MachineSet with one Machine",
			machineSets: []runtime.Object{
				newMachineSet("one-machines", 1, workerSelector),
			},
			machines: []runtime.Object{
				newMachine("running-0", workerLabels, "Running"),
			},
			expectedError: nil,
		},
		{
			name: "with a MachineSet with not enough Machines",
			machineSets: []runtime.Object{
				newMachineSet("no-machines", 3, workerSelector),
			},
			machines: []runtime.Object{
				newMachine("running-0", workerLabels, "Running"),
			},
			expectedError: errors.New("could not determine running Machines in MachineSet \"no-machines\": replicas not satisfied for MachineSet: expected 3 replicas, got 1 current replicas"),
		},
		{
			name: "with a MachineSet with 1 Machine Running, 2 Machines Provisioned",
			machineSets: []runtime.Object{
				newMachineSet("1-running-machine", 3, workerSelector),
			},
			machines: []runtime.Object{
				newMachine("running-0", workerLabels, "Running"),
				newMachine("provisioned-0", workerLabels, "Provisioned"),
				newMachine("provisioned-1", workerLabels, "Provisioned"),
				newMachine("infra-0", infraLabels, "Running"), // This Machine doesn't belong to a MachineSet so shouldn't affect the result.
			},
			expectedError: errors.New("minimum worker replica count (2) not yet met: current running replicas 1, waiting for [provisioned-0 provisioned-1]"),
		},
		{
			name: "with a MachineSet with 1 Machine Provisioned",
			machineSets: []runtime.Object{
				newMachineSet("1-provisioned-machine", 1, workerSelector),
			},
			machines: []runtime.Object{
				newMachine("provisioned-0", workerLabels, "Provisioned"),
			},
			expectedError: nil,
		},
		{
			name: "with a MachineSet with 2 Machines Running, 1 Machine Provisioned",
			machineSets: []runtime.Object{
				newMachineSet("1-running-machine", 3, workerSelector),
			},
			machines: []runtime.Object{
				newMachine("running-0", workerLabels, "Running"),
				newMachine("running-1", workerLabels, "Running"),
				newMachine("provisioned-0", workerLabels, "Provisioned"),
			},
			expectedError: nil,
		},
		{
			name: "with 2 MachineSets with 1 Machine Running, 2 Machines Provisioned each",
			machineSets: []runtime.Object{
				newMachineSet("1-running-worker", 3, workerSelector),
				newMachineSet("1-running-infra", 3, infraSelector),
			},
			machines: []runtime.Object{
				newMachine("worker-0", workerLabels, "Running"),
				newMachine("worker-1", workerLabels, "Provisioned"),
				newMachine("worker-2", workerLabels, "Provisioned"),
				newMachine("infra-0", infraLabels, "Running"),
				newMachine("infra-1", infraLabels, "Provisioned"),
				newMachine("infra-2", infraLabels, "Provisioned"),
			},
			expectedError: nil,
		},
		{
			// This would be a bit weird, it means the MachineSet controller was working, but now isn't?
			name: "with a MachineSet with no Machines, while other MachineSets are healthy",
			machineSets: []runtime.Object{
				newMachineSet("no-machines", 3, workerSelector),
				newMachineSet("infra", 3, infraSelector),
			},
			machines: []runtime.Object{
				newMachine("infra-0", infraLabels, "Running"),
				newMachine("infra-1", infraLabels, "Running"),
				newMachine("infra-2", infraLabels, "Running"),
			},
			expectedError: errors.New("could not determine running Machines in MachineSet \"no-machines\": replicas not satisfied for MachineSet: expected 3 replicas, got 0 current replicas"),
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

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			stopCh := make(chan struct{})
			defer close(stopCh)

			var machineObjects []runtime.Object
			machineObjects = append(machineObjects, tc.machineSets...)
			machineObjects = append(machineObjects, tc.machines...)

			optr, err := newFakeOperator(nil, nil, machineObjects, imagesJSONFile, nil, stopCh)
			if err != nil {
				t.Fatal(err)
			}

			err = optr.checkMinimumWorkerMachines()
			if tc.expectedError != nil {
				g.Expect(err).To(MatchError(tc.expectedError.Error()))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

func TestSyncWebhookConfiguration(t *testing.T) {

	testCases := []struct {
		name                         string
		platformType                 configv1.PlatformType
		expectedNrMutatingWebhooks   int
		expectedNrValidatingWebhooks int
	}{
		{
			name: "webhooks on non baremetal",
			// using AWS as random non baremetal platform
			platformType:                 configv1.AWSPlatformType,
			expectedNrMutatingWebhooks:   1,
			expectedNrValidatingWebhooks: 1,
		},
		{
			name:                         "webhooks on baremetal",
			platformType:                 configv1.BareMetalPlatformType,
			expectedNrMutatingWebhooks:   2,
			expectedNrValidatingWebhooks: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			stopCh := make(chan struct{})
			defer close(stopCh)
			optr, err := newFakeOperator(nil, nil, nil, "", nil, stopCh)
			if err != nil {
				t.Fatal(err)
			}

			nrMutatingWebhooks := 0
			nrValidatingWebhooks := 0
			_ = optr.syncWebhookConfiguration(&OperatorConfig{PlatformType: tc.platformType})
			for _, gen := range optr.generations {
				switch gen.Resource {
				case "mutatingwebhookconfigurations":
					nrMutatingWebhooks++
				case "validatingwebhookconfigurations":
					nrValidatingWebhooks++
				}
			}
			g.Expect(nrMutatingWebhooks).To(BeNumerically("==", tc.expectedNrMutatingWebhooks),
				"wrong nr of mutating webhooks")
			g.Expect(nrValidatingWebhooks).To(BeNumerically("==", tc.expectedNrValidatingWebhooks),
				"wrong nr of validating webhooks")
		})
	}
}

func TestNewKubeProxyContainers(t *testing.T) {
	testCases := []struct {
		name                       string
		image                      string
		withMHCProxy               bool
		tlsProfile                 configv1.TLSProfileSpec
		expectedCipherSuitesInArgs bool
		expectedPorts              map[string]int32
	}{
		{
			name:         "TLS 1.2 Intermediate profile with cipher suites",
			image:        "test-image:latest",
			withMHCProxy: true,
			tlsProfile: configv1.TLSProfileSpec{
				Ciphers: []string{
					"TLS_AES_128_GCM_SHA256",
					"TLS_AES_256_GCM_SHA384",
					"TLS_CHACHA20_POLY1305_SHA256",
					"ECDHE-ECDSA-AES128-GCM-SHA256",
					"ECDHE-RSA-AES128-GCM-SHA256",
				},
				MinTLSVersion: configv1.VersionTLS12,
			},
			expectedCipherSuitesInArgs: true,
			expectedPorts: map[string]int32{
				"kube-rbac-proxy-machineset-mtrc": machineSetExposeMetricsPort,
				"kube-rbac-proxy-machine-mtrc":    machineExposeMetricsPort,
				"kube-rbac-proxy-mhc-mtrc":        machineHealthCheckExposeMetricsPort,
			},
		},
		{
			name:         "TLS 1.3 Modern profile without cipher suites",
			image:        "test-image:latest",
			withMHCProxy: false,
			tlsProfile: configv1.TLSProfileSpec{
				Ciphers: []string{
					"TLS_AES_128_GCM_SHA256",
					"TLS_AES_256_GCM_SHA384",
					"TLS_CHACHA20_POLY1305_SHA256",
				},
				MinTLSVersion: configv1.VersionTLS13,
			},
			expectedCipherSuitesInArgs: false,
			expectedPorts: map[string]int32{
				"kube-rbac-proxy-machineset-mtrc": machineSetExposeMetricsPort,
				"kube-rbac-proxy-machine-mtrc":    machineExposeMetricsPort,
			},
		},
		{
			name:         "Empty cipher list",
			image:        "test-image:latest",
			withMHCProxy: false,
			tlsProfile: configv1.TLSProfileSpec{
				Ciphers:       []string{},
				MinTLSVersion: configv1.VersionTLS13,
			},
			expectedCipherSuitesInArgs: false,
			expectedPorts: map[string]int32{
				"kube-rbac-proxy-machineset-mtrc": machineSetExposeMetricsPort,
				"kube-rbac-proxy-machine-mtrc":    machineExposeMetricsPort,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			containers := newKubeProxyContainers(tc.image, tc.withMHCProxy, getTLSArgs(tc.tlsProfile))

			// Verify we get the expected number of containers
			g.Expect(containers).To(HaveLen(len(tc.expectedPorts)))

			// Verify each container has the correct TLS args and specific ports
			for _, container := range containers {
				// Verify basic container properties
				g.Expect(container.Image).To(Equal(tc.image))

				// Verify ports
				g.Expect(container.Ports).To(HaveLen(1))
				expectedPort, ok := tc.expectedPorts[container.Name]
				g.Expect(ok).To(BeTrue(), "Unexpected container name: %s", container.Name)
				g.Expect(container.Ports[0].ContainerPort).To(Equal(expectedPort))

				// Verify resource requests
				g.Expect(container.Resources.Requests).To(HaveKey(corev1.ResourceMemory))
				g.Expect(container.Resources.Requests).To(HaveKey(corev1.ResourceCPU))

				// Verify volume mounts
				g.Expect(container.VolumeMounts).To(HaveLen(2))

				// Verify TLS args
				hasCipherSuitesArg := false
				hasTLSMinVersionArg := false
				for _, arg := range container.Args {
					if strings.HasPrefix(arg, "--tls-cipher-suites=") {
						hasCipherSuitesArg = true
					}
					if strings.HasPrefix(arg, "--tls-min-version=") {
						hasTLSMinVersionArg = true
						g.Expect(arg).To(HavePrefix("--tls-min-version=" + string(tc.tlsProfile.MinTLSVersion)))
					}
				}

				g.Expect(hasCipherSuitesArg).To(Equal(tc.expectedCipherSuitesInArgs),
					"cipher suites arg presence mismatch for container %s", container.Name)
				g.Expect(hasTLSMinVersionArg).To(BeTrue(), "TLS min version arg should be present for container %s", container.Name)
			}
		})
	}
}

func TestNewContainersTLSArgs(t *testing.T) {
	testCases := []struct {
		name       string
		config     *OperatorConfig
		tlsProfile configv1.TLSProfileSpec
	}{
		{
			name: "TLS 1.2 with cipher suites",
			config: &OperatorConfig{
				TargetNamespace: targetNamespace,
				PlatformType:    configv1.AWSPlatformType,
				Controllers: Controllers{
					Provider:           "provider-image:latest",
					MachineSet:         "machineset-image:latest",
					NodeLink:           "nodelink-image:latest",
					MachineHealthCheck: "mhc-image:latest",
				},
			},
			tlsProfile: configv1.TLSProfileSpec{
				Ciphers: []string{
					"ECDHE-ECDSA-AES128-GCM-SHA256",
					"ECDHE-RSA-AES128-GCM-SHA256",
				},
				MinTLSVersion: configv1.VersionTLS12,
			},
		},
		{
			name: "TLS 1.3 without cipher suites",
			config: &OperatorConfig{
				TargetNamespace: targetNamespace,
				PlatformType:    configv1.GCPPlatformType,
				Controllers: Controllers{
					Provider:           "provider-image:latest",
					MachineSet:         "machineset-image:latest",
					NodeLink:           "nodelink-image:latest",
					MachineHealthCheck: "",
				},
			},
			tlsProfile: configv1.TLSProfileSpec{
				Ciphers:       []string{},
				MinTLSVersion: configv1.VersionTLS13,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			tlsArgs := getTLSArgs(tc.tlsProfile)
			containers := newContainers(tc.config, map[string]bool{}, tlsArgs)

			containerArgs := map[string][]string{}
			for _, c := range containers {
				containerArgs[c.Name] = c.Args
			}

			g.Expect(containerArgs).To(HaveKey("machineset-controller"))
			g.Expect(containerArgs).To(HaveKey("machine-controller"))
			g.Expect(containerArgs).To(HaveKey("nodelink-controller"))

			// Only machineset-controller should receive TLS args.
			machineSetJoined := strings.Join(containerArgs["machineset-controller"], " ")
			g.Expect(machineSetJoined).To(ContainSubstring("--tls-min-version="+string(tc.tlsProfile.MinTLSVersion)),
				"machineset-controller should have --tls-min-version")
			if len(tc.tlsProfile.Ciphers) > 0 {
				g.Expect(machineSetJoined).To(ContainSubstring("--tls-cipher-suites="),
					"machineset-controller should have --tls-cipher-suites when ciphers are specified")
			}

			for _, name := range []string{"machine-controller", "nodelink-controller"} {
				joined := strings.Join(containerArgs[name], " ")
				g.Expect(joined).ToNot(ContainSubstring("--tls-min-version="),
					"%s should not have TLS args", name)
				g.Expect(joined).ToNot(ContainSubstring("--tls-cipher-suites="),
					"%s should not have TLS args", name)
			}

			if tc.config.Controllers.MachineHealthCheck != "" {
				g.Expect(containerArgs).To(HaveKey("machine-healthcheck-controller"))
				mhcJoined := strings.Join(containerArgs["machine-healthcheck-controller"], " ")
				g.Expect(mhcJoined).ToNot(ContainSubstring("--tls-min-version="),
					"machine-healthcheck-controller should not have TLS args")
			}
		})
	}
}
