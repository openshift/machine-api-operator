package vsphere

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	vsphereapi "github.com/openshift/machine-api-operator/pkg/apis/vsphereprovider/v1beta1"
	"github.com/vmware/govmomi/simulator"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func init() {
	// Add types to scheme
	machinev1.AddToScheme(scheme.Scheme)
	configv1.AddToScheme(scheme.Scheme)
}

func TestMachineEvents(t *testing.T) {
	g := NewWithT(t)

	// Setup vsphere test environment
	model, session, server := initSimulator(t)
	defer model.Remove()
	defer server.Close()

	host, port, err := net.SplitHostPort(server.URL.Host)
	if err != nil {
		t.Fatal(err)
	}

	credentialsSecretUsername := fmt.Sprintf("%s.username", host)
	credentialsSecretPassword := fmt.Sprintf("%s.password", host)
	password, _ := server.URL.User.Password()

	vm := simulator.Map.Any("VirtualMachine").(*simulator.VirtualMachine)

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1")},
	}

	// Setup k8s test environment
	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	mgr, err := manager.New(cfg, manager.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
	})
	if err != nil {
		t.Fatal(err)
	}

	mgrCtx, cancel := context.WithCancel(context.Background())

	go func() {
		g.Expect(mgr.Start(mgrCtx)).To(Succeed())
	}()
	defer cancel()

	k8sClient := mgr.GetClient()
	eventRecorder := mgr.GetEventRecorderFor("vspherecontroller")

	configNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: openshiftConfigNamespace,
		},
	}
	g.Expect(k8sClient.Create(context.Background(), configNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), configNamespace)).To(Succeed())
	}()

	testNamespaceName := "test"

	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespaceName,
		},
	}
	g.Expect(k8sClient.Create(context.Background(), testNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), testNamespace)).To(Succeed())
	}()

	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: testNamespaceName,
		},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}

	g.Expect(k8sClient.Create(context.Background(), &credentialsSecret)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), &credentialsSecret)).To(Succeed())
	}()

	testConfig := fmt.Sprintf(testConfigFmt, port)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testname",
			Namespace: openshiftConfigNamespace,
		},
		Data: map[string]string{
			"testkey": testConfig,
		},
	}

	g.Expect(k8sClient.Create(context.Background(), configMap)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), configMap)).To(Succeed())
	}()

	infra := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: globalInfrastuctureName,
		},
		Spec: configv1.InfrastructureSpec{
			CloudConfig: configv1.ConfigMapFileReference{
				Name: "testname",
				Key:  "testkey",
			},
		},
	}
	g.Expect(k8sClient.Create(context.Background(), infra)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), infra)).To(Succeed())
	}()

	userDataSecretName := "vsphere-ignition"
	userDataSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      userDataSecretName,
			Namespace: testNamespaceName,
		},
		Data: map[string][]byte{
			userDataSecretKey: []byte("{}"),
		},
	}

	g.Expect(k8sClient.Create(context.Background(), userDataSecret)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), userDataSecret)).To(Succeed())
	}()

	createTagAndCategory(session, "CLUSTERID", "CLUSTERID")

	ctx := context.Background()

	cases := []struct {
		name      string
		error     string
		operation func(actuator *Actuator, machine *machinev1.Machine)
		event     string
	}{
		{
			name: "Create machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Create(nil, machine)
			},
			event: "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Create machine event failed, reconciler's create failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				machine.Labels[machinev1.MachineClusterIDLabel] = ""
				actuator.Create(ctx, machine)
			},
			event: "test: reconciler failed to Create machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
		},
		{
			name: "Create machine event succeed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Create(ctx, machine)
			},
			event: "Created Machine test",
		},
		{
			name: "Update machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Update(nil, machine)
			},
			event: "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Update machine event failed, reconciler's update failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				machine.Labels[machinev1.MachineClusterIDLabel] = ""
				actuator.Update(ctx, machine)
			},
			event: "test: reconciler failed to Update machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
		},
		{
			name: "Update machine event succeed and only one event is created",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Update(ctx, machine)
				actuator.Update(ctx, machine)
			},
			event: "Updated Machine test",
		},
		{
			name: "Delete machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Delete(nil, machine)
			},
			event: "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Delete machine event failed, reconciler's delete failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Delete(ctx, machine)
			},
			event: "test: reconciler failed to Delete machine: destroying vm in progress, reconciling",
		},
		{
			name: "Delete machine event succeed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) {
				actuator.Delete(ctx, machine)
			},
			event: "Deleted machine test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			timeout := 10 * time.Second
			gs := NewWithT(t)

			providerSpec, err := vsphereapi.RawExtensionFromProviderSpec(&vsphereapi.VSphereMachineProviderSpec{
				Template: vm.Name,
				Workspace: &vsphereapi.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
				DiskGiB: 1,
			})
			gs.Expect(err).ToNot(HaveOccurred())

			machine := &machinev1.Machine{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
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

			// Create the machine
			gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
			defer func() {
				gs.Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
			}()

			// Ensure the machine has synced to the cache
			getMachine := func() error {
				machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
				return k8sClient.Get(ctx, machineKey, machine)
			}
			gs.Eventually(getMachine, timeout).Should(Succeed())

			taskIDCache := make(map[string]string)
			params := ActuatorParams{
				Client:        k8sClient,
				EventRecorder: eventRecorder,
				APIReader:     k8sClient,
				TaskIDCache:   taskIDCache,
			}

			actuator := NewActuator(params)
			tc.operation(actuator, machine)

			eventList := &v1.EventList{}
			waitForEvent := func() error {
				err := k8sClient.List(ctx, eventList, client.InNamespace(machine.Namespace))
				if err != nil {
					return err
				}

				if len(eventList.Items) != 1 {
					return fmt.Errorf("expected len 1, got %d", len(eventList.Items))
				}

				if eventList.Items[0].Count != 1 {
					return fmt.Errorf("expected event %v to happen only once", eventList.Items[0].Name)
				}
				return nil
			}

			gs.Eventually(waitForEvent, timeout).Should(Succeed())

			gs.Expect(eventList.Items[0].Message).To(Equal(tc.event))

			for i := range eventList.Items {
				gs.Expect(k8sClient.Delete(ctx, &eventList.Items[i])).To(Succeed())
			}
		})
	}
}
