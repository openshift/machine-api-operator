# Nodelink Controller

The nodelink controller is one component of the
[Machine API](machine-api-overview.md). It is reponsible for watching `Node`
objects in the cluster and ensuring that related `Machine` objects contain a
valid node reference.

**Example Machine (truncated)**
```yaml
apiVersion: machine.openshift.io/v1beta1
kind: Machine
metadata:
  name: alpha-b6dhr-worker-us-east-2a-jxqng
  namespace: openshift-machine-api
status:
  nodeRef:
    kind: Node
    name: ip-10-0-145-184.us-east-2.compute.internal
    uid: e689b5e9-6c23-4b74-b23d-69432f81b230
```

**Example Node (truncated)**
```yaml
apiVersion: v1
kind: Node
metadata:
  annotations:
      machine.openshift.io/machine: openshift-machine-api/alpha-b6dhr-worker-us-east-2a-jxqng
  name: ip-10-0-145-184.us-east-2.compute.internal
  uid: e689b5e9-6c23-4b74-b23d-69432f81b230
```

In short the nodelink controller does the following:
1. Reconcile on node objects
2. If the node is not being deleted (does not have a deletion timestamp),
   attempt to find the related machine object by using the provider ID
   (`.spec.providerID`) or the IP address (`.status.addresses`).
3. If the machine is found, update its node reference (`.status.nodeRef`)
   with the name and UID of the associated node.
4. Add the `machine.openshift.io/machine` annotation to the node, with
   the value of `{machine name}/{machine namespace}`.
5. Copy the labels from the machine spec (`.spec.labels`) to the node.
6. Copy the taints from the machine spec (`.spec.taints`) to the node.

Additionally
1. Reconcile on machine objects
2. Attempt to find the node associated with the machine
3. If found, queue a reconcile event for that node to engage the behavior
   listed above.

## Troubleshooting

The most common errors to see from the nodelink controller are when the `Node`
or `Machine` objects have been deleted.

See the [troubleshooting guide](TroubleShooting.md) for more information.
