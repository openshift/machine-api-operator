# Document Purpose

This document describes how the *Machine API Operator* (herein MAO) gets deployed, and the MAO's responsibilities. It includes guidelines on how to troubleshoot typical issues with resources provisioned by MAO.

## CVO integration

MAO code is running under the `machine-api-operator` Deployment. This and other resources listed below rely on the *Cluster Version Operator* (herein CVO) management strategy". These resources are consumed from the ["/install" directory]( https://github.com/openshift/machine-api-operator/tree/master/install).

Namely:
- `openshift-machine-api` Namespace - the primary and the only namespace, where the MAO is hosted, and all of the dependent resources are being managed by the operator.
- `machine-api-operator` Deployment - MAO code runs here.
- `machine-api-operator-images` ConfigMap - is injected with provider images, used by MAO to deploy cloud-provider specific code.
- CredentialsRequests - granular request for cloud credentials, fulfilled by the [cloud-credential-operator](https://github.com/openshift/cloud-credential-operator)
- ImageStreams for the ConfigMap with provider image mapping - a set of plain DockerImages, injected into OCP release payload.
- Alerts, Metrics, CRDs, RBAC, and Services are deployed from the "/install" directory.

### Upgrade

MAO installation/upgrade procedure relies on CVO. 

MAO is deployed after CVO starts running at [run level](https://github.com/openshift/cluster-version-operator/blob/master/docs/dev/operators.md#how-do-i-get-added-as-a-special-run-level) 30, right after the Kubernetes operators (run level 10-29), and before the `machine-config-operator` (run level 80)

A newly deployed version of MAO Deployment is responsible for installing/upgrading provider executables, as described in the following section.

## This repository is responsible for:

### Maintaining

MAO hosts CustomResourceDefinitions and controllers, responsible for serving each of the following resources.
- Machines
- MachineSets
- MachineHealthChecks

This operator is responsible for the creation and maintenance of:
- `machine-api-operator` ClusterOperator - MAO status reporting
- `machine-api-controllers` Deployment - controllers for all supported CRDs
- `machine-api` ValidatingWebhookConfiguration and MutatingWebhookConfiguration - validation and defaulting for Machine resources
- DaemonSet termination handler - monitoring for spot instances state and remediating Machines, which are deployed on those in case the instance goes away.

### Implementing

- Machine controller - manages Machine resources. It uses actuator [interface](https://github.com/openshift/machine-api-operator/blob/master/pkg/controller/machine/actuator.go#), which follows a Machine lifecycle [pattern](https://github.com/openshift/enhancements/blob/master/enhancements/machine-api/machine-instance-lifecycle.md) This interface provides `Create`, `Update`, and `Delete` methods to manage your provider specific cloud instances, connected storage, and networking settings to make the instance prepared for bootstrapping. Each provider is therefore responsible for implementing these methods.
- MachineSet controller - manages MachineSet resources and ensures the presence of the expected number of replicas and a given provider config for a set of machines.
- MachineHealthCheck controller - manages MachineHealthCheck resources. Ensure machines being targeted by MachineHealthCheck objects are satisfying healthiness criteria or are remediated otherwise.
- NodeLink controller - ensure machines have a nodeRef based on `providerID` matching. Annotate nodes with a label containing the machine name.

### Integrating 

Providers which currently works with MAO, are:
- [AWS](https://github.com/openshift/cluster-api-provider-aws)
- [GCP](https://github.com/openshift/cluster-api-provider-gcp/)
- [OpenStack](https://github.com/openshift/cluster-api-provider-openstack/)
- [vSphere](https://github.com/openshift/machine-api-operator/tree/master/pkg/controller/vsphere)
- [Azure](https://github.com/openshift/cluster-api-provider-azure)
- [BareMetal](https://github.com/openshift/cluster-api-provider-baremetal/)
- [OVirt](https://github.com/openshift/cluster-api-provider-ovirt)

## Works closely, but not directly responsible for

- [cluster-api-actuator-pkg](https://github.com/openshift/cluster-api-actuator-pkg/) - hosting e2e tests for MAO and supported cloud providers.
- [machine-config-operator](https://github.com/openshift/machine-config-operator) - creating MachineConfigs with configuration to inject into provisioned Machine instances. Is responsible for initiating Node bootstrap procedure for newly created Machine.
- [cluster-machine-approver](https://github.com/openshift/cluster-machine-approver) - approving Node CSR for newly provisioned Machine.
- [cluster-autoscaler-operator](https://github.com/openshift/cluster-autoscaler-operator) - automatic scaling of MachineSet resources, manages ClusterAutoscaler and MachineAutoscaler resources.
- [release](https://github.com/openshift/release) - the tooling responsible for building openshift components and images, including MAO.
- [installer](https://github.com/openshift/installer) - provision the initial cluster infrastructure (`IPI`) from a scratch, which is later used by MAO to manipulate `VMs`, network and storage configuration for worker Machines.

## ClusterOperator

### Status management

MAO is responsible for status reporting on the `machine-api` ClusterOperator. Our status reporting  is following the [best practices](https://github.com/openshift/cluster-version-operator/blob/master/docs/dev/clusteroperator.md#conditions).

The status condition will turn `Degraded` if any of the managed resources fail to rollout, or are unavailable for longer [periods](https://github.com/openshift/machine-api-operator/blob/master/pkg/operator/sync.go#L31-L34) of time.

In addition to the cluster-operator status reporting, it is recommended to know relevant alerts described in the alerting [document](https://github.com/openshift/machine-api-operator/blob/master/docs/user/Alerts.md)

## Troubleshooting

Q: Any of the components maintained by MAO is lagging/missing/unchanged for a long period of time.
A: check the status of the `machine-api-operator` deployment, if it is running all the replicas. Check the `machine-api-operator` logs. Check if the `machine-api` cluster-operator didn’t go `Degraded`, or the `MachineApiOperatorDown` alert was not firing. 

Q: MAO deployment is outdated/missing
A: check the CVO health by checking ClusterVersion object ([guide](https://github.com/openshift/cluster-version-operator/blob/master/docs/user/status.md)) It should be `Available` and not `Progressing`.

More details on troubleshooting you will find in the troubleshooting [guide](https://github.com/openshift/machine-api-operator/blob/master/docs/user/TroubleShooting.md).
