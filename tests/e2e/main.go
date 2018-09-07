package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	operatorclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	operatorclienttypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	"github.com/kubernetes-incubator/apiserver-builder/pkg/controller"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apiv1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

const (
	pollInterval                            = 1 * time.Second
	timeoutPoolInterval                     = 5 * time.Second
	timeoutPoolClusterAPIDeploymentInterval = 5 * time.Minute
	timeoutPoolMachineSetRunningInterval    = 5 * time.Minute

	defaultLogLevel = "info"

	maoAWSConfigData = `apiVersion: v1
aws:
  availabilityZone: ""
  clusterID: e1b5d05b-ab2e-486b-f50e-4d23cf4a59b2
  clusterName: testCluster
  containerLinuxVersion: 1800.7.0
  image: ""
  region: eu-west-1
  releaseChannel: stable
  replicas: 2
kind: machineAPIOperatorConfig
libvirt: null
provider: aws
`
)

func usage() {
	fmt.Printf("Usage: %s\n\n", os.Args[0])
}

// TestConfig stores clients for managing various resources
type TestConfig struct {
	CAPIClient          *clientset.Clientset
	APIExtensionsClient *apiextensionsclientset.Clientset
	KubeClient          *kubernetes.Clientset
	OperatorClient      operatorclient.Interface
}

// NewTestConfig creates new test config with clients
func NewTestConfig(kubeconfig string) *TestConfig {
	config, err := controller.GetConfig(kubeconfig)
	if err != nil {
		glog.Fatalf("Could not create Config for talking to the apiserver: %v", err)
	}

	capiclient, err := clientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	apiextensionsclient, err := apiextensionsclientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create kubernetes client to talk to the apiserver: %v", err)
	}

	opclient := operatorclient.NewClient(kubeconfig)

	return &TestConfig{
		CAPIClient:          capiclient,
		APIExtensionsClient: apiextensionsclient,
		KubeClient:          kubeClient,
		OperatorClient:      opclient,
	}
}

func createNamespace(testConfig *TestConfig, namespace string) error {
	nsObj := &apiv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	log.Infof("Creating %q namespace...", nsObj.Name)
	if _, err := testConfig.KubeClient.CoreV1().Namespaces().Get(nsObj.Name, metav1.GetOptions{}); err != nil {
		if _, err := testConfig.KubeClient.CoreV1().Namespaces().Create(nsObj); err != nil {
			return fmt.Errorf("unable to create namespace: %v", err)
		}
	}

	return nil
}

func createCRD(testConfig *TestConfig, crd *apiextensionsv1beta1.CustomResourceDefinition) error {
	log.Infof("Creating %q CRD...", crd.Name)
	crcClient := testConfig.APIExtensionsClient.ApiextensionsV1beta1().CustomResourceDefinitions()
	if _, err := crcClient.Get(crd.Name, metav1.GetOptions{}); err != nil {
		if _, err := crcClient.Create(crd); err != nil {
			return fmt.Errorf("unable to create CRD: %v", err)
		}
	}

	return wait.Poll(pollInterval, timeoutPoolInterval, func() (bool, error) {
		if _, err := crcClient.Get(crd.Name, metav1.GetOptions{}); err == nil {
			return true, nil
		}

		log.Infof("Waiting for %q crd to be created", crd.Name)
		return false, nil
	})
}

func createAppVersionCRD(testConfig *TestConfig) error {
	appVersion := &operatorclienttypes.AppVersion{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AppVersion",
			APIVersion: "tco.coreos.com/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "machine-api",
			Namespace: "tectonic-system",
			Labels: map[string]string{
				"managed-by-channel-operator": "true",
			},
		},
		Spec: operatorclienttypes.AppVersionSpec{
			DesiredVersion: "",
			Paused:         false,
		},
		Status: operatorclienttypes.AppVersionStatus{
			CurrentVersion: "1",
			Paused:         false,
		},
		UpgradeReq:  1,
		UpgradeComp: 0,
	}

	log.Infof("Creating %q AppVersion...", strings.Join([]string{appVersion.Namespace, appVersion.Name}, "/"))
	if _, err := testConfig.OperatorClient.GetAppVersion("tectonic-system", "machine-api"); err != nil {
		if _, err := testConfig.OperatorClient.CreateAppVersion(appVersion); err != nil {
			return fmt.Errorf("unable to create AppVersion: %v", err)
		}
	}

	return nil
}

func createConfigMap(testConfig *TestConfig, configMap *apiv1.ConfigMap) error {
	log.Infof("Creating %q ConfigMap...", strings.Join([]string{configMap.Namespace, configMap.Name}, "/"))
	if _, err := testConfig.KubeClient.CoreV1().ConfigMaps(configMap.Namespace).Get(configMap.Name, metav1.GetOptions{}); err != nil {
		if _, err := testConfig.KubeClient.CoreV1().ConfigMaps(configMap.Namespace).Create(configMap); err != nil {
			return fmt.Errorf("unable to create ConfigMap: %v", err)
		}
	}

	return nil
}

func createDeployment(testConfig *TestConfig, deployment *appsv1beta2.Deployment) error {
	log.Info("Creating machine-api-operator...")
	deploymentsClient := testConfig.KubeClient.AppsV1beta2().Deployments(deployment.Namespace)
	if _, err := deploymentsClient.Get(deployment.Name, metav1.GetOptions{}); err != nil {
		if _, err := deploymentsClient.Create(deployment); err != nil {
			return fmt.Errorf("unable to create Deployment: %v", err)
		}
	}

	return nil
}

var rootCmd = &cobra.Command{
	Use:   "e2e",
	Short: "Test deployment of cluster-api stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		kubeconfig := cmd.Flag("kubeconfig").Value.String()
		logLevel := cmd.Flag("log-level").Value.String()
		maoImage := cmd.Flag("mao-image").Value.String()

		if kubeconfig == "" {
			return fmt.Errorf("--kubeconfig option is required")
		}

		log.SetOutput(os.Stdout)
		if lvl, err := log.ParseLevel(logLevel); err != nil {
			log.Panic(err)
		} else {
			log.SetLevel(lvl)
		}

		testConfig := NewTestConfig(kubeconfig)

		if err := createNamespace(testConfig, "tectonic-system"); err != nil {
			return err
		}

		crd := &apiextensionsv1beta1.CustomResourceDefinition{
			ObjectMeta: metav1.ObjectMeta{
				Name: "appversions.tco.coreos.com",
			},
			Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
				Group:   "tco.coreos.com",
				Version: "v1",
				Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
					Plural: "appversions",
					Kind:   "AppVersion",
				},
			},
		}

		if err := createCRD(testConfig, crd); err != nil {
			return err
		}

		if err := createAppVersionCRD(testConfig); err != nil {
			return err
		}

		mapConfigMap := &apiv1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mao-config-v1",
				Namespace: "kube-system",
			},
			Data: map[string]string{
				"mao-config": maoAWSConfigData,
			},
		}

		if err := createConfigMap(testConfig, mapConfigMap); err != nil {
			return err
		}

		var replicas int32 = 1
		runAsNonRoot := true
		var runAsUser int64 = 65534

		maoManifest := &appsv1beta2.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "machine-api-operator",
				Namespace: "kube-system",
				Labels: map[string]string{
					"k8s-app":                     "machine-api-operator",
					"managed-by-channel-operator": "true",
				},
			},
			Spec: appsv1beta2.DeploymentSpec{
				Replicas: &replicas,
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"k8s-app": "machine-api-operator",
					},
				},
				Template: apiv1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"k8s-app":                     "machine-api-operator",
							"managed-by-channel-operator": "true",
						},
					},
					Spec: apiv1.PodSpec{
						Containers: []apiv1.Container{
							{
								Name:  "machine-api-operator",
								Image: maoImage,
								Command: []string{
									"/machine-api-operator",
								},
								Args: []string{
									"-v=4",
								},
								Resources: apiv1.ResourceRequirements{
									Limits: apiv1.ResourceList{
										"cpu":    resource.MustParse("20m"),
										"memory": resource.MustParse("50Mi"),
									},
									Requests: apiv1.ResourceList{
										"cpu":    resource.MustParse("20m"),
										"memory": resource.MustParse("50Mi"),
									},
								},
								VolumeMounts: []apiv1.VolumeMount{
									{
										Name:      "mao-config",
										MountPath: "/etc/mao-config",
									},
								},
							},
						},
						ImagePullSecrets: []apiv1.LocalObjectReference{
							{
								Name: "coreos-pull-secret",
							},
						},
						NodeSelector: map[string]string{
							"node-role.kubernetes.io/master": "",
						},
						RestartPolicy: "Always",
						SecurityContext: &apiv1.PodSecurityContext{
							RunAsNonRoot: &runAsNonRoot,
							RunAsUser:    &runAsUser,
						},
						Tolerations: []apiv1.Toleration{
							{
								Key:      "node-role.kubernetes.io/master",
								Operator: "Exists",
								Effect:   "NoSchedule",
							},
						},
						Volumes: []apiv1.Volume{
							{
								Name: "mao-config",
								VolumeSource: apiv1.VolumeSource{
									ConfigMap: &apiv1.ConfigMapVolumeSource{
										LocalObjectReference: apiv1.LocalObjectReference{
											Name: "mao-config-v1",
										},
										Items: []apiv1.KeyToPath{
											{
												Key:  "mao-config",
												Path: "config",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		}

		if err := createDeployment(testConfig, maoManifest); err != nil {
			return err
		}

		err := wait.Poll(pollInterval, timeoutPoolClusterAPIDeploymentInterval, func() (bool, error) {
			if clusterAPIDeployment, err := testConfig.KubeClient.AppsV1beta2().Deployments("default").Get("clusterapi-apiserver", metav1.GetOptions{}); err == nil {
				// Check all the pods are running
				log.Infof("Waiting for all cluster-api deployment pods to be ready, have %v, expecting 1", clusterAPIDeployment.Status.ReadyReplicas)
				if clusterAPIDeployment.Status.ReadyReplicas < 1 {
					return false, nil
				}
				return true, nil
			}

			log.Info("Waiting for cluster-api deployment to be created")
			return false, nil
		})

		if err != nil {
			return err
		}

		// Verify cluster, machineSet and machines have been deployed
		var cluster, machineSet, workers bool
		err = wait.Poll(pollInterval, timeoutPoolMachineSetRunningInterval, func() (bool, error) {
			if _, err := testConfig.CAPIClient.ClusterV1alpha1().Clusters("default").Get("testCluster", metav1.GetOptions{}); err == nil {
				cluster = true
				log.Info("Cluster object has been created")
			}

			if _, err := testConfig.CAPIClient.ClusterV1alpha1().MachineSets("default").Get("worker", metav1.GetOptions{}); err == nil {
				machineSet = true
				log.Info("MachineSet object has been created")
			}

			if workersList, err := testConfig.CAPIClient.ClusterV1alpha1().Machines("default").List(metav1.ListOptions{}); err == nil {
				if len(workersList.Items) == 2 {
					workers = true
					log.Info("Machine objects has been created")
				}
			}

			if cluster && machineSet && workers {
				return true, nil
			}
			log.Info("Waiting for cluster, machineSet and machines to be created")
			return false, nil
		})

		if err != nil {
			return err
		}

		log.Info("The cluster-api stack is ready")
		log.Info("The cluster, the machineSet and the machines have been deployed")
		// TODO(alberto): verify machines are running against aws API. We'll need an ignition stub

		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringP("kubeconfig", "m", "", "Kubernetes config")
	rootCmd.PersistentFlags().StringP("log-level", "l", defaultLogLevel, "Log level (debug,info,warn,error,fatal)")
	rootCmd.PersistentFlags().StringP("mao-image", "", "machine-api-operator:mvp", "machine-api-operator docker image to run")
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}
