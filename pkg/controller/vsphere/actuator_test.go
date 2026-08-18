package vsphere

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/vmware/govmomi/simulator"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ipamv1beta1 "sigs.k8s.io/cluster-api/api/ipam/v1beta1" //nolint:staticcheck
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

func init() {
	// Add types to scheme
	if err := machinev1.Install(scheme.Scheme); err != nil {
		panic(err)
	}

	if err := configv1.Install(scheme.Scheme); err != nil {
		panic(err)
	}

	if err := ipamv1beta1.AddToScheme(scheme.Scheme); err != nil {
		panic(err)
	}
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

	vm := model.Map().Any("VirtualMachine").(*simulator.VirtualMachine)
	vm.Config.Version = minimumHWVersionString

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1", "zz_generated.crd-manifests"),
			filepath.Join("..", "..", "..", "third_party", "cluster-api", "crd")},
	}

	// Setup k8s test environment
	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	mgr, err := manager.New(cfg, manager.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
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
	eventRecorder := mgr.GetEventRecorder("vspherecontroller")
	configNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: openshiftConfigNamespaceForTest,
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

	credentialsSecretName := "test"
	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecretName,
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

	testConfig := fmt.Sprintf(testConfigFmt, port, credentialsSecretName, testNamespaceName)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OpenshiftConfigManagedConfigMap,
			Namespace: openshiftConfigNamespaceForTest,
		},
		Data: map[string]string{
			OpenshiftConfigManagedCloudConfigKey: testConfig,
		},
	}

	g.Expect(k8sClient.Create(context.Background(), configMap)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), configMap)).To(Succeed())
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

	_, err = createTagAndCategory(session, tagToCategoryName("CLUSTERID"), "CLUSTERID")
	g.Expect(err).ToNot(HaveOccurred())

	ctx := context.Background()

	cases := []struct {
		name      string
		errorMsg  string
		operation func(actuator *Actuator, machine *machinev1.Machine) error
		reason    string
		event     string
	}{
		{
			name: "Create machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				machine.Spec = machinev1.MachineSpec{
					ProviderSpec: machinev1.ProviderSpec{
						Value: &runtime.RawExtension{
							Raw: []byte{'1'},
						},
					},
				}
				return actuator.Create(nil, machine) //nolint:staticcheck
			},
			errorMsg: "test: failed to create scope for machine: test: machine scope require a context",
			reason:   "FailedCreate",
			event:    "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Create machine event failed, reconciler's create failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				machine.Labels[machinev1.MachineClusterIDLabel] = ""
				return actuator.Create(ctx, machine)
			},
			errorMsg: "test: reconciler failed to Create machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
			reason:   "FailedCreate",
			event:    "test: reconciler failed to Create machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
		},
		{
			name: "Create machine event succeed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Create(ctx, machine)
			},
			reason: "Create",
			event:  "Created Machine test",
		},
		{
			name: "Update machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Update(nil, machine) //nolint:staticcheck
			},
			errorMsg: "test: failed to create scope for machine: test: machine scope require a context",
			reason:   "FailedUpdate",
			event:    "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Update machine event failed, reconciler's update failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				machine.Labels[machinev1.MachineClusterIDLabel] = ""
				return actuator.Update(ctx, machine)
			},
			errorMsg: "test: reconciler failed to Update machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
			reason:   "FailedUpdate",
			event:    "test: reconciler failed to Update machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
		},
		{
			name: "Update machine event succeed and only one event is created",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				err := actuator.Update(ctx, machine)
				if err != nil {
					return err
				}
				return actuator.Update(ctx, machine)
			},
			reason: "Update",
			event:  "Updated Machine test",
		},
		{
			name: "Delete machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Delete(nil, machine) //nolint:staticcheck
			},
			errorMsg: "test: failed to create scope for machine: test: machine scope require a context",
			reason:   "FailedDelete",
			event:    "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Delete machine event failed, reconciler's delete failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Delete(ctx, machine)
			},
			errorMsg: "test: reconciler failed to Delete machine: destroying vm in progress, requeuing",
			reason:   "FailedDelete",
			event:    "test: reconciler failed to Delete machine: destroying vm in progress, requeuing",
		},
		{
			name: "Delete machine event succeed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Delete(ctx, machine)
			},
			reason: "Delete",
			event:  "Deleted machine test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			timeout := 10 * time.Second
			gs := NewWithT(t)

			providerSpec, err := RawExtensionFromProviderSpec(&machinev1.VSphereMachineProviderSpec{
				Template: vm.Name,
				Workspace: &machinev1.Workspace{
					Server: host,
				},
				CredentialsSecret: &corev1.LocalObjectReference{
					Name: "test",
				},
				UserDataSecret: &corev1.LocalObjectReference{
					Name: userDataSecretName,
				},
				DiskGiB: 10,
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
				Status: machinev1.MachineStatus{
					NodeRef: &corev1.ObjectReference{
						Name: "test",
					},
				},
			}

			// Create the machine
			gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())

			// Make sure the machine and its event are deleted when the test ends
			defer func() {
				gs.Expect(k8sClient.Delete(context.Background(), machine)).To(Succeed())

				eventList := &corev1.EventList{}
				gs.Expect(k8sClient.List(context.Background(), eventList, client.InNamespace(machine.Namespace))).To(Succeed())
				for i := range eventList.Items {
					gs.Expect(k8sClient.Delete(context.Background(), &eventList.Items[i])).To(Succeed())
				}
			}()

			// Ensure the machine has synced to the cache
			getMachine := func() error {
				machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
				return k8sClient.Get(ctx, machineKey, &machinev1.Machine{})
			}
			gs.Eventually(getMachine, timeout).Should(Succeed())

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						machinev1.MachineClusterIDLabel: "CLUSTERID",
					},
				},
				Spec: corev1.NodeSpec{},
				Status: corev1.NodeStatus{
					VolumesAttached: []corev1.AttachedVolume{},
				},
			}

			// Create the node
			gs.Expect(k8sClient.Create(ctx, node)).To(Succeed())
			defer func() {
				gs.Expect(k8sClient.Delete(ctx, node)).To(Succeed())
			}()

			// Ensure the node has synced to the cache
			getNode := func() error {
				nodeKey := types.NamespacedName{Name: node.Name}
				return k8sClient.Get(ctx, nodeKey, &corev1.Node{})
			}
			gs.Eventually(getNode, timeout).Should(Succeed())

			gate, err := testutils.NewDefaultMutableFeatureGate()
			if err != nil {
				t.Errorf("Unexpected error setting up feature gates: %v", err)
			}

			taskIDCache := make(map[string]string)
			params := ActuatorParams{
				Client:                   k8sClient,
				EventRecorder:            eventRecorder,
				APIReader:                k8sClient,
				TaskIDCache:              taskIDCache,
				OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
				FeatureGates:             gate,
			}

			actuator := NewActuator(params)

			err = tc.operation(actuator, machine)
			if tc.errorMsg == "" {
				gs.Expect(err).ToNot(HaveOccurred())
			} else {
				gs.Expect(err.Error()).To(Equal(tc.errorMsg))
			}

			eventList := &corev1.EventList{}
			var matchingEvent *corev1.Event
			waitForEvent := func() error {
				err := k8sClient.List(ctx, eventList, client.InNamespace(machine.Namespace))
				if err != nil {
					return err
				}

				matchingCount := 0
				matchingEvent = nil
				for i := range eventList.Items {
					event := eventList.Items[i]
					if event.InvolvedObject.Kind == "Machine" &&
						event.InvolvedObject.Name == machine.Name &&
						event.Reason == tc.reason &&
						event.Message == tc.event {
						matchingCount++
						matchingEvent = &event
					}
				}

				if matchingCount == 0 {
					return fmt.Errorf("matching event not found for machine %s", machine.Name)
				}

				if matchingCount > 1 {
					return fmt.Errorf("expected one matching event, got %d", matchingCount)
				}
				return nil
			}

			gs.Eventually(waitForEvent, timeout).Should(Succeed())

			gs.Expect(matchingEvent).ToNot(BeNil())
			gs.Expect(matchingEvent.Reason).To(Equal(tc.reason))
			gs.Expect(matchingEvent.Message).To(Equal(tc.event))
		})
	}
}

func TestTaskIDCacheBehavior(t *testing.T) {
	g := NewWithT(t)

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

	vm := model.Map().Any("VirtualMachine").(*simulator.VirtualMachine)
	vm.Config.Version = minimumHWVersionString

	testEnv := &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "install"),
			filepath.Join("..", "..", "..", "vendor", "github.com", "openshift", "api", "config", "v1", "zz_generated.crd-manifests"),
			filepath.Join("..", "..", "..", "third_party", "cluster-api", "crd")},
	}

	cfg, err := testEnv.Start()
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(cfg).ToNot(BeNil())
	defer func() {
		g.Expect(testEnv.Stop()).To(Succeed())
	}()

	mgr, err := manager.New(cfg, manager.Options{
		Scheme: scheme.Scheme,
		Metrics: metricsserver.Options{
			BindAddress: "0",
		},
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
	eventRecorder := mgr.GetEventRecorder("vspherecontroller")

	configNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: openshiftConfigNamespaceForTest,
		},
	}
	g.Expect(k8sClient.Create(context.Background(), configNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), configNamespace)).To(Succeed())
	}()

	testNamespaceName := "test-cache"
	testNamespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNamespaceName,
		},
	}
	g.Expect(k8sClient.Create(context.Background(), testNamespace)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), testNamespace)).To(Succeed())
	}()

	credentialsSecretName := "test"
	credentialsSecret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      credentialsSecretName,
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

	testConfig := fmt.Sprintf(testConfigFmt, port, credentialsSecretName, testNamespaceName)
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      OpenshiftConfigManagedConfigMap,
			Namespace: openshiftConfigNamespaceForTest,
		},
		Data: map[string]string{
			OpenshiftConfigManagedCloudConfigKey: testConfig,
		},
	}
	g.Expect(k8sClient.Create(context.Background(), configMap)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(context.Background(), configMap)).To(Succeed())
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

	_, err = createTagAndCategory(session, tagToCategoryName("CLUSTERID"), "CLUSTERID")
	g.Expect(err).ToNot(HaveOccurred())

	ctx := context.Background()
	timeout := 10 * time.Second

	newProviderSpec := func() *runtime.RawExtension {
		t.Helper()
		ps, err := RawExtensionFromProviderSpec(&machinev1.VSphereMachineProviderSpec{
			Template: vm.Name,
			Workspace: &machinev1.Workspace{
				Server: host,
			},
			CredentialsSecret: &corev1.LocalObjectReference{
				Name: "test",
			},
			UserDataSecret: &corev1.LocalObjectReference{
				Name: userDataSecretName,
			},
			DiskGiB: 10,
		})
		if err != nil {
			t.Fatal(err)
		}
		return ps
	}

	newMachine := func(name string) *machinev1.Machine {
		return &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: testNamespaceName,
				Labels: map[string]string{
					machinev1.MachineClusterIDLabel: "CLUSTERID",
				},
			},
			Spec: machinev1.MachineSpec{
				ProviderSpec: machinev1.ProviderSpec{
					Value: newProviderSpec(),
				},
			},
		}
	}

	t.Run("TaskIDCache populated on successful create", func(t *testing.T) {
		gs := NewWithT(t)

		gate, err := testutils.NewDefaultMutableFeatureGate()
		gs.Expect(err).ToNot(HaveOccurred())

		taskIDCache := make(map[string]string)
		actuator := NewActuator(ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		})

		machine := newMachine("test-cache-populated")
		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		gs.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}, &machinev1.Machine{})
		}, timeout).Should(Succeed())
		defer func() {
			gs.Expect(k8sClient.Delete(context.Background(), machine)).To(Succeed())
		}()

		err = actuator.Create(ctx, machine)
		gs.Expect(err).ToNot(HaveOccurred())

		gs.Expect(taskIDCache).To(HaveKey(machine.Name))
		gs.Expect(taskIDCache[machine.Name]).ToNot(BeEmpty())
	})

	t.Run("TaskIDCache cleared when PatchMachine fails", func(t *testing.T) {
		gs := NewWithT(t)

		gate, err := testutils.NewDefaultMutableFeatureGate()
		gs.Expect(err).ToNot(HaveOccurred())

		taskIDCache := make(map[string]string)
		actuator := NewActuator(ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		})

		machine := newMachine("test-cache-cleared")
		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		gs.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}, &machinev1.Machine{})
		}, timeout).Should(Succeed())

		// First Create succeeds, populating the cache with the task ID.
		err = actuator.Create(ctx, machine)
		gs.Expect(err).ToNot(HaveOccurred())
		gs.Expect(taskIDCache).To(HaveKey(machine.Name))
		cachedTaskID := taskIDCache[machine.Name]
		gs.Expect(cachedTaskID).ToNot(BeEmpty())

		// Delete the machine from k8s so PatchMachine will fail on the next Create.
		gs.Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
		gs.Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}, &machinev1.Machine{})
			return err != nil
		}, timeout).Should(BeTrue())

		// Second Create: reconciler runs but PatchMachine fails (machine not in k8s).
		// The fix should clear the cache entry so recovery is possible.
		err = actuator.Create(ctx, machine)
		gs.Expect(err).To(HaveOccurred())
		gs.Expect(taskIDCache).ToNot(HaveKey(machine.Name))
	})

	t.Run("TaskIDCache mismatch causes requeue", func(t *testing.T) {
		gs := NewWithT(t)

		gate, err := testutils.NewDefaultMutableFeatureGate()
		gs.Expect(err).ToNot(HaveOccurred())

		taskIDCache := map[string]string{
			"test-cache-mismatch": "stale-task-id-from-failed-patch",
		}
		actuator := NewActuator(ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		})

		machine := newMachine("test-cache-mismatch")
		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		gs.Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}, &machinev1.Machine{})
		}, timeout).Should(Succeed())
		defer func() {
			gs.Expect(k8sClient.Delete(context.Background(), machine)).To(Succeed())
		}()

		// Machine has no TaskRef in providerStatus (empty string), but cache
		// has "stale-task-id-from-failed-patch". This mismatch should trigger
		// a RequeueAfterError to avoid racing with a stale cache.
		err = actuator.Create(ctx, machine)
		gs.Expect(err).To(HaveOccurred())

		var requeueErr *machinecontroller.RequeueAfterError
		gs.Expect(errors.As(err, &requeueErr)).To(BeTrue(), "expected RequeueAfterError, got: %v", err)
		gs.Expect(requeueErr.RequeueAfter).To(Equal(requeueAfterSeconds * time.Second))
	})
}
