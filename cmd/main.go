package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/golang/glog"

	apiv1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	opclient "github.com/coreos-inc/tectonic-operators/operator-client/pkg/client"
	optypes "github.com/coreos-inc/tectonic-operators/operator-client/pkg/types"
	xotypes "github.com/coreos-inc/tectonic-operators/x-operator/pkg/types"
	"github.com/coreos-inc/tectonic-operators/x-operator/pkg/xoperator"
	"github.com/openshift/machine-api-operator/pkg/render"
	machineAPI "github.com/openshift/machine-api-operator/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	"sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	clusterApiScheme "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset/scheme"
)

var (
	kubeconfig  string
	manifestDir string
	configPath  string
)

func init() {
	flag.Set("logtostderr", "true")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Kubeconfig file to access a remote cluster. Warning: For testing only, do not use in production.")
	flag.StringVar(&manifestDir, "manifest-dir", "/manifests", "Path to dir with manifest templates.")
	flag.StringVar(&configPath, "config", "/etc/mao-config/config", "Cluster config file from which to obtain configuration options")
	flag.Parse()
}

const (
	providerAWS     = "aws"
	providerLibvirt = "libvirt"
)

func main() {
	config, err := render.Config(configPath)
	if err != nil {
		glog.Fatalf("Error reading machine-api config: %v", err)
	}

	err = createNamespace(config.TargetNamespace)
	if err != nil {
		glog.Fatalf("Error creating namespace: %v", err)
	}

	// Hack to deploy cluster and machineSet objects
	// TODO: manage the machineSet object by the operator
	go deployMachineSet()

	// TODO: drop x-operator library
	// Integrate with https://github.com/openshift/cluster-version-operator when is ready
	// Consider reuse https://github.com/openshift/library-go/tree/master/pkg/operator/resource
	if err := xoperator.Run(xoperator.Config{
		Client: opclient.NewClient(kubeconfig),
		LeaderElectionConfig: xoperator.LeaderElectionConfig{
			Kubeconfig: kubeconfig,
			Namespace:  optypes.TectonicNamespace,
		},
		OperatorName:   machineAPI.MachineAPIOperatorName,
		AppVersionName: machineAPI.MachineAPIVersionName,
		Renderer: func() []xotypes.UpgradeSpec {
			return rendererFromFile(config)
		},
	}); err != nil {
		glog.Fatalf("Failed to run machine-api operator: %v", err)
	}
}

// rendererFromFile reads the config object on demand from the path and then passes it to the
// renderer.
func rendererFromFile(config *render.OperatorConfig) []xotypes.UpgradeSpec {
	return render.MakeRenderer(config, manifestDir)()
}

func getConfig(kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" {
		glog.V(4).Infof("Loading kube client config from path %q", kubeconfig)
		return clientcmd.BuildConfigFromFlags("", kubeconfig)
	} else {
		glog.V(4).Infof("Using in-cluster kube client config")
		return rest.InClusterConfig()
	}
}

func createNamespace(namespace string) error {
	config, err := getConfig(kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kube config %#v", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Error creating kube client %#v", err)
	}

	_, err = client.CoreV1().Namespaces().Create(&apiv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: namespace},
	})

	switch {
	case apierrors.IsAlreadyExists(err):
		glog.Infof("Namespace %s already exists.", namespace)

	case err == nil:
		glog.Infof("Created namespace %s.", namespace)

	default:
		return err
	}

	return nil
}

func deployMachineSet() {
	//Cluster API client
	config, err := getConfig(kubeconfig)
	if err != nil {
		glog.Fatalf("Error building kube config %#v", err)
	}
	client, err := clientset.NewForConfig(config)
	clusterApiScheme.AddToScheme(scheme.Scheme)
	decode := scheme.Codecs.UniversalDeserializer().Decode
	v1alphaClient := client.ClusterV1alpha1()

	operatorConfig, err := render.Config(configPath)
	if err != nil {
		glog.Fatalf("Error reading machine-api-operator config: %v", err)
	}

	var machinesFolder string
	if operatorConfig.Provider == providerAWS {
		machinesFolder = "machines/aws"
	} else if operatorConfig.Provider == providerLibvirt {
		machinesFolder = "machines/libvirt"
	}

	// Create Cluster object
	clusterTemplateData, err := ioutil.ReadFile(fmt.Sprintf("%s/cluster.yaml", machinesFolder)) // just pass the file name
	if err != nil {
		glog.Fatalf("Error reading %#v", err)
	}

	clusterPopulatedData, err := render.Manifests(operatorConfig, clusterTemplateData)
	if err != nil {
		glog.Fatalf("Unable to render manifests %q: %v", clusterTemplateData, err)
	}

	clusterObj, _, err := decode([]byte(clusterPopulatedData), nil, nil)
	if err != nil {
		glog.Fatalf("Error decoding %#v", err)
	}
	cluster := clusterObj.(*clusterv1.Cluster)

	// Create MachineSet object
	machineSetTemplateData, err := ioutil.ReadFile(fmt.Sprintf("%s/machine-set.yaml", machinesFolder)) // just pass the file name
	if err != nil {
		glog.Fatalf("Error reading %#v", err)
	}

	machineSetPopulatedData, err := render.Manifests(operatorConfig, machineSetTemplateData)
	if err != nil {
		glog.Fatalf("Unable to render manifests %q: %v", machineSetTemplateData, err)
	}

	machineSetObj, _, err := decode([]byte(machineSetPopulatedData), nil, nil)
	if err != nil {
		glog.Fatalf("Error decoding %#v", err)
	}
	machineSet := machineSetObj.(*clusterv1.MachineSet)

	for {
		glog.Info("Trying to deploy Cluster object")
		if _, err := v1alphaClient.Clusters("default").Create(cluster); err != nil {
			glog.Infof("Cannot create cluster, retrying in 5 secs: %v", err)
		} else {
			glog.Info("Created Cluster object")
		}

		glog.Info("Trying to deploy MachineSet object")
		_, err = v1alphaClient.MachineSets("default").Create(machineSet)
		if err != nil {
			glog.Infof("Cannot create MachineSet, retrying in 5 secs: %v", err)
		} else {
			glog.Info("Created MachineSet object Successfully")
			return
		}
		time.Sleep(5 * time.Second)
	}
}
