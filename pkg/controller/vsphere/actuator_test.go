package vsphere

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	testutils "github.com/openshift/machine-api-operator/pkg/util/testing"

	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/simulator"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/events"
	ipamv1beta1 "sigs.k8s.io/cluster-api/api/ipam/v1beta1" //nolint:staticcheck
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
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

// TestActuatorCreateTaskRefLifecycle verifies the actuator's clone task
// reference bookkeeping: the reference is remembered even when the status patch
// that would persist it is denied, so a retry reconciles the same task instead
// of requeueing forever or submitting a duplicate clone (OCPBUGS-100316).
func TestActuatorCreateTaskRefLifecycle(t *testing.T) {
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
	namespace := "test"

	vm := model.Map().Any("VirtualMachine").(*simulator.VirtualMachine)
	vm.Config.Version = minimumHWVersionString

	credentialsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: namespace},
		Data: map[string][]byte{
			credentialsSecretUsername: []byte(server.URL.User.Username()),
			credentialsSecretPassword: []byte(password),
		},
	}
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: OpenshiftConfigManagedConfigMap, Namespace: openshiftConfigNamespaceForTest},
		Data:       map[string]string{OpenshiftConfigManagedCloudConfigKey: fmt.Sprintf(testConfigFmt, port, "test", namespace)},
	}
	userDataSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "vsphere-ignition", Namespace: namespace},
		Data:       map[string][]byte{userDataSecretKey: []byte("{}")},
	}

	newMachine := func(name string) *machinev1.Machine {
		providerSpec, err := RawExtensionFromProviderSpec(&machinev1.VSphereMachineProviderSpec{
			Template:          vm.Name,
			Workspace:         &machinev1.Workspace{Server: host},
			CredentialsSecret: &corev1.LocalObjectReference{Name: "test"},
			UserDataSecret:    &corev1.LocalObjectReference{Name: "vsphere-ignition"},
			DiskGiB:           10,
		})
		if err != nil {
			t.Fatal(err)
		}
		return &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
				Labels:    map[string]string{machinev1.MachineClusterIDLabel: "CLUSTERID"},
			},
			Spec:   machinev1.MachineSpec{ProviderSpec: machinev1.ProviderSpec{Value: providerSpec}},
			Status: machinev1.MachineStatus{},
		}
	}

	denyStatusPatch := func(base client.WithWatch) client.WithWatch {
		return interceptor.NewClient(base, interceptor.Funcs{
			SubResourcePatch: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ client.Patch, _ ...client.SubResourcePatchOption) error {
				return fmt.Errorf("admission webhook denied the request")
			},
		})
	}

	gates, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Fatalf("unexpected error setting up feature gates: %v", err)
	}

	waitForCloneTask := func(g *WithT, taskRef string) {
		moTask, err := session.GetTask(context.TODO(), taskRef)
		g.Expect(err).ToNot(HaveOccurred())
		if moTask != nil {
			g.Expect(object.NewTask(session.Client.Client, moTask.Reference()).Wait(context.TODO())).To(Succeed())
		}
	}

	t.Run("a denied status patch retains the clone task reference", func(t *testing.T) {
		g := NewWithT(t)
		machine := newMachine("patch-denied")

		base := fake.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(machine).
			WithRuntimeObjects(credentialsSecret, configMap, userDataSecret, machine).
			Build()

		taskIDCache := map[string]string{}
		actuator := NewActuator(ActuatorParams{
			Client:                   denyStatusPatch(base),
			APIReader:                base,
			EventRecorder:            events.NewFakeRecorder(10),
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gates,
		})

		err := actuator.Create(context.Background(), machine)
		g.Expect(err).To(HaveOccurred())
		// The clone identity must survive the failed patch so the next reconcile
		// reconciles the same task rather than submitting a duplicate clone.
		g.Expect(taskIDCache).To(HaveKey(machine.Name))
		g.Expect(taskIDCache[machine.Name]).ToNot(BeEmpty())

		waitForCloneTask(g, taskIDCache[machine.Name])
	})

	t.Run("a successful patch caches the clone task reference", func(t *testing.T) {
		g := NewWithT(t)
		machine := newMachine("patch-ok")

		c := fake.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(machine).
			WithRuntimeObjects(credentialsSecret, configMap, userDataSecret, machine).
			Build()

		taskIDCache := map[string]string{}
		actuator := NewActuator(ActuatorParams{
			Client:                   c,
			APIReader:                c,
			EventRecorder:            events.NewFakeRecorder(10),
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gates,
		})

		g.Expect(actuator.Create(context.Background(), machine)).To(Succeed())
		g.Expect(taskIDCache).To(HaveKey(machine.Name))
		g.Expect(taskIDCache[machine.Name]).ToNot(BeEmpty())

		waitForCloneTask(g, taskIDCache[machine.Name])
	})

	t.Run("a lost task reference is reconciled on retry without a second clone", func(t *testing.T) {
		g := NewWithT(t)

		// Hold the clone task in-flight so the cloned VM is not yet discoverable
		// in vCenter - the exact window in which a lost TaskRef previously caused
		// a duplicate clone submission.
		simulator.TaskDelay.MethodDelay = map[string]int{"CloneVm": 2000, "LockHandoff": 0}
		defer func() { simulator.TaskDelay = simulator.DelayConfig{} }()

		machine := newMachine("inflight")
		base := fake.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(machine).
			WithRuntimeObjects(credentialsSecret, configMap, userDataSecret, machine).
			Build()

		taskIDCache := map[string]string{}
		actuator := NewActuator(ActuatorParams{
			Client:                   denyStatusPatch(base),
			APIReader:                base,
			EventRecorder:            events.NewFakeRecorder(10),
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gates,
		})

		vmCountBefore := len(model.Map().All("VirtualMachine"))
		cloneTasksBefore := countTasksMatching(model, cloneVmTaskDescriptionId)

		// First reconcile submits the clone; the status patch is denied so the
		// TaskRef lives only in the cache.
		g.Expect(actuator.Create(context.Background(), machine)).To(HaveOccurred())
		g.Expect(taskIDCache).To(HaveKey(machine.Name))
		g.Expect(countTasksMatching(model, cloneVmTaskDescriptionId)).To(Equal(cloneTasksBefore+1), "first reconcile must submit exactly one clone")
		// The clone is still running, so the VM is not yet discoverable. This
		// confirms the in-flight window is actually reproduced.
		g.Expect(model.Map().All("VirtualMachine")).To(HaveLen(vmCountBefore), "clone should still be in-flight (VM not yet created)")

		// Retry the way the machine controller would: with a freshly read
		// Machine. Because the status patch was denied, the persisted object has
		// no TaskRef, so the actuator must recover it from the cache rather than
		// submit a second clone. (Reusing the in-memory pointer would hide the
		// bug, since PatchMachine mutates it before the failed patch.)
		fresh := &machinev1.Machine{}
		g.Expect(base.Get(context.Background(), client.ObjectKeyFromObject(machine), fresh)).To(Succeed())
		g.Expect(fresh.Status.ProviderStatus).To(BeNil(), "denied status patch must not have persisted a TaskRef")
		g.Expect(actuator.Create(context.Background(), fresh)).To(HaveOccurred())
		g.Expect(countTasksMatching(model, cloneVmTaskDescriptionId)).To(Equal(cloneTasksBefore+1), "retry must not submit a second clone")

		// Let the clone finish before teardown.
		waitForCloneTask(g, taskIDCache[machine.Name])
	})

	t.Run("a stale nonempty task reference is reconciled from the cache on retry", func(t *testing.T) {
		g := NewWithT(t)

		machine := newMachine("stale-nonempty")
		base := fake.NewClientBuilder().WithScheme(scheme.Scheme).
			WithStatusSubresource(machine).
			WithRuntimeObjects(credentialsSecret, configMap, userDataSecret, machine).
			Build()

		taskIDCache := map[string]string{}

		// Clone successfully first, so the Machine object and the cache both
		// track the clone task and the (powered-off) VM exists.
		allowActuator := NewActuator(ActuatorParams{
			Client:                   base,
			APIReader:                base,
			EventRecorder:            events.NewFakeRecorder(10),
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gates,
		})
		g.Expect(allowActuator.Create(context.Background(), machine)).To(Succeed())
		cloneTaskRef := taskIDCache[machine.Name]
		g.Expect(cloneTaskRef).ToNot(BeEmpty())
		waitForCloneTask(g, cloneTaskRef)

		// Hold power-on in-flight and deny status patches, so the power-on task
		// is submitted but never persisted onto the Machine object.
		simulator.TaskDelay.MethodDelay = map[string]int{"PowerOnMultiVM": 2000, "LockHandoff": 0}
		defer func() { simulator.TaskDelay = simulator.DelayConfig{} }()

		denyActuator := NewActuator(ActuatorParams{
			Client:                   denyStatusPatch(base),
			APIReader:                base,
			EventRecorder:            events.NewFakeRecorder(10),
			TaskIDCache:              taskIDCache,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gates,
		})

		powerOnBefore := countTasksMatching(model, powerOnTaskDescriptionID)

		// Reconcile the finished clone: submits a power-on task and advances the
		// cache, but the denied patch leaves the Machine object on the clone task.
		fresh1 := &machinev1.Machine{}
		g.Expect(base.Get(context.Background(), client.ObjectKeyFromObject(machine), fresh1)).To(Succeed())
		g.Expect(denyActuator.Create(context.Background(), fresh1)).To(HaveOccurred())
		g.Expect(countTasksMatching(model, powerOnTaskDescriptionID)).To(Equal(powerOnBefore+1), "reconciling the finished clone must submit exactly one power-on")
		g.Expect(taskIDCache[machine.Name]).ToNot(Equal(cloneTaskRef), "cache should have advanced to the power-on task")

		// Retry with a freshly read Machine, which still carries the stale clone
		// task because the power-on patch was denied. The actuator must recover
		// the newer power-on task from the cache instead of reprocessing the
		// clone and submitting a second power-on.
		fresh2 := &machinev1.Machine{}
		g.Expect(base.Get(context.Background(), client.ObjectKeyFromObject(machine), fresh2)).To(Succeed())
		g.Expect(denyActuator.Create(context.Background(), fresh2)).To(HaveOccurred())
		g.Expect(countTasksMatching(model, powerOnTaskDescriptionID)).To(Equal(powerOnBefore+1), "retry must not submit a second power-on")

		// Let the power-on finish before teardown.
		moTask, err := session.GetTask(context.TODO(), taskIDCache[machine.Name])
		g.Expect(err).ToNot(HaveOccurred())
		if moTask != nil {
			g.Expect(object.NewTask(session.Client.Client, moTask.Reference()).Wait(context.TODO())).To(Succeed())
		}
	})
}

// powerOnTaskDescriptionID is the DescriptionId of the task issued when powering
// on a VM via Datacenter.PowerOnVM in the simulator.
const powerOnTaskDescriptionID = "powerOnMultiVM"

// countTasksMatching returns the number of tasks in the simulator inventory
// whose DescriptionId contains the given substring.
func countTasksMatching(model *simulator.Model, descriptionSubstring string) int {
	count := 0
	for _, ref := range model.Map().AllReference("") {
		if task, ok := ref.(*simulator.Task); ok && strings.Contains(task.Info.DescriptionId, descriptionSubstring) {
			count++
		}
	}
	return count
}
