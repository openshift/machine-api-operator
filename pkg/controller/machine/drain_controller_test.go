package machine

import (
	"context"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
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

	machine.Status.Phase = pointer.StringPtr(phase)
	if phase == PhaseDeleting {
		machine.ObjectMeta.DeletionTimestamp = &now
	}

	return machine
}

func TestDrainControllerReconcileRequest(t *testing.T) {

	getDrainControllerReconciler := func(fakeObjs ...runtime.Object) (*machineDrainController, *record.FakeRecorder) {
		recorder := record.NewFakeRecorder(10)
		return &machineDrainController{
			Client:        fake.NewFakeClientWithScheme(scheme.Scheme, fakeObjs...),
			scheme:        scheme.Scheme,
			eventRecorder: recorder,
		}, recorder
	}

	getDrainedConditions := func(msg string) machinev1.Conditions {
		condition := conditions.TrueCondition(machinev1.MachineDrained)
		condition.Message = msg
		return machinev1.Conditions{*condition}
	}

	t.Run("ignore machine not in the deleting phase", func(t *testing.T) {
		g := NewGomegaWithT(t)
		cases := []struct {
			phase string
		}{
			{phase: PhaseRunning},
			{phase: PhaseFailed},
			{phase: PhaseProvisioning},
			{phase: PhaseProvisioned},
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

		machine := getMachine("deleting", PhaseDeleting)
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

		machine := getMachine("no-node", PhaseDeleting)
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

		machine := getMachine("annotated", PhaseDeleting)
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

		machine := getMachine("drained", PhaseDeleting)
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

			fakeClient := fake.NewFakeClientWithScheme(scheme.Scheme, tc.nodes...)

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
