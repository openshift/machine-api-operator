package operator

import (
	"fmt"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1alpha1helpers"
	"github.com/openshift/machine-api-operator/pkg/apis/machineapi/v1alpha1"
	operatorconfigclient "github.com/openshift/machine-api-operator/pkg/generated/clientset/versioned"
	operatorclientinformers "github.com/openshift/machine-api-operator/pkg/generated/informers/externalversions"
	"github.com/openshift/machine-api-operator/pkg/operator/configobservation/configobservercontroller"
	"github.com/openshift/machine-api-operator/pkg/operator/v400_00_assets"
)

func RunOperator(clientConfig *rest.Config, stopCh <-chan struct{}) error {
	kubeClient, err := kubernetes.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	operatorConfigClient, err := operatorconfigclient.NewForConfig(clientConfig)
	if err != nil {
		return err
	}
	dynamicClient, err := dynamic.NewForConfig(clientConfig)
	if err != nil {
		return err
	}

	v1alpha1helpers.EnsureOperatorConfigExists(
		dynamicClient,
		v400_00_assets.MustAsset("v4.00.0/clusterapi-manager/operator-config.yaml"),
		schema.GroupVersionResource{Group: v1alpha1.GroupName, Version: "v1alpha1", Resource: "openshiftapiserveroperatorconfigs"},
		v1alpha1helpers.GetImageEnv,
	)

	operatorConfigInformers := operatorclientinformers.NewSharedInformerFactory(operatorConfigClient, 10*time.Minute)
	kubeInformersForOpenShiftClusterAPINamespace := kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 10*time.Minute, kubeinformers.WithNamespace(targetNamespaceName))

	operatorClient := &operatorClient{
		informers: operatorConfigInformers,
		client:    operatorConfigClient.MachineapiV1alpha1(),
	}

	workloadController := NewWorkloadController(
		os.Getenv("IMAGE"),
		operatorConfigInformers.Machineapi().V1alpha1().MachineAPIOperatorConfigs(),
		kubeInformersForOpenShiftClusterAPINamespace,
		operatorConfigClient.MachineapiV1alpha1(),
		kubeClient,
	)

	configObserver := configobservercontroller.NewConfigObserver(
		operatorClient,
		operatorConfigInformers,
	)

	clusterOperatorStatus := status.NewClusterOperatorStatusController(
		"openshift-cluster-api",
		"machine-api-operator",
		dynamicClient,
		operatorClient,
	)

	operatorConfigInformers.Start(stopCh)
	kubeInformersForOpenShiftClusterAPINamespace.Start(stopCh)

	go workloadController.Run(1, stopCh)
	go configObserver.Run(1, stopCh)
	go clusterOperatorStatus.Run(1, stopCh)

	<-stopCh
	return fmt.Errorf("stopped")
}
