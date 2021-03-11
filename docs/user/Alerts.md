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
(mapi_machine_created_timestamp_seconds{phase!="Running"}) > 0
```

### Possible Causes
* The machine was not properly provisioned in the cloud provider due to machine misconfiguration, invalid credentials, or lack of cloud capacity
* The machine took longer than two hours to join the cluster and the bootstrap CSRs were not approved (due to networking or cloud quota/capacity constraints)
* Unusual hostname presented by the kubelet on the bootstrap CSR is preventing CSR approval

### Resolution
If the machine never became a node, consult the machine troubleshooting guide.

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
