package vsphere

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/vmware/govmomi/simulator"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"
)

// TestActuatorConcurrentReconciliation drives Exists()+Update() for several distinct
// machines concurrently, as Change 4's MaxConcurrentReconciles enables. It exists primarily
// to be run with `-race`, guarding against regressions that reintroduce unsynchronized maps
// on the Actuator (TaskIDCache, scopeCache) now that reconciliation is no longer guaranteed
// to be serialized.
func TestActuatorConcurrentReconciliation(t *testing.T) {
	g := NewWithT(t)

	const numMachines = 5

	model, server := initSimulatorCustom(t, func(m *simulator.Model) {
		m.Machine = numMachines
	})
	defer model.Remove()
	defer server.Close()

	simSession := getSimulatorSession(t, server)

	host, port, err := net.SplitHostPort(server.URL.Host)
	g.Expect(err).ToNot(HaveOccurred())

	namespace := "test"
	credentialsSecretUsername := fmt.Sprintf("%s.username", host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", host)
	password, _ := server.URL.User.Password()

	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	testConfig := fmt.Sprintf(testConfigFmt, port, "test", namespace)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OpenshiftConfigManagedConfigMap,
			Namespace: openshiftConfigNamespaceForTest,
		},
		Data: map[string]string{
			OpenshiftConfigManagedCloudConfigKey: testConfig,
		},
	}

	_, err = createTagAndCategory(simSession, tagToCategoryName("CLUSTERID"), "CLUSTERID")
	g.Expect(err).ToNot(HaveOccurred())

	vms := model.Map().All("VirtualMachine")
	g.Expect(len(vms)).To(BeNumerically(">=", numMachines))

	machines := make([]*machinev1.Machine, numMachines)
	for i := 0; i < numMachines; i++ {
		vm := vms[i].(*simulator.VirtualMachine)
		instanceUUID := fmt.Sprintf("aaaaaaaa-bbbb-cccc-dddd-%012d", i)
		vm.Config.InstanceUuid = instanceUUID

		providerSpec, err := RawExtensionFromProviderSpec(&machinev1.VSphereMachineProviderSpec{
			Workspace: &machinev1.Workspace{
				Server: host,
			},
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: "test",
			},
			Template: vm.Name,
			Network: machinev1.NetworkSpec{
				Devices: []machinev1.NetworkDeviceSpec{
					{NetworkName: "test"},
				},
			},
		})
		g.Expect(err).ToNot(HaveOccurred())

		machines[i] = &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("test-%d", i),
				Namespace: namespace,
				UID:       apimachinerytypes.UID(instanceUUID),
				Labels: map[string]string{
					machinev1.MachineClusterIDLabel: "CLUSTERID",
				},
			},
			Spec: machinev1.MachineSpec{
				ProviderSpec: machinev1.ProviderSpec{
					Value: providerSpec,
				},
			},
		}
	}

	objs := []runtimeclient.Object{credentialsSecret, configMap}
	for _, machine := range machines {
		objs = append(objs, machine)
	}

	client := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(objs...).WithStatusSubresource(&machinev1.Machine{}).Build()

	gate, err := testutils.NewDefaultMutableFeatureGate()
	g.Expect(err).ToNot(HaveOccurred())

	a := NewActuator(ActuatorParams{
		Client:                   client,
		APIReader:                client,
		EventRecorder:            events.NewFakeRecorder(numMachines * 4),
		FeatureGates:             gate,
		OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
	})

	var wg sync.WaitGroup
	errs := make([]error, numMachines)
	for i := 0; i < numMachines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			machine := machines[i]
			ctx := context.Background()

			exists, err := a.Exists(ctx, machine)
			if err != nil {
				errs[i] = fmt.Errorf("exists: %w", err)
				return
			}
			if !exists {
				errs[i] = fmt.Errorf("expected machine %d to exist", i)
				return
			}
			if err := a.Update(ctx, machine); err != nil {
				errs[i] = fmt.Errorf("update: %w", err)
				return
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		g.Expect(err).ToNot(HaveOccurred(), "machine %d", i)
	}

	// scopeCache must not leak entries once every machine's Update() has completed.
	for _, machine := range machines {
		_, ok := a.scopeCache.Load(machine.GetUID())
		g.Expect(ok).To(BeFalse(), "expected scope cache entry for %s to be cleared", machine.GetName())
	}
}
