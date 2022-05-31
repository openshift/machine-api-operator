package machine

import (
	"context"
	"testing"

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
	if phase == phaseDeleting {
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
			{phase: phaseRunning},
			{phase: phaseFailed},
			{phase: phaseProvisioning},
			{phase: phaseProvisioned},
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

		machine := getMachine("deleting", phaseDeleting)
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

		machine := getMachine("no-node", phaseDeleting)
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

		machine := getMachine("annotated", phaseDeleting)
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

		machine := getMachine("drained", phaseDeleting)
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
