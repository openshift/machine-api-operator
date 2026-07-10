package vsphere

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
	machinev1 "github.com/openshift/api/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
)

func TestHashProviderSpec(t *testing.T) {
	g := NewWithT(t)

	g.Expect(hashProviderSpec(nil)).To(HaveLen(16))
	g.Expect(hashProviderSpec([]byte("foo"))).To(Equal(hashProviderSpec([]byte("foo"))))
	g.Expect(hashProviderSpec([]byte("foo"))).ToNot(Equal(hashProviderSpec([]byte("bar"))))
	g.Expect(hashProviderSpec(nil)).To(Equal(hashProviderSpec([]byte{})))
}

func stableMachine() *machinev1.Machine {
	raw := []byte(`{"foo":"bar"}`)
	m := &machinev1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
			Annotations: map[string]string{
				lastReconciledProviderSpecHashAnnotation: hashProviderSpec(raw),
				lastFullReconcileTimestampAnnotation:     time.Now().Format(time.RFC3339),
			},
		},
		Spec: machinev1.MachineSpec{
			ProviderID: ptr.To("vsphere://12345"),
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{Raw: raw},
			},
		},
		Status: machinev1.MachineStatus{
			Phase:   ptr.To(machinev1.PhaseRunning),
			NodeRef: &corev1.ObjectReference{Name: "test-node"},
			Addresses: []corev1.NodeAddress{
				{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
			},
		},
	}
	return m
}

func TestCanSkipFullReconcile(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(m *machinev1.Machine)
		expected bool
	}{
		{
			name:     "fully stable machine",
			mutate:   func(m *machinev1.Machine) {},
			expected: true,
		},
		{
			name: "missing phase",
			mutate: func(m *machinev1.Machine) {
				m.Status.Phase = nil
			},
			expected: false,
		},
		{
			name: "wrong phase",
			mutate: func(m *machinev1.Machine) {
				m.Status.Phase = ptr.To(machinev1.PhaseProvisioned)
			},
			expected: false,
		},
		{
			name: "missing providerID",
			mutate: func(m *machinev1.Machine) {
				m.Spec.ProviderID = nil
			},
			expected: false,
		},
		{
			name: "empty providerID",
			mutate: func(m *machinev1.Machine) {
				m.Spec.ProviderID = ptr.To("")
			},
			expected: false,
		},
		{
			name: "missing nodeRef",
			mutate: func(m *machinev1.Machine) {
				m.Status.NodeRef = nil
			},
			expected: false,
		},
		{
			name: "missing addresses",
			mutate: func(m *machinev1.Machine) {
				m.Status.Addresses = nil
			},
			expected: false,
		},
		{
			name: "deleting",
			mutate: func(m *machinev1.Machine) {
				now := metav1.Now()
				m.DeletionTimestamp = &now
			},
			expected: false,
		},
		{
			name: "nil provider spec value",
			mutate: func(m *machinev1.Machine) {
				m.Spec.ProviderSpec.Value = nil
			},
			expected: false,
		},
		{
			name: "missing annotations (upgrade path)",
			mutate: func(m *machinev1.Machine) {
				m.Annotations = nil
			},
			expected: false,
		},
		{
			name: "missing hash annotation only",
			mutate: func(m *machinev1.Machine) {
				delete(m.Annotations, lastReconciledProviderSpecHashAnnotation)
			},
			expected: false,
		},
		{
			name: "missing timestamp annotation only",
			mutate: func(m *machinev1.Machine) {
				delete(m.Annotations, lastFullReconcileTimestampAnnotation)
			},
			expected: false,
		},
		{
			name: "hash mismatch",
			mutate: func(m *machinev1.Machine) {
				m.Annotations[lastReconciledProviderSpecHashAnnotation] = "deadbeefdeadbeef"
			},
			expected: false,
		},
		{
			name:     "hash match",
			mutate:   func(m *machinev1.Machine) {},
			expected: true,
		},
		{
			name: "expired timestamp",
			mutate: func(m *machinev1.Machine) {
				m.Annotations[lastFullReconcileTimestampAnnotation] = time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
			},
			expected: false,
		},
		{
			name: "timestamp just under TTL",
			mutate: func(m *machinev1.Machine) {
				m.Annotations[lastFullReconcileTimestampAnnotation] = time.Now().Add(-59 * time.Minute).Format(time.RFC3339)
			},
			expected: true,
		},
		{
			name: "unparseable timestamp",
			mutate: func(m *machinev1.Machine) {
				m.Annotations[lastFullReconcileTimestampAnnotation] = "not-a-timestamp"
			},
			expected: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			m := stableMachine()
			tc.mutate(m)
			g.Expect(canSkipFullReconcile(m)).To(Equal(tc.expected))
		})
	}
}

// TestActuatorShortCircuitsStableMachine verifies that a stable, Running machine causes
// Exists() and Update() to return immediately without touching the Actuator's client,
// apiReader, or event recorder (i.e. without making any vCenter or Kubernetes API calls).
// A bare Actuator with nil fields is used deliberately: if the short-circuit did not fire,
// any attempt to use those nil fields would panic.
func TestActuatorShortCircuitsStableMachine(t *testing.T) {
	g := NewWithT(t)

	a := &Actuator{}
	m := stableMachine()

	exists, err := a.Exists(t.Context(), m)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(exists).To(BeTrue())

	g.Expect(a.Update(t.Context(), m)).To(Succeed())
}

// TestActuatorUpgradePathDoesNotShortCircuit verifies that a machine missing the
// short-circuit annotations (e.g. right after an upgrade from a version that didn't set
// them) does not short-circuit, since canSkipFullReconcile requires them to be present.
func TestActuatorUpgradePathDoesNotShortCircuit(t *testing.T) {
	g := NewWithT(t)

	m := stableMachine()
	m.Annotations = nil

	g.Expect(canSkipFullReconcile(m)).To(BeFalse())
}

func TestMarkFullReconcileComplete(t *testing.T) {
	g := NewWithT(t)

	raw := []byte(`{"foo":"bar"}`)
	m := &machinev1.Machine{
		Spec: machinev1.MachineSpec{
			ProviderSpec: machinev1.ProviderSpec{
				Value: &runtime.RawExtension{Raw: raw},
			},
		},
	}

	before := time.Now()
	markFullReconcileComplete(m)
	after := time.Now()

	g.Expect(m.Annotations).To(HaveKeyWithValue(lastReconciledProviderSpecHashAnnotation, hashProviderSpec(raw)))
	g.Expect(m.Annotations).To(HaveKey(lastFullReconcileTimestampAnnotation))

	parsed, err := time.Parse(time.RFC3339, m.Annotations[lastFullReconcileTimestampAnnotation])
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(parsed).To(BeTemporally(">=", before.Truncate(time.Second)))
	g.Expect(parsed).To(BeTemporally("<=", after.Add(time.Second)))
}
