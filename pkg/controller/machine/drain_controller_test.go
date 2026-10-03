package machine

import (
	"context"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kubefake "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/drain"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	machinev1 "github.com/openshift/api/machine/v1beta1"
	"github.com/openshift/machine-api-operator/pkg/util/conditions"
)

func getMachine(name string, phase string) *machinev1.Machine {
	now := metav1.Now()
	machine := &machinev1.Machine{
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   "default",
			Finalizers:  []string{machinev1.MachineFinalizer, metav1.FinalizerDeleteDependents},
			Annotations: map[string]string{},
			Labels: map[string]string{
				machinev1.MachineClusterIDLabel: "testcluster",
			},
		},
		Spec: machinev1.MachineSpec{
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{
					Raw: []byte("{}"),
				},
			},
		},
		Status: machinev1.MachineStatus{
			NodeRef: &corev1.ObjectReference{
				Name: "foo",
			},
		},
	}

	machine.Status.Phase = ptr.To[string](phase)
	if phase == machinev1.PhaseDeleting {
		machine.ObjectMeta.DeletionTimestamp = &now
	}

	return machine
}

func TestDrainControllerReconcileRequest(t *testing.T) {

	getDrainControllerReconciler := func(fakeObjs ...runtime.Object) (*machineDrainController, *record.FakeRecorder) {
		recorder := record.NewFakeRecorder(10)
		return &machineDrainController{
			Client:        fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(fakeObjs...).WithStatusSubresource(&machinev1.Machine{}).Build(),
			scheme:        scheme.Scheme,
			eventRecorder: recorder,
		}, recorder
	}

	getDrainedConditions := func(msg string) []machinev1.Condition {
		condition := conditions.TrueCondition(machinev1.MachineDrained)
		condition.Message = msg
		return []machinev1.Condition{*condition}
	}

	t.Run("ignore machine not in the deleting phase", func(t *testing.T) {
		g := NewGomegaWithT(t)
		cases := []struct {
			phase string
		}{
			{phase: machinev1.PhaseRunning},
			{phase: machinev1.PhaseFailed},
			{phase: machinev1.PhaseProvisioning},
			{phase: machinev1.PhaseProvisioned},
			{phase: "some_random_thing_there"},
		}
		for _, tc := range cases {
			t.Run(tc.phase, func(t *testing.T) {
				machine := getMachine(tc.phase, tc.phase)
				drainController, recorder := getDrainControllerReconciler(machine)
				request := reconcile.Request{NamespacedName: types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}}

				_, err := drainController.Reconcile(context.TODO(), request)
				g.Expect(err).NotTo(HaveOccurred())
				g.Consistently(recorder.Events).ShouldNot(Receive())

				updatedMachine := &machinev1.Machine{}
				g.Expect(drainController.Client.Get(context.TODO(), request.NamespacedName, updatedMachine)).To(Succeed())
				g.Expect(len(updatedMachine.Status.Conditions)).To(BeZero())
			})
		}
	})

	t.Run("hold machine with pre-drain hook", func(t *testing.T) {
		g := NewGomegaWithT(t)

		machine := getMachine("deleting", machinev1.PhaseDeleting)
		machine.Spec.LifecycleHooks.PreDrain = []machinev1.LifecycleHook{{Name: "stop", Owner: "drain"}}

		drainController, recorder := getDrainControllerReconciler(machine)
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}}

		_, err := drainController.Reconcile(context.TODO(), request)
		g.Expect(err).NotTo(HaveOccurred())
		g.Eventually(recorder.Events).Should(Receive(ContainSubstring("Drain blocked by pre-drain hook")))

		updatedMachine := &machinev1.Machine{}
		g.Expect(drainController.Client.Get(context.TODO(), request.NamespacedName, updatedMachine)).To(Succeed())
		g.Expect(len(updatedMachine.Status.Conditions)).To(BeZero())
	})

	t.Run("skip machine without node", func(t *testing.T) {
		g := NewGomegaWithT(t)

		machine := getMachine("no-node", machinev1.PhaseDeleting)
		machine.Status.NodeRef = nil

		drainController, recorder := getDrainControllerReconciler(machine)
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}}

		_, err := drainController.Reconcile(context.TODO(), request)
		g.Expect(err).NotTo(HaveOccurred())
		g.Eventually(recorder.Events).Should(Receive(ContainSubstring("Node drain skipped")))

		updatedMachine := &machinev1.Machine{}
		g.Expect(drainController.Client.Get(context.TODO(), request.NamespacedName, updatedMachine)).To(Succeed())
		expectedConditions := getDrainedConditions("Node drain skipped")
		g.Expect(updatedMachine.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
	})

	t.Run("skip machine with proper annotation", func(t *testing.T) {
		g := NewGomegaWithT(t)

		machine := getMachine("annotated", machinev1.PhaseDeleting)
		machine.ObjectMeta.Annotations[ExcludeNodeDrainingAnnotation] = "here"

		drainController, recorder := getDrainControllerReconciler(machine)
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}}

		_, err := drainController.Reconcile(context.TODO(), request)
		g.Expect(err).NotTo(HaveOccurred())
		g.Eventually(recorder.Events).Should(Receive(ContainSubstring("Node drain skipped")))

		updatedMachine := &machinev1.Machine{}
		g.Expect(drainController.Client.Get(context.TODO(), request.NamespacedName, updatedMachine)).To(Succeed())
		expectedConditions := getDrainedConditions("Node drain skipped")
		g.Expect(updatedMachine.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
	})

	t.Run("ignore already drained machine", func(t *testing.T) {
		g := NewGomegaWithT(t)

		machine := getMachine("drained", machinev1.PhaseDeleting)
		machine.Status.Conditions = getDrainedConditions("Drained")

		drainController, recorder := getDrainControllerReconciler(machine)
		request := reconcile.Request{NamespacedName: types.NamespacedName{Name: machine.Name, Namespace: machine.Namespace}}

		_, err := drainController.Reconcile(context.TODO(), request)
		g.Expect(err).NotTo(HaveOccurred())
		g.Consistently(recorder.Events).ShouldNot(Receive())

		updatedMachine := &machinev1.Machine{}
		g.Expect(drainController.Client.Get(context.TODO(), request.NamespacedName, updatedMachine)).To(Succeed())
		expectedConditions := getDrainedConditions("Drained")
		g.Expect(updatedMachine.Status.Conditions).To(conditions.MatchConditions(expectedConditions))
	})
}

func TestIsDrainAllowed(t *testing.T) {
	cordonedNode := newNode("cordoned", cordoned)
	workerNode := newNode("worker")
	masterNode := newNode("master", masterLabel)
	masterNodeCordoned := newNode("master-cordoned", masterLabel, cordoned)
	controlPlaneNode := newNode("controlplane", controlPlaneLabel)
	controlPlaneNodeCordoned := newNode("controlplane", controlPlaneLabel, cordoned)

	testCases := []struct {
		name          string
		node          *corev1.Node
		nodes         []runtime.Object
		expectedError error
	}{
		{
			name: "With a node that is already cordoned",
			node: cordonedNode,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNode,
				workerNode,
				cordonedNode,
			},
		},
		{
			name: "With a node not labelled as a control plane or master",
			node: workerNode,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNode,
				workerNode,
				cordonedNode,
			},
		},
		{
			name: "With a cordoned control plane node",
			node: controlPlaneNodeCordoned,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNode,
				workerNode,
				cordonedNode,
			},
		},
		{
			name: "With a cordoned master node",
			node: masterNodeCordoned,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNode,
				workerNode,
				cordonedNode,
			},
		},
		{
			name: "With a control plane node",
			node: controlPlaneNode,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNode,
				workerNode,
				cordonedNode,
			},
		},
		{
			name: "With a master node",
			node: masterNode,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNode,
				workerNode,
				cordonedNode,
			},
		},
		{
			name: "With a control plane node and another control plane node is already cordoned",
			node: controlPlaneNode,
			nodes: []runtime.Object{
				controlPlaneNodeCordoned,
				masterNode,
				workerNode,
				cordonedNode,
			},
			expectedError: &RequeueAfterError{RequeueAfter: 20 * time.Second},
		},
		{
			name: "With a control plane node and another master node is already cordoned",
			node: controlPlaneNode,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNodeCordoned,
				workerNode,
				cordonedNode,
			},
			expectedError: &RequeueAfterError{RequeueAfter: 20 * time.Second},
		},
		{
			name: "With a master node and another control plane node is already cordoned",
			node: masterNode,
			nodes: []runtime.Object{
				controlPlaneNodeCordoned,
				masterNode,
				workerNode,
				cordonedNode,
			},
			expectedError: &RequeueAfterError{RequeueAfter: 20 * time.Second},
		},
		{
			name: "With a master node and another master node is already cordoned",
			node: masterNode,
			nodes: []runtime.Object{
				controlPlaneNode,
				masterNodeCordoned,
				workerNode,
				cordonedNode,
			},
			expectedError: &RequeueAfterError{RequeueAfter: 20 * time.Second},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(tc.nodes...).Build()

			d := &machineDrainController{
				Client: fakeClient,
			}

			err := d.isDrainAllowed(ctx, tc.node)
			if tc.expectedError != nil {
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

func newNode(name string, transforms ...func(n *corev1.Node)) *corev1.Node {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: make(map[string]string),
		},
	}

	for _, transform := range transforms {
		transform(n)
	}

	return n
}

func cordoned(n *corev1.Node) {
	n.Spec.Unschedulable = true
}

func controlPlaneLabel(n *corev1.Node) {
	n.GetLabels()[nodeControlPlaneLabel] = ""
}

func masterLabel(n *corev1.Node) {
	n.GetLabels()[nodeMasterLabel] = ""
}

func TestCordonNodeSerializesCPDrains(t *testing.T) {
	g := NewGomegaWithT(t)

	cpNode1 := newNode("cp-node-1", controlPlaneLabel)
	cpNode2 := newNode("cp-node-2", controlPlaneLabel)
	workerNode := newNode("worker-1")

	// controller-runtime fake client for isDrainAllowed (List)
	crClient := fake.NewClientBuilder().
		WithScheme(scheme.Scheme).
		WithRuntimeObjects(cpNode1, cpNode2, workerNode).
		Build()

	// kube fake clientset for drain.RunCordonOrUncordon (node update)
	kubeClient := kubefake.NewSimpleClientset(cpNode1.DeepCopy(), cpNode2.DeepCopy(), workerNode.DeepCopy())

	// Bridge: when the kube client cordons a node via patch (sets Unschedulable=true),
	// also update the controller-runtime fake client so isDrainAllowed sees it.
	// This simulates the informer cache catching up after a direct API write.
	// RunCordonOrUncordon uses a strategic merge patch, so we intercept "patch" actions.
	kubeClient.PrependReactor("patch", "nodes", func(action clienttesting.Action) (bool, runtime.Object, error) {
		patchAction, ok := action.(clienttesting.PatchAction)
		if !ok {
			return false, nil, nil
		}
		nodeName := patchAction.GetName()

		// Propagate the cordon to the controller-runtime fake client
		existing := &corev1.Node{}
		if err := crClient.Get(context.Background(), types.NamespacedName{Name: nodeName}, existing); err == nil {
			existing.Spec.Unschedulable = true
			if err := crClient.Update(context.Background(), existing); err != nil {
				t.Logf("bridge: failed to update CR client for node %s: %v", nodeName, err)
			}
		}
		return false, nil, nil // fall through to the default handler
	})

	d := &machineDrainController{
		Client: crClient,
	}

	makeDrainer := func() *drain.Helper {
		return &drain.Helper{
			Ctx:                 context.Background(),
			Client:              kubeClient,
			Force:               true,
			IgnoreAllDaemonSets: true,
			DeleteEmptyDirData:  true,
			GracePeriodSeconds:  -1,
			Timeout:             20 * time.Second,
			Out:                 writer{t.Log},
			ErrOut:              writer{t.Log},
		}
	}

	var wg sync.WaitGroup
	errs := make([]error, 2)

	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = d.cordonNode(context.Background(), makeDrainer(), cpNode1)
	}()
	go func() {
		defer wg.Done()
		errs[1] = d.cordonNode(context.Background(), makeDrainer(), cpNode2)
	}()
	wg.Wait()

	// Exactly one should succeed and one should get a RequeueAfterError,
	// because the mutex serializes the check+cordon and the second goroutine
	// sees the first node as already cordoned.
	succeeded := 0
	requeued := 0
	for _, err := range errs {
		if err == nil {
			succeeded++
		} else {
			g.Expect(err).To(MatchError(ContainSubstring("drain not permitted")))
			requeued++
		}
	}

	g.Expect(succeeded).To(Equal(1), "exactly one CP drain should succeed")
	g.Expect(requeued).To(Equal(1), "exactly one CP drain should be requeued")
}
