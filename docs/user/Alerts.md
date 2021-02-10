- [MachineWithoutValidNode](#machinewithoutvalidnode)
  - [Query](#query)
  - [Possible Causes](#possible-causes)
  - [Resolution](#resolution)
- [MachineNotStarting](#machinenotstarting)
  - [Query](#query-1)
  - [Possible Causes](#possible-causes-1)
  - [Resolution](#resolution-1)
- [MachineDeletingTooLong](#machinedeletingtoolong)
  - [Query](#query-2)
  - [Possibly Causes](#possibly-causes)
  - [Resolution](#resolution-2)
- [MachineAPIOperatorMetricsCollectionFailing](#machineapioperatormetricscollectionfailing)
  - [Query](#query-3)
  - [Possible Causes](#possible-causes-2)
  - [Resolution](#resolution-3)
- [MachineAPIOperatorDown](#machineapioperatordown)
  - [Query](#query-4)
  - [Possible Causes](#possible-causes-3)
  - [Resolution](#resolution-4)

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
If the machine never became a node, consult the [machine troubleshooting guide](TroubleShooting.md).
If the node was deleted from the api, you may choose to delete the machine object, if appropriate.  (FYI, The machine-api will automatically delete nodes, there is no need to delete node objects directly)

## MachineNotYetRunning
Machine did not reach the “Running” Phase.  Running phase is when the machine has successfully become a node and joined the cluster.

### Query
```
# for: 60m
(mapi_machine_created_timestamp_seconds{phase!="Running" and phase!="Deleting"}) > 0
```

### Possible Causes
* The machine was not properly provisioned in the cloud provider due to machine misconfiguration, invalid credentials, or lack of cloud capacity
* The machine took longer than two hours to join the cluster and the bootstrap CSRs were not approved (due to networking or cloud quota/capacity constraints)
* Unusual hostname presented by the kubelet on the bootstrap CSR is preventing CSR approval

### Resolution
If the machine never became a node, consult the [machine troubleshooting guide](TroubleShooting.md).

## MachineNotYetDeleted
Machine has been in the "Deleting" state for too long.

### Query
```
# for: 60m
(mapi_machine_created_timestamp_seconds{phase=="Deleting"}) > 0
```

### Possibly Causes
* The node became unreachable and is preventing the machine from being removed
* A communication failure with the provider is preventing the machine from being removed
* A pod disruption budget is blocking the node from draining

### Resolution
Investigate why the machine has not finished deleting. Consult the
[machine troubleshooting guide](TroubleShooting.md).

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

## MachineAPIOperatorDown
The machine-api-operator is not up.

### Query
```
# for: 5m
absent(up{job="machine-api-operator"} == 1)
```

### Possible Causes
* The deployment has been scaled down
* machine-api-operator is in a failed state

### Resolution
Investigate logs, deployment, and pod events for the machine-api-operator.
