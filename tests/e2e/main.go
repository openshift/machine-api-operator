package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"
	"github.com/openshift/machine-api-operator/pkg/render"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/controller"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apiv1 "k8s.io/api/core/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsscheme "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

const (
	pollInterval                            = 1 * time.Second
	timeoutPoolInterval                     = 5 * time.Second
	timeoutPoolAWSInterval                  = 25 * time.Second
	timeoutPoolClusterAPIDeploymentInterval = 10 * time.Minute
	timeoutPoolMachineSetRunningInterval    = 10 * time.Minute

	defaultLogLevel          = "info"
	targetNamespace          = "openshift-machine-api-operator"
	awsCredentialsSecretName = "aws-credentials-secret"
	region                   = "us-east-1"
	machineSetReplicas       = 2
)

func usage() {
	fmt.Printf("Usage: %s\n\n", os.Args[0])
}

// TestConfig stores clients for managing various resources
type TestConfig struct {
	CAPIClient          *clientset.Clientset
	APIExtensionsClient *apiextensionsclientset.Clientset
	KubeClient          *kubernetes.Clientset
	AWSClient           *AWSClient
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

	return &TestConfig{
		CAPIClient:          capiclient,
		APIExtensionsClient: apiextensionsclient,
		KubeClient:          kubeClient,
	}
}

// Kube library
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

func createConfigMap(testConfig *TestConfig, configMap *apiv1.ConfigMap) error {
	log.Infof("Creating %q ConfigMap...", strings.Join([]string{configMap.Namespace, configMap.Name}, "/"))
	if _, err := testConfig.KubeClient.CoreV1().ConfigMaps(configMap.Namespace).Get(configMap.Name, metav1.GetOptions{}); err != nil {
		if _, err := testConfig.KubeClient.CoreV1().ConfigMaps(configMap.Namespace).Create(configMap); err != nil {
			return fmt.Errorf("unable to create ConfigMap: %v", err)
		}
	}

	return nil
}

func createSecret(testConfig *TestConfig, secret *apiv1.Secret) error {
	log.Infof("Creating %q secret...", strings.Join([]string{secret.Namespace, secret.Name}, "/"))
	if _, err := testConfig.KubeClient.CoreV1().Secrets(secret.Namespace).Get(secret.Name, metav1.GetOptions{}); err != nil {
		if _, err := testConfig.KubeClient.CoreV1().Secrets(secret.Namespace).Create(secret); err != nil {
			return fmt.Errorf("unable to create secret: %v", err)
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

// binary runner
func cmdRun(assetsDir string, binaryPath string, args ...string) error {
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = assetsDir
	return cmd.Run()
}

var rootCmd = &cobra.Command{
	Use:   "e2e",
	Short: "Test deployment of cluster-api stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		kubeconfig := cmd.Flag("kubeconfig").Value.String()
		logLevel := cmd.Flag("log-level").Value.String()
		maoImage := cmd.Flag("mao-image").Value.String()
		assetsPath := cmd.Flag("assets-path").Value.String()
		clusterID := cmd.Flag("cluster-id").Value.String()

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

		// create terraform stub enviroment
		if err := cmdRun(assetsPath, "terraform", "init"); err != nil {
			glog.Fatalf("unable to run terraform init: %v", err)
		}
		tfVars := fmt.Sprintf("-var=enviroment_id=%s", clusterID)
		if err := cmdRun(assetsPath, "terraform", "apply", tfVars, "-auto-approve"); err != nil {
			glog.Fatalf("unable to run terraform apply -auto-approve: %v", err)
		}
		defer tearDown(testConfig, assetsPath)

		// create mao deployment and assumptions, i.e secrets, namespaces, appVersion, mao config
		// generate aws creds kube secret
		if err := cmdRun(assetsPath, "./generate.sh"); err != nil {
			glog.Fatalf("unable to run generate.sh: %v", err)
		}

		// generate assumed namespaces
		if err := createNamespace(testConfig, targetNamespace); err != nil {
			return err
		}

		// create status CRD
		apiextensionsscheme.AddToScheme(scheme.Scheme)
		decode := scheme.Codecs.UniversalDeserializer().Decode
		if statusCRD, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/status-crd.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			CRDObj, _, err := decode([]byte(statusCRD), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			CRD := CRDObj.(*apiextensionsv1beta1.CustomResourceDefinition)

			if err := createCRD(testConfig, CRD); err != nil {
				return err
			}
		}

		// genereate mao config
		maoConfigTemplateData, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/mao-config.yaml"))
		if err != nil {
			glog.Fatalf("Error reading %#v", err)
		}
		configValues := &render.OperatorConfig{
			AWS: &render.AWSConfig{
				ClusterID:   clusterID,
				ClusterName: clusterID,
				Region:      region,
			},
		}
		maoConfigPopulatedData, err := render.Manifests(configValues, maoConfigTemplateData)
		if err != nil {
			glog.Fatalf("Unable to render manifests %q: %v", maoConfigTemplateData, err)
		} else {
			mapConfigMap := &apiv1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster-config-v1",
					Namespace: "kube-system",
				},
				Data: map[string]string{
					"mao-config": string(maoConfigPopulatedData),
				},
			}
			if err := createConfigMap(testConfig, mapConfigMap); err != nil {
				return err
			}
		}

		// secrets
		// create cluster api server secrets
		if secretData, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/clusterapi-apiserver-certs.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			secretObj, _, err := decode([]byte(secretData), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			secret := secretObj.(*apiv1.Secret)

			if err := createSecret(testConfig, secret); err != nil {
				return err
			}
		}

		// create aws creds secret
		if secretData, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/aws-credentials.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			secretObj, _, err := decode([]byte(secretData), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			secret := secretObj.(*apiv1.Secret)
			if err := createSecret(testConfig, secret); err != nil {
				return err
			}
		}

		// create ign config secret
		if secretData, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/ign-config.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			secretObj, _, err := decode([]byte(secretData), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			secret := secretObj.(*apiv1.Secret)
			if err := createSecret(testConfig, secret); err != nil {
				return err
			}
		}

		awsClient, err := NewClient(testConfig.KubeClient, awsCredentialsSecretName, targetNamespace, region)
		if err != nil {
			glog.Fatalf("Could not create aws client: %v", err)
		}
		testConfig.AWSClient = awsClient

		// create operator deployment
		type deploymentValues struct {
			Image string
		}
		dv := deploymentValues{
			Image: maoImage,
		}
		if deploymentData, err := RenderTemplateFromFile(dv, filepath.Join(assetsPath, "manifests/operator-deployment.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			deploymentObj, _, err := decode([]byte(deploymentData), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			deployment := deploymentObj.(*appsv1beta2.Deployment)
			if err := createDeployment(testConfig, deployment); err != nil {
				return err
			}
		}

		// TESTS
		// verify the cluster-api is running
		err = wait.Poll(pollInterval, timeoutPoolClusterAPIDeploymentInterval, func() (bool, error) {
			if clusterAPIDeployment, err := testConfig.KubeClient.AppsV1beta2().Deployments(targetNamespace).Get("clusterapi-apiserver", metav1.GetOptions{}); err == nil {
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
			if _, err := testConfig.CAPIClient.ClusterV1alpha1().Clusters(targetNamespace).Get(clusterID, metav1.GetOptions{}); err == nil {
				cluster = true
				log.Info("Cluster object has been created")
			}

			if _, err := testConfig.CAPIClient.ClusterV1alpha1().MachineSets(targetNamespace).Get("worker", metav1.GetOptions{}); err == nil {
				machineSet = true
				log.Info("MachineSet object has been created")
			}

			if workersList, err := testConfig.CAPIClient.ClusterV1alpha1().Machines(targetNamespace).List(metav1.ListOptions{}); err == nil {
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

		// verify aws instances are running
		err = wait.Poll(pollInterval, timeoutPoolAWSInterval, func() (bool, error) {
			log.Info("Waiting for aws instances to come up")
			runningInstances, err := testConfig.AWSClient.GetRunningInstances(clusterID)
			if err != nil {
				return false, fmt.Errorf("unable to get running instances from aws: %v", err)
			}
			if len(runningInstances) == machineSetReplicas {
				log.Info("Two instances are running on aws")
				return true, nil
			}
			return false, nil
		})

		if err != nil {
			return err
		}

		log.Info("All verified successfully. Tearing down...")
		return nil
	},
}

func tearDown(testConfig *TestConfig, assetsPath string) error {
	// delete machine set
	// not erroring here so we try to terraform destroy
	if err := testConfig.CAPIClient.ClusterV1alpha1().MachineSets(targetNamespace).Delete("worker", &metav1.DeleteOptions{}); err != nil {
		log.Warningf("unable to delete machineSet, %v", err)
	}

	// delete terraform stub environment
	log.Info("Running terraform destroy")
	if err := cmdRun(assetsPath, "terraform", "destroy", "-force"); err != nil {
		return fmt.Errorf("unable run terraform destroy: %v", err)
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringP("kubeconfig", "m", "", "Kubernetes config")
	rootCmd.PersistentFlags().StringP("log-level", "l", defaultLogLevel, "Log level (debug,info,warn,error,fatal)")
	rootCmd.PersistentFlags().StringP("mao-image", "", "machine-api-operator:mvp", "machine-api-operator docker image to run")
	rootCmd.PersistentFlags().StringP("assets-path", "", "./tests/e2e", "path to terraform and kube assets")
	rootCmd.PersistentFlags().StringP("cluster-id", "", "testCluster", "A unique id for the environment to build")
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}
