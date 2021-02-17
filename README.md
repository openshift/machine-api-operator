# Machine API Operator

The Machine API Operator manages the lifecycle of specific purpose CRDs, controllers and RBAC objects that extend the Kubernetes API.
This allows to convey desired state of machines in a cluster in a declarative fashion.

See https://github.com/openshift/enhancements/tree/master/enhancements/machine-api for more details.

Have a question? See our [Frequently Asked Questions](FAQ.md) for common inquiries.

## Architecture

![Machine API Operator overview](machine-api-operator.png)

## CRDs

- MachineSet
- Machine
- MachineHealthCheck

## Controllers

- MachineSet Controller

Ensure presence of expected number of replicas and a given provider config for a set of machines.

- Machine Controller

  - [cluster-api-provider-aws](https://github.com/openshift/cluster-api-provider-aws)

  - [cluster-api-provider-gcp](https://github.com/openshift/cluster-api-provider-gcp)

  - [cluster-api-provider-azure](https://github.com/openshift/cluster-api-provider-azure)

  - [cluster-api-provider-libvirt](https://github.com/openshift/cluster-api-provider-libvirt)

  - [cluster-api-provider-openstack](https://github.com/openshift/cluster-api-provider-openstack)

  - [cluster-api-provider-baremetal](https://github.com/openshift/cluster-api-provider-baremetal)

  - [cluster-api-provider-ovirt](https://github.com/openshift/cluster-api-provider-ovirt)

Ensure that a provider instance is created for a Machine object in a given provider.

- Node link Controller

Ensure machines have a nodeRef based on IPs or providerID matching.
Annotate nodes with a label containing the machine name.


- Machine healthcheck controller

Ensure machines targeted by MachineHealthCheck objects satisfy a healthiness criteria or are remediated otherwise.

## Creating machines

You can create a new machine by [applying a manifest representing an instance of the machine CRD](docs/examples/machine.yaml)

The `machine.openshift.io/cluster-api-cluster` label will be used by the controllers to lookup for the right cloud instance.

You can set other labels to provide a convenient way for users and consumers to retrieve groups of machines:
```
machine.openshift.io/cluster-api-machine-role: worker
machine.openshift.io/cluster-api-machine-type: worker
```

## Dev

- Generate code (if needed):

  ```sh
  $ make generate
  ```

- Build:

  ```sh
  $ make build
  ```

- Run:

  ```sh
  $ ./bin/machine-api-operator start --kubeconfig ${HOME}/.kube/config --images-json=pkg/operator/fixtures/images.json
  ```

- Image:

  ```
  $ make image
  ```

The Machine API Operator is designed to work in conjunction with the [Cluster Version Operator](https://github.com/openshift/cluster-version-operator).
You can see it in action by running an [OpenShift Cluster deployed by the Installer](https://github.com/openshift/installer).

However you can run it in a vanilla Kubernetes cluster by precreating some assets:

- Create a `openshift-machine-api-operator` namespace
- Create a [CRD Status definition](config/0000_00_cluster-version-operator_01_clusteroperator.crd.yaml)
- Create a [CRD Machine definition](install/0000_30_machine-api-operator_02_machine.crd.yaml)
- Create a [CRD MachineSet definition](install/0000_30_machine-api-operator_03_machineset.crd.yaml)
- Create a [Installer config](config/kubemark-config-infra.yaml)
- Then you can run it as a [deployment](install/0000_30_machine-api-operator_11_deployment.yaml)
- You should then be able to deploy a [machineSet](config/machineset.yaml) object

For more information see [hacking-guide](docs/dev/hacking-guide.md).

## Machine API operator with Kubemark over Kubernetes

INFO: For development and testing purposes only

1. Deploy MAO over Kubernetes:
  ```sh
   $ kustomize build | kubectl apply -f -
   ```

2. Deploy [Kubemark actuator](https://github.com/openshift/cluster-api-provider-kubemark) prerequisities:
   ```sh
   $ kustomize build config | kubectl apply -f -
   ```

3. Create `cluster` `infrastructure.config.openshift.io` to tell the MAO to deploy `kubemark` provider:
   ```yaml
   apiVersion: apiextensions.k8s.io/v1beta1
   kind: CustomResourceDefinition
   metadata:
     name: infrastructures.config.openshift.io
   spec:
     group: config.openshift.io
     names:
       kind: Infrastructure
       listKind: InfrastructureList
       plural: infrastructures
       singular: infrastructure
     scope: Cluster
     versions:
     - name: v1
       served: true
   storage: true
   ---
   apiVersion: config.openshift.io/v1
   kind: Infrastructure
   metadata:
     name: cluster
   status:
     platform: kubemark
   ```

   The file is already present under `config/kubemark-config-infra.yaml` so it's sufficient to run:
   ```sh
   $ kubectl apply -f config/kubemark-config-infra.yaml
   ```

## OpenShift Bugzilla

The Bugzilla product for this repository is "Cloud Compute" under [OpenShift Container Platform](https://bugzilla.redhat.com/enter_bug.cgi?product=OpenShift%20Container%20Platform).

## CI & tests

Run unit test:

```
$ make test
```

Run e2e-aws-operator tests. This tests assume that a cluster deployed by the Installer is up and running and a ```KUBECONFIG``` environment variable is set:

```
$ make test-e2e
```

Tests are located under [machine-api-operator repository][1] and executed in prow CI system. A link to failing tests is published as a comment in PR by `@openshift-ci-robot`. Current test status for all OpenShift components can be found in https://deck-ci.svc.ci.openshift.org.

CI configuration is stored under [openshift/release][2] repository and is split into 4 files:
  - [cluster/ci/config/prow/plugins.yaml][3] - says which prow plugins are available and where job config is stored
  - [ci-operator/config/openshift/machine-api-operator/master.yaml][4] - configuration for machine-api-operator component repository
  - [ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-presubmits.yaml][5] - prow jobs configuration for presubmits
  - [ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-postsubmits.yaml][6] - prow jobs configuration for postsubmits

More information about those files can be found in [ci-operator onboarding file][7].

[1]: https://github.com/openshift/machine-api-operator
[2]: https://github.com/openshift/release
[3]: https://github.com/openshift/release/blob/master/cluster/ci/config/prow/plugins.yaml
[4]: https://github.com/openshift/release/blob/master/ci-operator/config/openshift/machine-api-operator/openshift-machine-api-operator-master.yaml
[5]: https://github.com/openshift/release/blob/master/ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-presubmits.yaml
[6]: https://github.com/openshift/release/blob/master/ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-postsubmits.yaml
[7]: https://github.com/openshift/ci-operator/blob/master/ONBOARD.md
