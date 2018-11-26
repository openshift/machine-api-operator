# ClusterOperator Custom Resource

The ClusterOperator is a custom resource object which holds the current state of an operator. This object is used by operators to convey their state to the rest of the cluster.

Ref: [godoc](https://godoc.org/github.com/openshift/api/config/v1#ClusterOperator) for more info on the ClusterOperator type.

## Why I want ClusterOperator Custom Resource in /manifests

ClusterVersionOperator sweeps the release payload and applies it to the cluster. So if your operator manages a critical component for the cluster and you want ClusterVersionOperator to wait for your operator to **complete** before it continues to apply other operators, you must include the ClusterOperator Custom Resource in [`/manifests`](operators.md#what-do-i-put-in-manifests).

## How should I include ClusterOperator Custom Resource in /manifests

### How ClusterVersionOperator handles ClusterOperator in release payload

When ClusterVersionOperator encounters a ClusterOperator Custom Resource,

- It uses the `.metadata.name` and `.metadata.namespace` to find the corresponding ClusterOperator instance in the cluster
- It then waits for the instance in the cluster until
  - `.status.version` in the live instance matches the `.status.version` from the release payload and
  - the live instance `.status.conditions` report available, not progressing and not failed
- It then continues to the next task.

**NOTE**: ClusterVersionOperator sweeps the manifests in the release payload in alphabetical order, therefore if the ClusterOperator Custom Resource exists before the deployment for the operator that is supposed to report the Custom Resource, ClusterVersionOperator will be stuck waiting and cannot proceed.

### What should be the contents of ClusterOperator Custom Resource in /manifests

There are 3 important things that need to be set in the ClusterOperator Custom Resource in /manifests for CVO to correctly handle it.

- `.metadata.namespace`: namespace for finding the live instance in cluster
- `.metadata.name`: name for finding the live instance in the namespace
- `.status.version`: this is the version that the operator is expected to report. ClusterVersionOperator only respects the `.status.conditions` from instance that reports `.status.version`

Example:

For a cluster operator `my-cluster-operator` applying version `1.0.0`, that is reporting its status using ClusterOperator instance `my-cluster-operator` in namespace `my-cluster-operator-namespace`.

The ClusterOperator Custom Resource in /manifests should look like,

```yaml
apiVersion: operatorstatus.openshift.io
kind: ClusterOperator
metadata:
  namespace: my-cluster-operator-namespace
  name: my-cluster-operator
status:
  version: 1.0.0
```

## What should an operator report with ClusterOperator Custom Resource

### Status

The operator should ensure that all the fields of `.status` in ClusterOperator are atomic changes. This means that all the fields in the `.status` are only valid together and do not partially represent the status of the operator.

### Version

The operator should report a version which indicates the components that it is applying to the cluster.

### Conditions

Refer [the godocs](https://godoc.org/github.com/openshift/api/config/v1#ClusterStatusConditionType) for conditions.
