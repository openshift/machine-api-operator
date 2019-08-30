package nodelink

import (
	"context"
	"fmt"
	"reflect"

	mapiv1beta1 "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	machineAnnotationKey   = "machine.openshift.io/machine"
	machineInternalIPIndex = "machineInternalIPIndex"
	machineProviderIDIndex = "machineProviderIDIndex"
	nodeInternalIPIndex    = "nodeInternalIPIndex"
	nodeProviderIDIndex    = "nodeProviderIDIndex"
)

// blank assignment to verify that ReconcileNodeLink implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileNodeLink{}

// ReconcileNodeLink reconciles a Node object
type ReconcileNodeLink struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	// This is useful for unit testing so we can mock cache IndexField
	// and emulate Client.List.MatchingField behaviour
	listNodesByFieldFunc    func(key, value string) ([]corev1.Node, error)
	listMachinesByFieldFunc func(key, value string) ([]mapiv1beta1.Machine, error)
	nodeReadinessCache      map[string]bool
}

// Add creates a new Nodelink Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager, opts manager.Options) error {
	reconciler, err := newReconciler(mgr)
	if err != nil {
		return fmt.Errorf("error building reconciler: %v", err)
	}
	return add(mgr, reconciler, reconciler.nodeRequestFromMachine)
}

func indexNodeByProviderID(object runtime.Object) []string {
	if node, ok := object.(*corev1.Node); ok {
		if node.Spec.ProviderID != "" {
			klog.V(3).Infof("Adding providerID %q for node %q to indexer", node.Spec.ProviderID, node.GetName())
			return []string{node.Spec.ProviderID}
		}
		return nil
	}
	klog.Warningf("Expected a node for indexing field, got: %T", object)
	return nil
}

func indexMachineByProvider(object runtime.Object) []string {
	if machine, ok := object.(*mapiv1beta1.Machine); ok {
		if machine.Spec.ProviderID != nil {
			if *machine.Spec.ProviderID != "" {
				klog.V(3).Infof("Adding providerID %q for machine %q to indexer", *machine.Spec.ProviderID, machine.GetName())
				return []string{*machine.Spec.ProviderID}
			}
		}
		return nil
	}
	klog.Warningf("Expected a machine for indexing field, got: %T", object)
	return nil
}

func indexNodeByInternalIP(object runtime.Object) []string {
	node, ok := object.(*corev1.Node)
	if !ok {
		klog.Warningf("expected a node for indexing field, got: %T", object)
		return nil
	}

	var keys []string
	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			keys = append(keys, a.Address)
			klog.V(3).Infof("Adding internal IP %q for node %q to indexer", a.Address, node.GetName())
		}
	}

	return keys
}

func indexMachineByInternalIP(object runtime.Object) []string {
	machine, ok := object.(*mapiv1beta1.Machine)
	if !ok {
		klog.Warningf("Expected a machine for indexing field, got: %T", object)
		return nil
	}

	var keys []string
	for _, a := range machine.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			keys = append(keys, a.Address)
			klog.V(3).Infof("Adding internal IP %q for machine %q to indexer", a.Address, machine.GetName())
		}
	}

	return keys
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) (*ReconcileNodeLink, error) {
	// set convenient indexers
	if err := mgr.GetCache().IndexField(&corev1.Node{},
		nodeProviderIDIndex,
		indexNodeByProviderID,
	); err != nil {
		return nil, fmt.Errorf("error setting index fields: %v", err)
	}

	if err := mgr.GetCache().IndexField(&mapiv1beta1.Machine{},
		machineProviderIDIndex,
		indexMachineByProvider,
	); err != nil {
		return nil, fmt.Errorf("error setting index fields: %v", err)
	}

	if err := mgr.GetCache().IndexField(&corev1.Node{},
		nodeInternalIPIndex,
		indexNodeByInternalIP,
	); err != nil {
		return nil, fmt.Errorf("error setting index fields: %v", err)
	}

	if err := mgr.GetCache().IndexField(&mapiv1beta1.Machine{},
		machineInternalIPIndex,
		indexMachineByInternalIP,
	); err != nil {
		return nil, fmt.Errorf("error setting index fields: %v", err)
	}

	r := ReconcileNodeLink{
		client: mgr.GetClient(),
	}
	r.nodeReadinessCache = make(map[string]bool)

	// This is useful for unit testing so we can mock cache IndexField
	// and emulate Client.List.MatchingField behaviour
	r.listNodesByFieldFunc = r.listNodesByField
	r.listMachinesByFieldFunc = r.listMachinesByField
	return &r, nil
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler, mapFn handler.ToRequestsFunc) error {
	// Create a new controller
	c, err := controller.New("nodelink-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	//Watch for changes to Node
	err = c.Watch(&source.Kind{Type: &corev1.Node{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to Machines and enqueue if it exists the backed node
	err = c.Watch(&source.Kind{Type: &mapiv1beta1.Machine{}}, &handler.EnqueueRequestsFromMapFunc{ToRequests: mapFn})
	if err != nil {
		return err
	}

	return nil
}

// Reconcile reads that state of the cluster for a Node object and makes changes based on the state read
// and what is in the Node.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileNodeLink) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	klog.Infof("Reconciling Node %v", request)

	// Fetch the Node instance
	node := &corev1.Node{}
	err := r.client.Get(context.TODO(), request.NamespacedName, node)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		klog.Errorf("Error getting node: %v", err)
		return reconcile.Result{}, fmt.Errorf("error getting node: %v", err)
	}

	machine, err := r.findMachineFromNode(node)
	if err != nil {
		klog.Errorf("Failed to find machine from node %q: %v", node.GetName(), err)
		return reconcile.Result{}, fmt.Errorf("failed to find machine from node %q: %v", node.GetName(), err)
	}

	if machine == nil {
		klog.Warningf("Machine for node %q not found", node.GetName())
		return reconcile.Result{}, nil
	}

	if err := r.updateNodeRef(machine, node); err != nil {
		return reconcile.Result{}, fmt.Errorf("error updating nodeRef for machine %q and node %q: %v", machine.GetName(), node.GetName(), err)
	}

	if !node.DeletionTimestamp.IsZero() {
		klog.Infof("Node %q is being deleted", node.GetName())
		return reconcile.Result{}, nil
	}

	modNode := node.DeepCopy()
	if modNode.Annotations == nil {
		modNode.Annotations = map[string]string{}
	}
	modNode.Annotations[machineAnnotationKey] = fmt.Sprintf("%s/%s", machine.GetNamespace(), machine.GetName())

	if modNode.Labels == nil {
		modNode.Labels = map[string]string{}
	}

	for k, v := range machine.Spec.Labels {
		klog.V(4).Infof("Copying label %s = %s", k, v)
		modNode.Labels[k] = v
	}

	addTaintsToNode(modNode, machine)

	if !reflect.DeepEqual(node, modNode) {
		klog.V(3).Infof("Node %q has changed, updating", modNode.GetName())
		if err := r.client.Update(context.Background(), modNode); err != nil {
			return reconcile.Result{}, fmt.Errorf("error updating node: %v", err)
		}
	}

	return reconcile.Result{}, nil
}

// updateNodeRef set the given node as nodeRef in the machine status
func (r *ReconcileNodeLink) updateNodeRef(machine *mapiv1beta1.Machine, node *corev1.Node) error {
	now := metav1.Now()
	machine.Status.LastUpdated = &now

	if !node.DeletionTimestamp.IsZero() {
		machine.Status.NodeRef = nil
		if err := r.client.Status().Update(context.Background(), machine); err != nil {
			return fmt.Errorf("error updating machine %q: %v", machine.GetName(), err)
		}
		delete(r.nodeReadinessCache, node.GetName())
		return nil
	}

	nodeReady := isNodeReady(node)
	// skip update if cached and no change in readiness.
	if cachedReady, ok := r.nodeReadinessCache[node.GetName()]; ok &&
		cachedReady == nodeReady {
		return nil
	}

	// if the nodeReadiness has changed the machine is updated so
	// watchers can take action, e.g machine controller
	machine.Status.NodeRef = &corev1.ObjectReference{
		Kind: "Node",
		Name: node.GetName(),
		UID:  node.GetUID(),
	}
	if err := r.client.Status().Update(context.Background(), machine); err != nil {
		return fmt.Errorf("error updating machine %q: %v", machine.GetName(), err)
	}
	r.nodeReadinessCache[node.GetName()] = nodeReady

	klog.Infof("Successfully updated nodeRef for machine %q and node %q", machine.GetName(), node.GetName())
	return nil
}

// nodeRequestFromMachine returns a reconcile.request for the node backed by the received machine
func (r *ReconcileNodeLink) nodeRequestFromMachine(o handler.MapObject) []reconcile.Request {
	klog.V(3).Infof("Watched machine event, finding node to reconcile.Request")
	// get machine
	machine := &mapiv1beta1.Machine{}
	if err := r.client.Get(
		context.Background(),
		client.ObjectKey{
			Namespace: o.Meta.GetNamespace(),
			Name:      o.Meta.GetName(),
		},
		machine,
	); err != nil {
		klog.Errorf("No-op: Unable to retrieve machine %s/%s from store: %v", o.Meta.GetNamespace(), o.Meta.GetName(), err)
		return []reconcile.Request{}
	}

	if machine.DeletionTimestamp != nil {
		klog.V(3).Infof("No-op: Machine %q has a deletion timestamp", o.Meta.GetName())
		return []reconcile.Request{}
	}

	// find node
	node, err := r.findNodeFromMachine(machine)
	if err != nil {
		klog.Errorf("No-op: Failed to find node for machine %q: %v", machine.GetName(), err)
		return []reconcile.Request{}
	}
	if node != nil {
		return []reconcile.Request{
			{
				NamespacedName: client.ObjectKey{
					Namespace: node.GetNamespace(),
					Name:      node.GetName(),
				},
			},
		}
	}

	klog.V(3).Infof("No-op: Node for machine %q not found", machine.GetName())
	return []reconcile.Request{}
}

// findNodeFromMachine find a node from by providerID and fallback to find by IP
func (r *ReconcileNodeLink) findNodeFromMachine(machine *mapiv1beta1.Machine) (*corev1.Node, error) {
	klog.V(3).Infof("Finding node from machine %q", machine.GetName())
	node, err := r.findNodeFromMachineByProviderID(machine)
	if err != nil {
		return nil, fmt.Errorf("failed to find node from machine %q by ProviderID: %v", machine.GetName(), err)
	}
	if node != nil {
		return node, nil
	}

	node, err = r.findNodeFromMachineByIP(machine)
	if err != nil {
		return nil, fmt.Errorf("failed to find node from machine %q by internal IP: %v", machine.GetName(), err)
	}
	return node, nil
}

func (r *ReconcileNodeLink) findNodeFromMachineByProviderID(machine *mapiv1beta1.Machine) (*corev1.Node, error) {
	klog.V(3).Infof("Finding node from machine %q by providerID", machine.GetName())
	if machine.Spec.ProviderID == nil {
		klog.Warningf("Machine %q has no providerID", machine.GetName())
		return nil, nil
	}

	nodes, err := r.listNodesByFieldFunc(nodeProviderIDIndex, *machine.Spec.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed getting node list: %v", err)
	}

	if len(nodes) > 1 {
		return nil, fmt.Errorf("failed getting node: expected 1 node, got %v", len(nodes))
	}

	if len(nodes) == 1 {
		klog.V(3).Infof("Found node %q for machine %q with providerID %q", nodes[0].GetName(), machine.GetName(), nodes[0].Spec.ProviderID)
		return nodes[0].DeepCopy(), nil
	}

	return nil, nil
}

func (r *ReconcileNodeLink) findNodeFromMachineByIP(machine *mapiv1beta1.Machine) (*corev1.Node, error) {
	klog.V(3).Infof("Finding node from machine %q by IP", machine.GetName())
	var machineInternalAddress string
	for _, a := range machine.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			machineInternalAddress = a.Address
			klog.V(3).Infof("Found internal IP for machine %q: %q", machine.GetName(), machineInternalAddress)
			break
		}
	}

	if machineInternalAddress == "" {
		klog.Warningf("not found internal IP for machine %q", machine.GetName())
		return nil, nil
	}

	nodes, err := r.listNodesByFieldFunc(nodeInternalIPIndex, machineInternalAddress)
	if err != nil {
		return nil, fmt.Errorf("failed getting node list: %v", err)
	}

	if len(nodes) > 1 {
		return nil, fmt.Errorf("failed getting node: expected 1 node, got %v", len(nodes))
	}

	if len(nodes) == 1 {
		klog.V(3).Infof("Found node %q for machine %q with internal IP %q", nodes[0].GetName(), machine.GetName(), machineInternalAddress)
		return nodes[0].DeepCopy(), nil
	}

	klog.V(3).Infof("Matching node not found for machine %q with internal IP %q", machine.GetName(), machineInternalAddress)
	return nil, nil
}

func (r *ReconcileNodeLink) findMachineFromNode(node *corev1.Node) (*mapiv1beta1.Machine, error) {
	klog.V(3).Infof("Finding machine from node %q", node.GetName())
	machine, err := r.findMachineFromNodeByProviderID(node)
	if err != nil {
		return nil, fmt.Errorf("failed to find machine from node %q by ProviderID: %v", node.GetName(), err)
	}
	if machine != nil {
		return machine, nil
	}

	machine, err = r.findMachineFromNodeByIP(node)
	if err != nil {
		return nil, fmt.Errorf("failed to find machine from node %q by internal IP: %v", node.GetName(), err)
	}
	return machine, nil
}

func (r *ReconcileNodeLink) findMachineFromNodeByProviderID(node *corev1.Node) (*mapiv1beta1.Machine, error) {
	klog.V(3).Infof("Finding machine from node %q by ProviderID", node.GetName())
	if node.Spec.ProviderID == "" {
		klog.Warningf("Node %q has no providerID", node.GetName())
		return nil, nil
	}

	machines, err := r.listMachinesByFieldFunc(machineProviderIDIndex, node.Spec.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("failed getting node list: %v", err)
	}

	if len(machines) > 1 {
		return nil, fmt.Errorf("failed getting machine: expected 1 machine, got %v", len(machines))
	}

	if len(machines) > 0 {
		klog.V(3).Infof("Found machine %q for node %q with providerID %q", machines[0].GetName(), node.GetName(), node.Spec.ProviderID)
		return machines[0].DeepCopy(), nil
	}
	return nil, nil
}

func (r *ReconcileNodeLink) findMachineFromNodeByIP(node *corev1.Node) (*mapiv1beta1.Machine, error) {
	klog.V(3).Infof("Finding machine from node %q by IP", node.GetName())
	var nodeInternalAddress string
	for _, a := range node.Status.Addresses {
		if a.Type == corev1.NodeInternalIP {
			nodeInternalAddress = a.Address
			klog.V(3).Infof("Found internal IP for node %q: %q", node.GetName(), nodeInternalAddress)
			break
		}
	}

	if nodeInternalAddress == "" {
		klog.Warningf("Node %q has no internal IP", node.GetName())
		return nil, nil
	}

	machines, err := r.listMachinesByFieldFunc(machineInternalIPIndex, nodeInternalAddress)
	if err != nil {
		return nil, fmt.Errorf("failed getting node list: %v", err)
	}

	if len(machines) > 1 {
		return nil, fmt.Errorf("failed getting machine: expected 1 machine, got %v", len(machines))
	}

	if len(machines) == 1 {
		klog.V(3).Infof("Found machine %q for node %q with internal IP %q", machines[0].GetName(), node.GetName(), nodeInternalAddress)
		return machines[0].DeepCopy(), nil
	}

	klog.V(3).Infof("Matching machine not found for node %q with internal IP %q", node.GetName(), nodeInternalAddress)
	return nil, nil
}

// addTaintsToNode adds taints from machine object to the node object
// Taints are to be an authoritative list on the machine spec per cluster-api comments.
// However, we believe many components can directly taint a node and there is no direct source of truth that should enforce a single writer of taints
func addTaintsToNode(node *corev1.Node, machine *mapiv1beta1.Machine) {
	for _, mTaint := range machine.Spec.Taints {
		klog.V(4).Infof("Adding taint %v from machine %q to node %q", mTaint, machine.GetName(), node.GetName())
		alreadyPresent := false
		for _, nTaint := range node.Spec.Taints {
			if nTaint.Key == mTaint.Key && nTaint.Effect == mTaint.Effect {
				klog.V(4).Infof("Skipping to add machine taint, %v, to the node. Node already has a taint with same key and effect", mTaint)
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			node.Spec.Taints = append(node.Spec.Taints, mTaint)
		}
	}
}

func (r *ReconcileNodeLink) listNodesByField(key, value string) ([]corev1.Node, error) {
	nodeList := &corev1.NodeList{}
	if err := r.client.List(
		context.TODO(),
		nodeList,
		client.MatchingField(key, value),
	); err != nil {
		return nil, fmt.Errorf("failed getting node list: %v", err)
	}
	return nodeList.Items, nil
}

func (r *ReconcileNodeLink) listMachinesByField(key, value string) ([]mapiv1beta1.Machine, error) {
	machineList := &mapiv1beta1.MachineList{}
	if err := r.client.List(
		context.TODO(),
		machineList,
		client.MatchingField(key, value),
	); err != nil {
		return nil, fmt.Errorf("failed getting node list: %v", err)
	}
	return machineList.Items, nil
}

// isNodeReady returns true if a node is ready; false otherwise.
func isNodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}
