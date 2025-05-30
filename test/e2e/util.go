package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/api/machine/v1beta1"
	configclient "github.com/openshift/client-go/config/clientset/versioned"
	machinesetclient "github.com/openshift/client-go/machine/clientset/versioned/typed/machine/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/scale"
	e2e "k8s.io/kubernetes/test/e2e/framework"
	e2eskipper "k8s.io/kubernetes/test/e2e/framework/skipper"
)

const (
	MachineAPINamespace = "openshift-machine-api"
	MachineAPIGroup     = "machine.openshift.io"
	ScaleTimeout        = time.Second * 5
)

// SkipUnlessMachineAPIOperator is used to deterine if the Machine API is installed and running in a cluster.
// It is expected to skip the test if it determines that the Machine API is not installed/running.
// Use this early in a test that relies on Machine API functionality.
//
// It checks to see if the machine custom resource is installed in the cluster.
// If machines are not installed, or there are no machines in the cluster, it skips the test case.
// It then checks to see if the `openshift-machine-api` namespace is installed.
// If the namespace is not present it skips the test case.
func SkipUnlessMachineAPIOperator(dc dynamic.Interface, c coreclient.NamespaceInterface) {
	machineClient := dc.Resource(schema.GroupVersionResource{Group: "machine.openshift.io", Resource: "machines", Version: "v1beta1"})

	err := wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		// Listing the resource will return an IsNotFound error when the CRD has not been installed.
		// Otherwise it would return an empty list if no Machines are in use, which should not be
		// possible if the MachineAPI operator is in use.
		machines, err := machineClient.List(context.Background(), metav1.ListOptions{})
		// If no error was returned and the list of Machines is populated, this cluster is using MachineAPI
		if err == nil {
			// If the Machine CRD exists but there are no Machine objects in the cluster we should
			// skip the test because any cluster that is using MachineAPI from the install will have
			// Machines for the control plane nodes at the minimum.
			if len(machines.Items) == 0 {
				e2eskipper.Skipf("The cluster supports the Machine CRD but has no Machines available")
			}

			return true, nil
		}

		// Not found error on the Machine CRD, cluster is not using MachineAPI
		if errors.IsNotFound(err) {
			e2eskipper.Skipf("The cluster does not support machine instances")
		}
		e2e.Logf("Unable to check for machine api operator: %v", err)
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred())

	err = wait.PollImmediate(time.Second, time.Minute, func() (bool, error) {
		// Check if the openshift-machine-api namespace is present, if not then this
		// cluster is not using MachineAPI.
		_, err := c.Get(context.Background(), "openshift-machine-api", metav1.GetOptions{})
		if err == nil {
			return true, nil
		}
		if errors.IsNotFound(err) {
			e2eskipper.Skipf("The cluster machines are not managed by machine api operator")
		}
		e2e.Logf("Unable to check for machine api operator: %v", err)
		return false, nil
	})
	Expect(err).NotTo(HaveOccurred())
}

func LoadInfra(cfg *rest.Config) *configv1.Infrastructure {
	configClient, err := configclient.NewForConfig(cfg)
	Expect(err).NotTo(HaveOccurred())

	infra, err := configClient.ConfigV1().Infrastructures().Get(context.Background(), "cluster", metav1.GetOptions{})
	Expect(err).NotTo(HaveOccurred())

	return infra
}

func GetMachineSets(cfg *rest.Config) (*v1beta1.MachineSetList, error) {
	ctx := context.Background()
	client, err := machinesetclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	ms := client.MachineSets(MachineAPINamespace)
	return ms.List(ctx, metav1.ListOptions{})
}

// ScaleMachineSet scales a machineSet with a given name to the given number of replicas.
// This was borrowed from origin.  Ideally we should make this a sharable method if possible.
func ScaleMachineSet(cfg *rest.Config, name string, replicas int) error {
	scaleClient, err := GetScaleClient(cfg)
	if err != nil {
		return fmt.Errorf("error calling getScaleClient: %v", err)
	}

	// Depending on how long its been since machineset was create, we may hit an issue.  This eventually will just try
	// again for a few seconds and then quit if unable to scale.
	Eventually(func() error {
		scale, err := scaleClient.Scales(MachineAPINamespace).Get(context.Background(), schema.GroupResource{Group: MachineAPIGroup, Resource: "MachineSet"}, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("error calling scaleClient.Scales get: %v", err)
		}

		scaleUpdate := scale.DeepCopy()
		scaleUpdate.Spec.Replicas = int32(replicas)
		_, err = scaleClient.Scales(MachineAPINamespace).Update(context.Background(), schema.GroupResource{Group: MachineAPIGroup, Resource: "MachineSet"}, scaleUpdate, metav1.UpdateOptions{})
		if err != nil {
			return fmt.Errorf("error calling scaleClient.Scales update while setting replicas to %d: %v", err, replicas)
		}
		return nil
	}, ScaleTimeout).ShouldNot(HaveOccurred())
	return nil
}

func GetScaleClient(cfg *rest.Config) (scale.ScalesGetter, error) {
	discoveryClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("error discovering client: %v", err)
	}

	groupResources, err := restmapper.GetAPIGroupResources(discoveryClient)
	if err != nil {
		return nil, fmt.Errorf("error getting API resources: %v", err)
	}
	restMapper := restmapper.NewDiscoveryRESTMapper(groupResources)
	scaleKindResolver := scale.NewDiscoveryScaleKindResolver(discoveryClient)

	scaleClient, err := scale.NewForConfig(cfg, restMapper, dynamic.LegacyAPIPathResolverFunc, scaleKindResolver)
	if err != nil {
		return nil, fmt.Errorf("error creating scale client: %v", err)
	}
	return scaleClient, nil
}

func CreateMachine(ctx context.Context, cfg *rest.Config, mc *machinesetclient.MachineV1beta1Client, machineName, role string, provider *runtime.RawExtension) (*v1beta1.Machine, error) {
	// Get infra for configs
	infra := LoadInfra(cfg)

	machine := &v1beta1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineSet",
			APIVersion: "machine.openshift.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      machineName,
			Namespace: MachineAPINamespace,
			Labels: map[string]string{
				"machine.openshift.io/test":                     machineName,
				"machine.openshift.io/cluster-api-cluster":      infra.Status.InfrastructureName,
				"machine.openshift.io/cluster-api-machine-role": role,
				"machine.openshift.io/cluster-api-machine-type": role,
			},
		},
		Spec: v1beta1.MachineSpec{
			ObjectMeta: v1beta1.ObjectMeta{},
			ProviderSpec: v1beta1.ProviderSpec{
				Value: provider,
			},
			Taints: []v1.Taint{
				{
					Effect: v1.TaintEffectNoSchedule,
					Key:    "mapi-e2e",
					Value:  "yes",
				},
			},
		},
	}

	return mc.Machines(MachineAPINamespace).Create(ctx, machine, metav1.CreateOptions{})
}

func CreateMachineSet(ctx context.Context, cfg *rest.Config, mc *machinesetclient.MachineV1beta1Client, name, role string, provider *runtime.RawExtension) (*v1beta1.MachineSet, error) {
	replicas := int32(0)

	// Get infra for configs
	infra := LoadInfra(cfg)

	machineset := &v1beta1.MachineSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "MachineSet",
			APIVersion: "machine.openshift.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: MachineAPINamespace,
			Labels: map[string]string{
				"machine.openshift.io/test": name,
			},
		},
		Spec: v1beta1.MachineSetSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"machine.openshift.io/cluster-api-cluster":    infra.Status.InfrastructureName,
					"machine.openshift.io/cluster-api-machineset": name,
				},
			},
			Replicas: &replicas,
			Template: v1beta1.MachineTemplateSpec{
				ObjectMeta: v1beta1.ObjectMeta{
					Labels: map[string]string{
						"machine.openshift.io/cluster-api-machineset":   name,
						"machine.openshift.io/cluster-api-cluster":      infra.Status.InfrastructureName,
						"machine.openshift.io/cluster-api-machine-role": role,
						"machine.openshift.io/cluster-api-machine-type": role,
					},
				},
				Spec: v1beta1.MachineSpec{
					LifecycleHooks: v1beta1.LifecycleHooks{},
					ObjectMeta:     v1beta1.ObjectMeta{},
					ProviderSpec: v1beta1.ProviderSpec{
						Value: provider,
					},
					Taints: []v1.Taint{
						{
							Effect: v1.TaintEffectNoSchedule,
							Key:    "mapi-e2e",
							Value:  "yes",
						},
					},
				},
			},
		},
	}

	return mc.MachineSets(MachineAPINamespace).Create(ctx, machineset, metav1.CreateOptions{})
}
