# Machine Disruption Budget

## Summary

Machine disruption budget controller should monitor machines and **MachineDisruptionBudget** objects and updates accordingly the **MachineDisruptionBudget** object status.
It will provide the updated ingormation regarding the number of healthy machines with specific labels under the cluster that can be used by another controllers.

## Motivation

To provide updated information to a user or to an external controller regarding the number of healthy machines under the cluster.

### Goals

* To provide the updated information regarding the number of healthy machines with specific labels via **MachineDisruptionBudget** object.

### Non-goals

* To provide controller to delete unhealthy machines.
* To provide integration with controller that will delete unhealthy machines.

## Proposal

### Driving Use Case

* When we talk about bare metal machines, possible the situation when the bare metal machine runs some critical flow, like CEPH storage replication and if more than the specified number of machines of such kind unavailable we do not want to delete all unavailable machines until we will have a healthy ones that succeeded to replicate the storage.
* Prevent the fencing storm when the cluster has the controller that responsible for deletion of unhealthy machines. The controller will check the relevant **MachineDisruptionBudget** object for the machine and if it already has more unhealthy machines than expected the controller will skip deletion of the unhealthy machine.

### MachineDisruptionBudget API

This proposal introduces a new API type: **MachineDisruptionBudget**.

This API makes possible to create simple **MachineDisruptionBudget** object with a label selector. You can specify `minAvailable` to prevent the deletion when less than `minAvailable` healthy machines are available or `maxUnavailable` to prevent the deletion when more than `maxUnavailable` unhealthy machines exist.

#### Example with minAvailable

```yaml
apiVersion: healthchecking.openshift.io/v1alpha1
kind: MachineDisruptionBudget
metadata:
  name: mdb-test
  namespace: openshift-machine-api
spec:
  selector:
    matchLabels:
      machine.openshift.io/cluster-api-machine-role: worker
      machine.openshift.io/ceph-storage: true
  minAvailable: 3
```

#### Example with maxUnavailable

```yaml
apiVersion: healthchecking.openshift.io/v1alpha1
kind: MachineDisruptionBudget
metadata:
  name: mdb-test
  namespace: openshift-machine-api
spec:
  selector:
    matchLabels:
      machine.openshift.io/cluster-api-cluster: test-1-4pcgd
      machine.openshift.io/cluster-api-machine-role: worker
      machine.openshift.io/cluster-api-machine-type: worker
      machine.openshift.io/cluster-api-machineset: test-1-4pcgd-worker-0
  maxUnavailable: 4
```

#### MachineDisruptionBudget status

**MachineDisruptionBudget** status will show update information regarding the healthy machines under the cluster.

```yaml
status:
  # total number of machines with the labels that correspond to the label selector
  ExpectedMachines: 5
  # currently number of healthy machines
  currentHealthy: 3
  # desired number of healthy machines
  desiredHealthy: 3
  # how many disruptions is currently available
  disruptionsAllowed: 0
  # used mostly for the integration with an additional controller that will delete unhealthy machines, if observedGeneration is different from the generation of the MDB object it means that MDB object still does not have updated information
  observedGeneration: 1
```

### Future integration with the MachineHealthCheck controller

When the cluster has an unhealthy machine, the controller will try to get the `MachineDisruptionBudget` object for the machine, if it does not have one, continue, as usual, otherwise:

- if the MDB `observedGeneration` less than the MDB `generation`, skip the machine deletion
- if the MDB `disruptionsAllowed` less or equal to zero, skip the machine deletion
- if the `DisruptedMachines` map has more `MaxDisruptedMachinSize` entries, skip the machine deletion
- if it has `disruptionsAllowed` greater than zero, it will reduce the MDB `disruptionsAllowed` by one, add the deleted machine to the `DisruptedMachines` map and will try to update the MDB status, in case it fails, the code will retry to update again with exponential backoff
