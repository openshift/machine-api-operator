package nodelink

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	mapiv1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func init() {
	if err := mapiv1beta1.AddToScheme(scheme.Scheme); err != nil {
		klog.Fatal(err)
	}
}

const (
	ownerControllerKind = "MachineSet"
	namespace           = "openshift-machine-api"
)

var (
	knownDate = metav1.Time{Time: time.Date(1985, 06, 03, 0, 0, 0, 0, time.Local)}
)

func node(name, providerID string, addresses []corev1.NodeAddress, taints []corev1.Taint) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceNone,
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Node",
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: knownDate,
				},
			},
		},
	}

	if providerID != "" {
		node.Spec.ProviderID = providerID
	}
	if addresses != nil {
		node.Status.Addresses = addresses
	}
	if taints != nil {
		node.Spec.Taints = taints
	}
	return node
}

func machine(name, providerID string, addresses []corev1.NodeAddress, taints []corev1.Taint) *mapiv1beta1.Machine {
	machine := &mapiv1beta1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"foo": "a",
				"bar": "b",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: ownerControllerKind,
				},
			},
		},
		TypeMeta: metav1.TypeMeta{
			Kind: "Machine",
		},
		Spec: mapiv1beta1.MachineSpec{},
	}

	if providerID != "" {
		machine.Spec.ProviderID = &providerID
	}
	if addresses != nil {
		machine.Status.Addresses = addresses
	}
	if taints != nil {
		machine.Spec.Taints = taints
	}
	return machine
}

type fakeReconciler struct {
	ReconcileNodeLink
	fakeNodeIndexer    map[string]corev1.Node
	fakeMachineIndexer map[string]mapiv1beta1.Machine
}

func newFakeReconciler(client client.Client, machine *mapiv1beta1.Machine, node *corev1.Node) fakeReconciler {
	r := fakeReconciler{
		ReconcileNodeLink: ReconcileNodeLink{
			client: client,
		},
		fakeNodeIndexer:    make(map[string]corev1.Node),
		fakeMachineIndexer: make(map[string]mapiv1beta1.Machine),
	}
	r.listNodesByFieldFunc = func(_, value string) ([]corev1.Node, error) {
		_, ok := r.fakeNodeIndexer[value]
		if ok {
			return []corev1.Node{r.fakeNodeIndexer[value]}, nil
		}
		return nil, nil
	}
	r.listMachinesByFieldFunc = func(_, value string) ([]mapiv1beta1.Machine, error) {
		_, ok := r.fakeMachineIndexer[value]
		if ok {
			return []mapiv1beta1.Machine{r.fakeMachineIndexer[value]}, nil
		}
		return nil, nil
	}
	r.buildFakeNodeIndexer(*node)
	r.buildFakeMachineIndexer(*machine)

	return r
}

func (r *fakeReconciler) buildFakeNodeIndexer(nodes ...corev1.Node) {
	for i := range nodes {
		if nodes[i].Spec.ProviderID != "" {
			r.fakeNodeIndexer[nodes[i].Spec.ProviderID] = nodes[i]
		}
		for j := range nodes[i].Status.Addresses {
			r.fakeNodeIndexer[nodes[i].Status.Addresses[j].Address] = nodes[i]
		}
	}
}

func (r *fakeReconciler) buildFakeMachineIndexer(machines ...mapiv1beta1.Machine) {
	for i := range machines {
		if machines[i].Spec.ProviderID != nil {
			r.fakeMachineIndexer[*machines[i].Spec.ProviderID] = machines[i]
		}
		for j := range machines[i].Status.Addresses {
			r.fakeMachineIndexer[machines[i].Status.Addresses[j].Address] = machines[i]
		}
	}
}

func TestFindMachineFromNodeByProviderID(t *testing.T) {
	testCases := []struct {
		machine  *mapiv1beta1.Machine
		node     *corev1.Node
		expected *mapiv1beta1.Machine
	}{
		{
			machine:  machine("noProviderID", "", nil, nil),
			node:     node("noProviderID", "", nil, nil),
			expected: nil,
		},
		{
			machine:  machine("matchingProviderID", "test", nil, nil),
			node:     node("matchingProviderID", "test", nil, nil),
			expected: machine("matchingProviderID", "test", nil, nil),
		},
		{
			machine:  machine("noMatchingProviderID", "providerID", nil, nil),
			node:     node("noMatchingProviderID", "differentProviderID", nil, nil),
			expected: nil,
		},
	}
	for _, tc := range testCases {
		r := newFakeReconciler(fake.NewFakeClientWithScheme(scheme.Scheme, tc.machine), tc.machine, tc.node)

		machine, err := r.findMachineFromNodeByProviderID(tc.node)
		if err != nil {
			t.Errorf("unexpected error finding machine from node by providerID: %v", err)
		}
		if !reflect.DeepEqual(machine, tc.expected) {
			t.Errorf("expected %v, got: %v", tc.expected, machine)
		}

	}
}

func TestFindMachineFromNodeByIP(t *testing.T) {
	testCases := []struct {
		machine  *mapiv1beta1.Machine
		node     *corev1.Node
		expected *mapiv1beta1.Machine
	}{
		{
			machine:  machine("noInternalIP", "", nil, nil),
			node:     node("noInternalIP", "", nil, nil),
			expected: nil,
		},
		{
			machine: machine("matchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "matchingInternalIP",
				},
			}, nil),
			node: node("matchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "matchingInternalIP",
				},
			}, nil),
			expected: machine("matchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "matchingInternalIP",
				},
			}, nil),
		},
		{
			machine: machine("nonMatchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "one IP",
				},
			}, nil),
			node: node("nonMatchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "a different IP",
				},
			}, nil),
			expected: nil,
		},
	}
	for _, tc := range testCases {
		r := newFakeReconciler(fake.NewFakeClientWithScheme(scheme.Scheme, tc.machine), tc.machine, tc.node)
		machine, err := r.findMachineFromNodeByIP(tc.node)
		if err != nil {
			t.Errorf("unexpected error finding machine from node by IP: %v", err)
		}
		if !reflect.DeepEqual(machine, tc.expected) {
			t.Errorf("expected: %v, got: %v", tc.expected, machine)
		}

	}
}

func TestFindNodeFromMachineByProviderID(t *testing.T) {
	testCases := []struct {
		machine  *mapiv1beta1.Machine
		node     *corev1.Node
		expected *corev1.Node
	}{
		{
			machine:  machine("noProviderID", "", nil, nil),
			node:     node("noProviderID", "", nil, nil),
			expected: nil,
		},
		{
			machine:  machine("matchingProviderID", "test", nil, nil),
			node:     node("matchingProviderID", "test", nil, nil),
			expected: node("matchingProviderID", "test", nil, nil),
		},
		{
			machine:  machine("noMatchingProviderID", "providerID", nil, nil),
			node:     node("noMatchingProviderID", "differentProviderID", nil, nil),
			expected: nil,
		},
	}
	for _, tc := range testCases {
		r := newFakeReconciler(fake.NewFakeClientWithScheme(scheme.Scheme, tc.node), tc.machine, tc.node)

		node, err := r.findNodeFromMachineByProviderID(tc.machine)
		if err != nil {
			t.Errorf("unexpected error finding machine from node by providerID: %v", err)
		}

		if !reflect.DeepEqual(node, tc.expected) {
			t.Errorf("expected: %v, got: %v", tc.expected, node)
		}
	}
}

func TestFindNodeFromMachineByIP(t *testing.T) {
	testCases := []struct {
		machine  *mapiv1beta1.Machine
		node     *corev1.Node
		expected *corev1.Node
	}{
		{
			machine: machine("noInternalIP", "", nil, nil),
			node: node("anyInternalIP", "", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "internalIP",
				},
			}, nil),
			expected: nil,
		},
		{
			machine: machine("matchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "matchingInternalIP",
				},
			}, nil),
			node: node("matchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "matchingInternalIP",
				},
			}, nil),
			expected: node("matchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "matchingInternalIP",
				},
			}, nil),
		},
		{
			machine: machine("nonMatchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "one IP",
				},
			}, nil),
			node: node("nonMatchingInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "a different IP",
				},
			}, nil),
			expected: nil,
		},
	}
	for _, tc := range testCases {
		r := newFakeReconciler(fake.NewFakeClientWithScheme(scheme.Scheme, tc.node), tc.machine, tc.node)
		node, err := r.findNodeFromMachineByIP(tc.machine)
		if err != nil {
			t.Errorf("unexpected error finding node from machine by IP: %v", err)
		}
		if !reflect.DeepEqual(node, tc.expected) {
			t.Errorf("expected: %v, got: %v", tc.expected, node)
		}

	}
}

func TestAddTaintsToNode(t *testing.T) {
	testCases := []struct {
		description             string
		nodeTaints              []corev1.Taint
		machineTaints           []corev1.Taint
		expectedFinalNodeTaints []corev1.Taint
	}{
		{
			description:             "no previous taint on node. Machine adds none",
			nodeTaints:              []corev1.Taint{},
			machineTaints:           []corev1.Taint{},
			expectedFinalNodeTaints: []corev1.Taint{},
		},
		{
			description:             "no previous taint on node. Machine adds one",
			nodeTaints:              []corev1.Taint{},
			machineTaints:           []corev1.Taint{{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
			expectedFinalNodeTaints: []corev1.Taint{{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
		},
		{
			description:   "already taint on node. Machine adds another",
			nodeTaints:    []corev1.Taint{{Key: "key1", Value: "some-value", Effect: "Schedule"}},
			machineTaints: []corev1.Taint{{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
			expectedFinalNodeTaints: []corev1.Taint{{Key: "key1", Value: "some-value", Effect: "Schedule"},
				{Key: "dedicated", Value: "some-value", Effect: "NoSchedule"}},
		},
		{
			description:             "already taint on node. Machine adding same taint",
			nodeTaints:              []corev1.Taint{{Key: "key1", Value: "v1", Effect: "Schedule"}},
			machineTaints:           []corev1.Taint{{Key: "key1", Value: "v2", Effect: "Schedule"}},
			expectedFinalNodeTaints: []corev1.Taint{{Key: "key1", Value: "v1", Effect: "Schedule"}},
		},
	}

	for _, test := range testCases {
		machine := machine("", "", nil, test.machineTaints)
		node := node("", "", nil, test.nodeTaints)
		addTaintsToNode(node, machine)
		if !reflect.DeepEqual(node.Spec.Taints, test.expectedFinalNodeTaints) {
			t.Errorf("Test case: %s. Expected: %v, got: %v", test.description, test.expectedFinalNodeTaints, node.Spec.Taints)
		}
	}
}

func TestNodeRequestFromMachine(t *testing.T) {
	testCases := []struct {
		machine  *mapiv1beta1.Machine
		node     *corev1.Node
		expected []reconcile.Request
	}{
		{
			machine:  machine("noMatch", "", nil, nil),
			node:     node("noMatch", "", nil, nil),
			expected: []reconcile.Request{},
		},
		{
			machine: machine("matchProviderID", "match", nil, nil),
			node:    node("matchProviderID", "match", nil, nil),
			expected: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: metav1.NamespaceNone,
						Name:      "matchProviderID",
					},
				},
			},
		},
		{
			machine: machine("matchInternalIP", "", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "matchingInternalIP",
			}}, nil),
			node: node("matchInternalIP", "", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "matchingInternalIP",
			}}, nil),
			expected: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: metav1.NamespaceNone,
						Name:      "matchInternalIP",
					},
				},
			},
		},
		{
			machine: machine("matchInternalIPAndProviderID", "match", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "matchingInternalIP",
			}}, nil),
			node: node("matchInternalIPAndProviderID", "match", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "matchingInternalIP",
			}}, nil),
			expected: []reconcile.Request{
				{
					NamespacedName: client.ObjectKey{
						Namespace: metav1.NamespaceNone,
						Name:      "matchInternalIPAndProviderID",
					},
				},
			},
		},
		{
			machine: machine("NonMatchInternalIPNorProviderID", "one providerID", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "one IP",
			}}, nil),
			node: node("NonMatchInternalIPNorProviderID", "different providerID", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "different IP",
			}}, nil),
			expected: []reconcile.Request{},
		},
	}

	for _, tc := range testCases {
		o := handler.MapObject{
			Meta:   tc.machine.GetObjectMeta(),
			Object: tc.machine,
		}
		r := newFakeReconciler(fake.NewFakeClientWithScheme(scheme.Scheme, tc.machine), tc.machine, tc.node)
		got := r.nodeRequestFromMachine(o)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("expected: %v, got: %v", tc.expected, got)
		}

	}
}

func TestReconcile(t *testing.T) {
	testCases := []struct {
		machine            *mapiv1beta1.Machine
		node               *corev1.Node
		expected           reconcile.Result
		expectedError      bool
		expectedNodeUpdate bool
	}{
		{
			machine:            machine("noMatch", "", nil, nil),
			node:               node("noMatch", "", nil, nil),
			expected:           reconcile.Result{},
			expectedError:      false,
			expectedNodeUpdate: false,
		},
		{
			machine:            machine("matchingProvideID", "match", nil, nil),
			node:               node("matchingProvideID", "match", nil, nil),
			expected:           reconcile.Result{},
			expectedError:      false,
			expectedNodeUpdate: true,
		},
		{
			machine: machine("matchInternalIP", "", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "matchingInternalIP",
			}}, nil),
			node: node("matchInternalIP", "", []corev1.NodeAddress{{
				Type:    corev1.NodeInternalIP,
				Address: "matchingInternalIP",
			}}, nil),
			expected:           reconcile.Result{},
			expectedError:      false,
			expectedNodeUpdate: true,
		},
	}

	for _, tc := range testCases {
		r := newFakeReconciler(fake.NewFakeClientWithScheme(scheme.Scheme, tc.node), tc.machine, tc.node)
		request := reconcile.Request{
			NamespacedName: client.ObjectKey{
				Namespace: metav1.NamespaceNone,
				Name:      tc.node.Name,
			},
		}

		got, err := r.Reconcile(request)
		if got != tc.expected {
			t.Errorf("expected %v, got: %v", tc.expected, got)
		}
		if (err != nil) != tc.expectedError {
			t.Errorf("expected %v, got: %v", tc.expectedError, err)
		}

		if tc.expectedNodeUpdate {
			freshNode := &corev1.Node{}
			if err := r.client.Get(
				context.TODO(),
				client.ObjectKey{
					Namespace: tc.node.GetNamespace(),
					Name:      tc.node.GetName(),
				},
				freshNode,
			); err != nil {
				t.Errorf("unexpected error getting node: %v", err)
			}

			nodeAnnotations := freshNode.GetAnnotations()
			got, ok := nodeAnnotations[machineAnnotationKey]
			if !ok {
				t.Errorf("expected node to have machine annotation")
			}
			expected := fmt.Sprintf("%s/%s", tc.machine.GetNamespace(), tc.machine.GetName())
			if got != expected {
				t.Errorf("expected: %v, got: %v", expected, got)
			}
		}
	}
}

func TestIndexNodeByProviderID(t *testing.T) {
	testCases := []struct {
		object   runtime.Object
		expected []string
	}{
		{
			object:   node("noProviderID", "", nil, nil),
			expected: nil,
		},
		{
			object:   machine("wrongObject", "test", nil, nil),
			expected: nil,
		},
		{
			object:   node("withProviderID", "test", nil, nil),
			expected: []string{"test"},
		},
	}

	for _, tc := range testCases {
		got := indexNodeByProviderID(tc.object)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Expected: %v, got: %v", tc.expected, got)
		}
	}
}

func TestIndexMachineByProvider(t *testing.T) {
	testCases := []struct {
		object   runtime.Object
		expected []string
	}{
		{
			object:   machine("noProviderID", "", nil, nil),
			expected: nil,
		},
		{
			object:   node("wrongObject", "test", nil, nil),
			expected: nil,
		},
		{
			object:   machine("withProviderID", "test", nil, nil),
			expected: []string{"test"},
		},
	}

	for _, tc := range testCases {
		got := indexMachineByProvider(tc.object)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Expected: %v, got: %v", tc.expected, got)
		}
	}
}

func TestIndexNodeByInternalIP(t *testing.T) {
	testCases := []struct {
		object   runtime.Object
		expected []string
	}{
		{
			object:   node("noInternalIP", "", nil, nil),
			expected: nil,
		},
		{
			object:   machine("wrongObject", "test", nil, nil),
			expected: nil,
		},
		{
			object: node("oneInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "ip",
				},
			}, nil),
			expected: []string{"ip"},
		},
		{
			object: node("multipleInternalIPs", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "ip1",
				},
				{
					Type:    corev1.NodeInternalIP,
					Address: "ip2",
				},
			}, nil),
			expected: []string{"ip1", "ip2"},
		},
	}

	for _, tc := range testCases {
		got := indexNodeByInternalIP(tc.object)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Expected: %v, got: %v", tc.expected, got)
		}
	}
}

func TestIndexMachineByInternalIP(t *testing.T) {
	testCases := []struct {
		object   runtime.Object
		expected []string
	}{
		{
			object:   machine("noInternalIP", "", nil, nil),
			expected: nil,
		},
		{
			object:   node("wrongObject", "test", nil, nil),
			expected: nil,
		},
		{
			object: machine("oneInternalIP", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "ip",
				},
			}, nil),
			expected: []string{"ip"},
		},
		{
			object: machine("multipleInternalIPs", "test", []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "ip1",
				},
				{
					Type:    corev1.NodeInternalIP,
					Address: "ip2",
				},
			}, nil),
			expected: []string{"ip1", "ip2"},
		},
	}

	for _, tc := range testCases {
		got := indexMachineByInternalIP(tc.object)
		if !reflect.DeepEqual(got, tc.expected) {
			t.Errorf("Expected: %v, got: %v", tc.expected, got)
		}
	}
}
