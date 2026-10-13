# User FAQ

This document consists of frequently asked questions intended for users.

<!-- toc -->
- [Important Terminology](#important-terminology)
  - [Machine vs Node vs VM/Instance](#machine-vs-node-vs-vminstance)
- [Machines](#machines)
  - [Machine Phases?](#machine-phases)
  - [Can I change a Machine’s Spec?](#can-i-change-a-machines-spec)
  - [I created a Machine but it never joined the cluster](#i-created-a-machine-but-it-never-joined-the-cluster)
  - [What happens when I delete a Machine?](#what-happens-when-i-delete-a-machine)
  - [I want to skip draining when I delete a Machine](#i-want-to-skip-draining-when-i-delete-a-machine)
  - [What happens if I delete an Instance or VM outside of the Machine API, such as in the AWS web console?](#what-happens-if-i-delete-an-instance-or-vm-outside-of-the-machine-api-such-as-in-the-aws-web-console)
  - [Can I remove the finalizer for a Machine that is stuck in deleting?](#can-i-remove-the-finalizer-for-a-machine-that-is-stuck-in-deleting)
  - [How do I set a Node’s Role (eg, Worker)](#how-do-i-set-a-nodes-role-eg-worker)
  - [How does the user-data get created for the VMs?](#how-does-the-user-data-get-created-for-the-vms)
  - [Adding Annotations and Labels to Nodes via Machines](#adding-annotations-and-labels-to-nodes-via-machines)
    - [Which Annotations and Labels get added to Nodes?](#which-annotations-and-labels-get-added-to-nodes)
    - [Which Annotations and Labels won't get added to Nodes?](#which-annotations-and-labels-wont-get-added-to-nodes)
  - [Adding Taints to Nodes via Machines](#adding-taints-to-nodes-via-machines)
  - [Machine API doesn’t support some cloud feature](#machine-api-doesnt-support-some-cloud-feature)
- [MachineSets](#machinesets)
  - [What decides which Machines to destroy when a MachineSet is scaled down?](#what-decides-which-machines-to-destroy-when-a-machineset-is-scaled-down)
  - [What Happens if I change a MachineSet](#what-happens-if-i-change-a-machineset)
  - [After I edit a MachineSet, how can I replace the existing Machines?](#after-i-edit-a-machineset-how-can-i-replace-the-existing-machines)
  - [Can I add an existing Machine to a MachineSet?](#can-i-add-an-existing-machine-to-a-machineset)
  - [Can I remove a Machine from a MachineSet without deleting it?](#can-i-remove-a-machine-from-a-machineset-without-deleting-it)
- [Machine Deployments](#machine-deployments)
  - [Upstream kubernetes cluster-api project is utilizing MachineDeployments, why isn’t OpenShift?](#upstream-kubernetes-cluster-api-project-is-utilizing-machinedeployments-why-isnt-openshift)
- [Cluster Autoscaler](#cluster-autoscaler)
  - [What is the difference between the ClusterAutoscaler and MachineAutoscaler resources?](#what-is-the-difference-between-the-clusterautoscaler-and-machineautoscaler-resources)
  - [Cluster AutoScaler won’t scale up](#cluster-autoscaler-wont-scale-up)
<!-- /toc -->

# Important Terminology
## Machine vs Node vs VM/Instance
Machine means a “Machine object” in the OpenShift API.  This object is reconciled by the “Machine-controller” and serves as a record of information for cloud resources.

“VM” or “Instance” means the actual compute resource running on the cloud.

“Node” means the kubelet and the “Node” object in the API.

These terms, while obviously related, are not interchangeable.  Oftentimes, people will say “I deleted the Node” and what they really mean is “I deleted the VM using the cloud provider’s CLI tool.”  As you can see, this can cause confusion and delay in resolving your issue.

# Machines

## Machine Phases?
Please see docs for Machine phases: https://github.com/openshift/enhancements/blob/master/enhancements/machine-api/machine-instance-lifecycle.md

## Can I change a Machine’s Spec?
It will have no effect, and may have negative results.  For example, if you remove a master Machine’s load balancer entries, the Machine will not be removed from the load balancer, and when the Machine is deleted, the instance will not be removed from the load balancer prior to deletion.  Changes to other attributes such as image-id or instance type will have no effect.

## I created a Machine but it never joined the cluster
This can be a variety of things.

First, determine if the Machine object’s status has information regarding networking and instance ID’s.  If this information is present, it means the cloud provider accepted the request to create the Machine and the Machine was created successfully.

If successful, check for any pending CSR requests.  If there are not any pending CSR requests, then the problem is not related to the Machine-api.  Possible causes include misconfigured MCO Machine config, cloud or networking problems, or a Machine that has the wrong user-provided configuration (eg, you set the wrong subnet or image ID).

If the Machine takes a very unusual amount of time to start after Machine creation, the CSR created for the associated Node to join the cluster will not be automatically approved.  This can be due to cloud capacity constraints, cloud api problems, temporary network conditions, or other circumstances.  You can either approve the CSRs (after approving one, another will be created for the same Node), or delete the Machine and create a new one (the MachineSet will create a new Machine for you if this Machine is part of a MachineSet).

If the Machine’s status does not have any instance ID or networking information associated with it, most likely the instance has failed to be created.  This can be due to misconfiguration, inadequate or invalid cloud credentials, cloud API quota exhaustion, or other cloud API problems such as an outage or temporary network condition.  You will need to inspect the Machine-controllers logs for more definitive information.

## What happens when I delete a Machine?
This section assumes you’ve deleted a Machine from the API, rather than deleting a VM or Instance in the cloud provider.

When a Machine is marked deleted, the associated Node is immediately cordoned and drained.  Draining utilizes the eviction API, so PodDisruptionBudgets will be respected.  If a Node cannot be successfully drained due to a PDB, the operation will retry indefinitely.

After the associated Node is drained, the associated instance or VM is deleted from the cloud provider.  After the instance is deleted in the cloud provider, the associated Node object is deleted from the API.

Finally, the finalizer is removed from the Machine object and the Machine object is removed from the API.

## I want to skip draining when I delete a Machine
This is not recommended for most cases, especially Master Machines.  Properly draining Machines will respect PodDisruptionBudgets and prevent the cluster and workloads from going into an unhealthy state.

You can optionally set an **annotation** **"machine.openshift.io/exclude-node-draining"** on each Machine object you wish for draining to be skipped.  Annotations take the form of key/value pairs.
This annotation does not require any specific value, merely the key being present will disable
draining (even with a value of 'false' or similar).  This can be applied or removed at any time.

## What happens if I delete an Instance or VM outside of the Machine API, such as in the AWS web console?
This is not recommended.  By default, the Machine-api will not take any corrective action.  If you are  utilizing MachineHealthChecks, the Machine may get deleted depending on the configuration of the MHC.

## Can I remove the finalizer for a Machine that is stuck in deleting?
This is not recommended.  This may result in orphaned Node objects and orphaned compute resources.

## How do I set a Node’s Role (eg, Worker)
This is set via the MCO’s config, not the Machine-api. The config a Machine receives upon boot is decided by the Machine's user-data.

## How does the user-data get created for the VMs?
The user-data is generated by the installer.  The Machine-api is just a consumer of the user-data and simply passes the data to the cloud.  Refer to documentation for the Machine-config-operator (not part of the Machine-api) for creating new Machine pools and user-data.

## Adding Annotations and Labels to Nodes via Machines
Annotations and Labels are added to Nodes via Machines whenever the machine or its node is updated. You can add arbitrary Annotations and Labels which are applied to Nodes immediately after they join the cluster. Removing Annotations or Labels from a Machine won't remove them from its Node. If you want to add an Annotation or Label that you will later remove, consider adding it directly to the Node object. If you add an Annotation or Label to a Machine, and later decide to remove it, you will need to remove it from the Machine first to prevent it from being reapplied, and then remove it from the Node object.

### Which Annotations and Labels get added to Nodes?
MachineSets can be used to help mark the Machines and Nodes that are created from them by using Annotations and Labels. To do this you will need to add this information in various places on your MachineSet depending on where you would like this information. **Note** this information will only be applied to new Machines and Nodes created from the MachineSet, they will not be retroactively applied.

Setting Labels or Annotations in a MachineSet in its `.spec.template.metadata` field will cause them to be applied to every Machine object created from that MachineSet. Specifically these Labels and Annotations will end up in the Machine object's `.metadata` field.

Setting Labels or Annotations in the MachineSet resource's `.spec.template.spec.metadata` field will cause them to be applied to every Machine and Node object created from the MachineSet. In the case of Machine objects, these Labels and Annotations will be applied in resource's `.spec.metadata` field. For Node objects, these values will be applied to `.metadata` field.

### Which Annotations and Labels won't get added to Nodes?
There are two other areas for Annotations and Labels that won't result in them being added to
the Node object after creation.

The first area is the MachineSet's own metadata annotations (`.metadata.annotations`) and Labels (`.metadata.labels`).  These are not copied to the Node.

The next area are the Annotations and Labels applied to each Machine object's metadata.  These
are applied to each Machine object, but not copied to the Node.  In the case of a
MachineSet, these are in the `.spec.template.metadata` field, or just the `.metadata` field when applied to an individual Machine.

## Adding Taints to Nodes via Machines
Similar to Annotations and Labels, MachineSets can be used to add Taints to the Machines and Nodes that it creates. To do this you will need to add the Taint information to your MachineSets. **Note** that these Taints will only be applied to the new Machines and Nodes created from the MachineSet, they will not be retroactively applied unless added directly to the Machine.

If you wish to remove a taint from a Node, you will need to remove it from the Machine first to prevent it from being reapplied, and then remove it from the Node object.

Setting Taints in a MachineSet's `.spec.template.spec.taints` field will cause them to be applied to every Machine and Node object created from the MachineSet. In the case of Machine objects, the Taints will appear in `.spec.taints` field, and for Node objects they will be in the `.spec.taints` field.

## Machine API doesn’t support some cloud feature
There is a limited number of features we support on each cloud provider that are relevant to most users.  We’re always working to add more features and better support existing features.  Please feel free to file an RFE for any functionality you need.

# MachineSets

## What decides which Machines to destroy when a MachineSet is scaled down?
By default, it selects a Machine at random.  You can set **Spec.DeletePolicy** to **“Random”, “Oldest”, or “Newest”**.  You can also designate Machines with an annotation which will override all other selection criteria: **"machine.openshift.io/delete-machine"**

## What Happens if I change a MachineSet
You are free to edit a MachineSet at any time.  Any changes you make will not affect existing Machines, only Machines created after the changes are made.

## After I edit a MachineSet, how can I replace the existing Machines?

There is not one right answer here, you can do a variety of things.

First, it’s recommended to disable the autoscaler.  You don’t want it to fight what you’re trying to do.  Second, try not to operate on more than about 20 Machines at once.  Reason being, some cloud providers have API quota and will rate limit the amount of requests you can make in a short period of time.

One option is to delete each Machine in the MachineSet (‘oc delete -n openshift-Machine-api Machine example-Machine-1’).  This will cause the MachineSet to automatically create a new Machine with the updated Machine template.  You can do these in rapid succession, or you can wait for the replacement Machine to be created, or wait some nominal amount of time between deleting each Machine.

Another option is to scale the MachineSet to 0, wait for the Machines to be marked deleted, then scale the MachineSet back to the desired value.

## Can I add an existing Machine to a MachineSet?
This is not recommended.  This could be achieved by creating the appropriate labels on a Machine to match the labels in the ‘Match Labels’ section of the MachineSet.  If this happens, the MachineSet will see it has too many Machines and get rid of one.

## Can I remove a Machine from a MachineSet without deleting it?
Yes, though consider carefully before doing so.  Inspect the Match Labels from the desired MachineSet.  Remove 1 or more of those labels from the desired Machine.  This will cause the Machine to be orphaned from the MachineSet, and the MachineSet-controller will create a new Machine to replace it.

# Machine Deployments

## Upstream kubernetes cluster-api project is utilizing MachineDeployments, why isn’t OpenShift?
Any change to a MachineDeployment will result in an immediate removal and replacement of an entire MachineSet.  This is a much more costly operation that making changes to an instance in-place.  In particular RHEL CoreOS allows the VM to boot into an entirely updated operating system without having to perform a reinstallation.

Modifications to a machine that can not be rolled out in-place (for example a change to the instance type) must be rolled out manually by scaling the MachineSet down and up again or by deleting machines one by one to trigger
a re-creation.

# Cluster Autoscaler

## What is the difference between the ClusterAutoscaler and MachineAutoscaler resources?
The ClusterAutoscaler resource is used to manage the lifecycle of the Kubernetes cluster autoscaler. It can be used to specify how the cluster autoscaler should be deployed and what global system limits it should respect. You can only create one of this resource.
([ClusterAutoscaler example](https://github.com/openshift/cluster-autoscaler-operator/blob/master/examples/clusterautoscaler.yaml))

The MachineAutoscaler resource is used to inform the cluster autoscaler that a MachineSet should be considered for autoscaling. Each MachineAutoscaler can be used to specify a single MachineSet as well as the minimum and maximum sizes for the set. You may create many of these resources depending on the topology of your cluster and your desired scaling needs.
([MachineAutoscaler example](https://github.com/openshift/cluster-autoscaler-operator/blob/master/examples/machineautoscaler.yaml))

## Cluster AutoScaler won’t scale up
For the most part, this happens when scaling up a MachineSet results in violating the one of the max resource quotas you have configured.  For example, Max CPU is the aggregate for all Machines in a MachineSet, not just one.  So, if your max replicas is set to 50, max CPU needs to be at minimum 50 * (instance-type-CPU).

Where to look for more info to troubleshoot?
‘oc get events’ can be helpful in identifying autoscaler decisions.

The logs of the autoscaler pod will also contain useful information.  Note, the ‘cluster-autoscaler-operator’ deploys the autoscaler.
