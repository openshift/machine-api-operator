## MachineWithoutValidNode
One or more machines does not have a valid node reference.  This condition has
persisted for 10 minutes or longer.

### Query
```
# for: 10m
(mapi_machine_created_timestamp_seconds unless on(node) kube_node_info) > 0
```

### Possible Causes
* A node for this machine never joined the cluster, it is possible the machine failed to boot
* The node was deleted from the cluster via the api, but the machine still exists

### Resolution
If the machine never became a node, consult the machine troubleshooting guide.
If the node was deleted from the api, you may choose to delete the machine object, if appropriate.  (FYI, The machine-api will automatically delete nodes, there is no need to delete node objects directly)

## MachineWithNoRunningPhase
Machine did not reach the “Running” Phase.  Running phase is when the machine has successfully become a node and joined the cluster.

### Query
```
# for: 10m
(mapi_machine_created_timestamp_seconds{phase!="Running|Deleting"}) > 0
```

### Possible Causes
* The machine was not properly provisioned in the cloud provider due to machine misconfiguration, invalid credentials, or lack of cloud capacity
* The machine took longer than two hours to join the cluster and the bootstrap CSRs were not approved (due to networking or cloud quota/capacity constraints)
* Unusual hostname presented by the kubelet on the bootstrap CSR is preventing CSR approval

### Resolution
If the machine never became a node, consult the machine troubleshooting guide.

## MachineNotYetDeleted
Machine has been in the "Deleting" phase for a long time. Deleting phase is added to a machine when it has been marked for deletion and given a deletion timestamp in etcd.

### Query
```
# for: 360m
(mapi_machine_created_timestamp_seconds{phase="Deleting"}) > 0
```

### Possible Causes
* Invalid cloud credentials are preventing deletion.
* A [Pod disruption budget](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#pod-disruption-budgets) is
  preventing Node removal.
* A Pod with a very long [graceful termination period](https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#graceful-termination-of-preemption-victims) is preventing Node removal.

### Resolution
Consult the `machine-controller`'s logs for root causes (see the [Troubleshooting Guide](TroubleShooting.md). In some
cases the machine may need to be removed manaually, starting with the instance in the cloud provider's console and
then the machine in OpenShift.

## MachineAPIOperatorMetricsCollectionFailing
Machine-api metrics are not being collected successfully.  This would be a very unusual error to see.

### Query
```
# for: 5m
mapi_mao_collector_up == 0
```

### Possible Causes
* Machine-api-operator is unable to list machines or machinesets to gather metrics
* Prometheus is not able to gather metrics from the MAo for 5 minutes or more
due to either network issue or missing service definition.

### Resolution
Investigate the logs of the machine-api-operator to determine why it is unable to gather machines and machinesets, or investigate the collection of metrics.

## MachineHealthCheckUnterminatedShortCircuit
A MachineHealthCheck has been in short circuit for an extended period of time
and is no longer remediating unhealthy machines.

### Query
```
# for: 30m
mapi_machinehealthcheck_short_circuit == 1
```

### Possible Causes
* The number of unhealthy machines has exceeded the `maxUnhealthy` limit for the check

### Resolution
Check to ensure that the `maxUnhealthy` field on the MachineHealthCheck is not set too low.
In some cases a low value for `maxUnhealthy` will mean that the MachineHealthCheck will enter
short-circuit if only a few nodes are unhealthy. Setting this value will be different for
every cluster's and user's needs, but in general you should consider the size of your cluster
and the maximum number of machines which can unhealthy before the MachineHealthCheck will
stop attempting remediation. You might consider setting this value to a percentage (eg `50%`)
to ensure that the MachineHealthCheck will continue to perform as expected as your cluster
grows.

If the `maxUnhealthy` value looks acceptable, the next step is to inspect the
unhealthy machines and remediate them manually if possible. This can usually be achieved
by deleting the machines in question and allowing the Machine API to recreate them.
