package vsphere

import (
	"context"
	"fmt"
	"net"
	"path/filepath"
	"testing"
	"time"

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

	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	apimachineryerrors "k8s.io/apimachinery/pkg/api/errors"
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
	eventRecorder := mgr.GetEventRecorderFor("vspherecontroller")
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
			Name:      "testname",
			Namespace: openshiftConfigNamespaceForTest,
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

	_, err = createTagAndCategory(session, tagToCategoryName("CLUSTERID"), "CLUSTERID")
	g.Expect(err).ToNot(HaveOccurred())

	ctx := context.Background()

	cases := []struct {
		name      string
		errorMsg  string
		operation func(actuator *Actuator, machine *machinev1.Machine) error
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
			event:    "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Create machine event failed, reconciler's create failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				machine.Labels[machinev1.MachineClusterIDLabel] = ""
				return actuator.Create(ctx, machine)
			},
			errorMsg: "test: reconciler failed to Create machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
			event:    "test: reconciler failed to Create machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
		},
		{
			name: "Create machine event succeed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Create(ctx, machine)
			},
			event: "Created Machine test",
		},
		{
			name: "Update machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Update(nil, machine) //nolint:staticcheck
			},
			errorMsg: "test: failed to create scope for machine: test: machine scope require a context",
			event:    "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Update machine event failed, reconciler's update failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				machine.Labels[machinev1.MachineClusterIDLabel] = ""
				return actuator.Update(ctx, machine)
			},
			errorMsg: "test: reconciler failed to Update machine: test: failed validating machine provider spec: test: missing \"machine.openshift.io/cluster-api-cluster\" label",
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
			event: "Updated Machine test",
		},
		{
			name: "Delete machine event failed on invalid machine scope",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Delete(nil, machine) //nolint:staticcheck
			},
			errorMsg: "test: failed to create scope for machine: test: machine scope require a context",
			event:    "test: failed to create scope for machine: test: machine scope require a context",
		},
		{
			name: "Delete machine event failed, reconciler's delete failed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Delete(ctx, machine)
			},
			errorMsg: "test: reconciler failed to Delete machine: destroying vm in progress, requeuing",
			event:    "test: reconciler failed to Delete machine: destroying vm in progress, requeuing",
		},
		{
			name: "Delete machine event succeed",
			operation: func(actuator *Actuator, machine *machinev1.Machine) error {
				return actuator.Delete(ctx, machine)
			},
			event: "Deleted machine test",
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
			failedProvStatusUpdate := make(map[string]*machinev1.VSphereMachineProviderStatus)
			params := ActuatorParams{
				Client:                   k8sClient,
				EventRecorder:            eventRecorder,
				APIReader:                k8sClient,
				TaskIDCache:              taskIDCache,
				FailedProvStatusUpdate:   failedProvStatusUpdate,
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
		})
	}
}

func TestFailedProvStatusRetry(t *testing.T) {
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
	eventRecorder := mgr.GetEventRecorderFor("vspherecontroller")

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
			Name:      "testname",
			Namespace: openshiftConfigNamespaceForTest,
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

	_, err = createTagAndCategory(session, tagToCategoryName("CLUSTERID"), "CLUSTERID")
	g.Expect(err).ToNot(HaveOccurred())

	ctx := context.Background()
	timeout := 10 * time.Second

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
	g.Expect(err).ToNot(HaveOccurred())

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
	g.Expect(k8sClient.Create(ctx, node)).To(Succeed())
	defer func() {
		g.Expect(k8sClient.Delete(ctx, node)).To(Succeed())
	}()
	getNode := func() error {
		nodeKey := types.NamespacedName{Name: node.Name}
		return k8sClient.Get(ctx, nodeKey, &corev1.Node{})
	}
	g.Eventually(getNode, timeout).Should(Succeed())

	gate, err := testutils.NewDefaultMutableFeatureGate()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("PatchMachine failure saves provider status and retry succeeds", func(t *testing.T) {
		gs := NewWithT(t)

		machineName := "test-retry-success"
		machine := &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      machineName,
				Namespace: testNamespaceName,
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

		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		defer func() {
			// Clean up if machine still exists
			if err := k8sClient.Delete(ctx, machine); err != nil && !apimachineryerrors.IsNotFound(err) {
				t.Logf("cleanup: %v", err)
			}
		}()

		getMachine := func() error {
			machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
			return k8sClient.Get(ctx, machineKey, &machinev1.Machine{})
		}
		gs.Eventually(getMachine, timeout).Should(Succeed())

		taskIDCache := make(map[string]string)
		failedProvStatusUpdate := make(map[string]*machinev1.VSphereMachineProviderStatus)
		params := ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			FailedProvStatusUpdate:   failedProvStatusUpdate,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		}
		actuator := NewActuator(params)

		// Step 1: Create successfully - the cache should be populated with the task ID.
		err := actuator.Create(ctx, machine)
		gs.Expect(err).ToNot(HaveOccurred())
		gs.Expect(taskIDCache).To(HaveKey(machineName))
		cachedTaskID := taskIDCache[machineName]
		gs.Expect(cachedTaskID).ToNot(BeEmpty())

		// Step 2: Simulate a PatchMachine failure by deleting the machine from the API,
		// then calling Create again. The in-memory machine still has the old TaskRef,
		// so the reconciler runs and sets a new TaskRef. But PatchMachine fails because
		// the machine no longer exists in the API.
		gs.Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
		gs.Eventually(func() bool {
			machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
			return k8sClient.Get(ctx, machineKey, &machinev1.Machine{}) != nil
		}, timeout).Should(BeTrue())

		err = actuator.Create(ctx, machine)
		gs.Expect(err).To(HaveOccurred())

		// Verify the failed provider status was cached for retry.
		gs.Expect(failedProvStatusUpdate).To(HaveKey(machineName))
		gs.Expect(failedProvStatusUpdate[machineName]).ToNot(BeNil())
		gs.Expect(failedProvStatusUpdate[machineName].TaskRef).ToNot(BeEmpty())

		// Step 3: Re-create the machine in the API so PatchMachine can succeed on retry.
		// Reset the in-memory machine to have a stale (empty) TaskRef so
		// the stale check triggers the retry path.
		newMachine := &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      machineName,
				Namespace: testNamespaceName,
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
		gs.Expect(k8sClient.Create(ctx, newMachine)).To(Succeed())
		gs.Eventually(func() error {
			machineKey := types.NamespacedName{Namespace: newMachine.Namespace, Name: newMachine.Name}
			return k8sClient.Get(ctx, machineKey, &machinev1.Machine{})
		}, timeout).Should(Succeed())

		// Call Create with the new machine (which has empty TaskRef, mismatching cache).
		// The retry logic should re-patch the saved status and clear FailedProvStatusUpdate.
		err = actuator.Create(ctx, newMachine)
		gs.Expect(err).ToNot(HaveOccurred())
		gs.Expect(failedProvStatusUpdate).ToNot(HaveKey(machineName))
	})

	t.Run("Stale TaskRef without saved status returns requeue error", func(t *testing.T) {
		gs := NewWithT(t)

		machineName := "test-stale-requeue"
		machine := &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      machineName,
				Namespace: testNamespaceName,
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

		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		defer func() {
			if err := k8sClient.Delete(ctx, machine); err != nil && !apimachineryerrors.IsNotFound(err) {
				t.Logf("cleanup: %v", err)
			}
		}()

		getMachine := func() error {
			machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
			return k8sClient.Get(ctx, machineKey, &machinev1.Machine{})
		}
		gs.Eventually(getMachine, timeout).Should(Succeed())

		// Pre-populate TaskIDCache with a value that won't match the machine's empty TaskRef.
		taskIDCache := map[string]string{
			machineName: "some-old-task-id",
		}
		failedProvStatusUpdate := make(map[string]*machinev1.VSphereMachineProviderStatus)
		params := ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			FailedProvStatusUpdate:   failedProvStatusUpdate,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		}
		actuator := NewActuator(params)

		// No FailedProvStatusUpdate entry, so this should return a RequeueAfterError.
		err := actuator.Create(ctx, machine)
		gs.Expect(err).To(HaveOccurred())

		_, isRequeue := err.(*machinecontroller.RequeueAfterError)
		gs.Expect(isRequeue).To(BeTrue())
	})

	t.Run("Update cleans up FailedProvStatusUpdate", func(t *testing.T) {
		gs := NewWithT(t)

		machineName := "test-update-cleanup"
		machine := &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      machineName,
				Namespace: testNamespaceName,
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

		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		defer func() {
			if err := k8sClient.Delete(ctx, machine); err != nil && !apimachineryerrors.IsNotFound(err) {
				t.Logf("cleanup: %v", err)
			}
		}()

		getMachine := func() error {
			machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
			return k8sClient.Get(ctx, machineKey, &machinev1.Machine{})
		}
		gs.Eventually(getMachine, timeout).Should(Succeed())

		taskIDCache := map[string]string{
			machineName: "some-task-id",
		}
		failedProvStatusUpdate := map[string]*machinev1.VSphereMachineProviderStatus{
			machineName: {TaskRef: "some-task-id"},
		}
		params := ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			FailedProvStatusUpdate:   failedProvStatusUpdate,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		}
		actuator := NewActuator(params)

		err := actuator.Update(ctx, machine)
		gs.Expect(err).ToNot(HaveOccurred())

		gs.Expect(failedProvStatusUpdate).ToNot(HaveKey(machineName))
		gs.Expect(taskIDCache).ToNot(HaveKey(machineName))
	})

	t.Run("Delete cleans up FailedProvStatusUpdate", func(t *testing.T) {
		gs := NewWithT(t)

		machineName := "test-delete-cleanup"
		machine := &machinev1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      machineName,
				Namespace: testNamespaceName,
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

		gs.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		defer func() {
			if err := k8sClient.Delete(ctx, machine); err != nil && !apimachineryerrors.IsNotFound(err) {
				t.Logf("cleanup: %v", err)
			}
		}()

		getMachine := func() error {
			machineKey := types.NamespacedName{Namespace: machine.Namespace, Name: machine.Name}
			return k8sClient.Get(ctx, machineKey, &machinev1.Machine{})
		}
		gs.Eventually(getMachine, timeout).Should(Succeed())

		taskIDCache := map[string]string{
			machineName: "some-task-id",
		}
		failedProvStatusUpdate := map[string]*machinev1.VSphereMachineProviderStatus{
			machineName: {TaskRef: "some-task-id"},
		}
		params := ActuatorParams{
			Client:                   k8sClient,
			EventRecorder:            eventRecorder,
			APIReader:                k8sClient,
			TaskIDCache:              taskIDCache,
			FailedProvStatusUpdate:   failedProvStatusUpdate,
			OpenshiftConfigNamespace: openshiftConfigNamespaceForTest,
			FeatureGates:             gate,
		}
		actuator := NewActuator(params)

		// Delete will fail (vm doesn't exist in vsphere sim for this machine)
		// but it should still clean up the caches before proceeding.
		_ = actuator.Delete(ctx, machine)

		gs.Expect(failedProvStatusUpdate).ToNot(HaveKey(machineName))
		gs.Expect(taskIDCache).ToNot(HaveKey(machineName))
	})
}
