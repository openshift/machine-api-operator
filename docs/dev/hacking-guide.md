# Machine API - Hacking guide

## Important
This guide assumes that the reader is familiar with Machine API and its resources ([Machines, MachineSets, etc.]( https://docs.openshift.com/container-platform/latest/machine_management/creating_machinesets/creating-machineset-aws.html)). 

Frequently asked questions can be found here - https://github.com/openshift/machine-api-operator/blob/master/FAQ.md

A diagram of the Machine lifecycle can be found here - https://github.com/openshift/enhancements/blob/master/enhancements/machine-api/machine-instance-lifecycle.md#machine-lifecycle-diagram

## Table of contents
- [I'm completely new here, where to start?](#im-completely-new-here-where-to-start?)
  * [General information about Machine API structure](#general-information-about-machine-api-structure)
  * [How to start contributing](#how-to-start-contributing)
- [How to run unit tests](#how-to-run-unit-tests)
- [How to run a component locally for testing](#how-to-run-a-component-locally-for-testing)
   * [Running machine controller](#running-machine-controller)
- [How to build the software in a container for remote testing](#how-to-build-the-software-in-a-container-for-remote-testing)
- [How to run e2e tests](#how-to-run-e2e-tests)
  * [Running specific e2e tests](#running-specific-e2e-tests)
- [How to update dependencies](#how-to-update-dependencies)
- [How to update generated artifacts](#how-to-update-generated-artifacts)
- [How to use something other than Docker to run make targets](#how-to-use-something-other-than-Docker-to-run-make-targets)
  * [Troubleshooting make targets](#troubleshooting-make-targets)
- [Some links to the CI configuration](#some-links-to-the-ci-configuration)
- [Where to file an issue](#where-to-file-an-issue)

## I'm completely new here, where to start?
### General information about Machine API structure
Machine API consists of a number different components:

- Machine API Operator - https://github.com/openshift/machine-api-operator. The operator contains CRDs (Machine,MachineSet, MachineHeathCheck) and is responsible for running controllers for given CRDs. It’s also responsible for running the controllers on the cluster.
  - [Machine](https://github.com/openshift/machine-api-operator/tree/master/pkg/controller/machine)
  - [MachineSet](https://github.com/openshift/machine-api-operator/tree/master/pkg/controller/machineset)
  - [MachineHealthCheck](https://github.com/openshift/machine-api-operator/tree/master/pkg/controller/machinehealthcheck)
  - [NodeLink](https://github.com/openshift/machine-api-operator/tree/master/pkg/controller/nodelink)

- Provider specific implementation. Each provider except vSphere is located in a separate repository. These providers use the code from the machine controller(link to MAO/machine-controller) and extend it with cloud specific logic:
  - https://github.com/openshift/cluster-api-provider-gcp
  - https://github.com/openshift/cluster-api-provider-azure
  - https://github.com/openshift/cluster-api-provider-aws
  - https://github.com/openshift/machine-api-operator/tree/master/pkg/controller/vsphere
  - https://github.com/openshift/cluster-api-provider-openstack
  - https://github.com/openshift/cluster-api-provider-baremetal
  - https://github.com/openshift/cluster-api-provider-ovirt

### How to start contributing

The Machine API Operator and it's components are written in Golang, make sure Golang is installed in your machine and it's
version is at least `1.15`

First you need to determine if the change is related to cloud provider specific logic or it's related to all providers? Maybe you want to contribute to our e2e test suite?

If it is a cloud provider related change, you should fork and clone one of provider repositories, the list can be found above.

If the change is related to all providers, you should fork and clone machine-api-operator.

In order to add changes to our e2e test suite fork and clone [cluster-api-actuator-pkg](https://github.com/openshift/cluster-api-actuator-pkg).

Our project follows the [GitHub flow](https://guides.github.com/introduction/flow/) for contributing the code.

## How to run unit tests
You may want to run unit tests before pushing changes. It can be done in a similar way for Machine API Operator and cloud providers repositories.

Prerequisite:
```
git checkout github.com/openshift/$repository_name
cd $repository_name
```
In order to run the unit tests locally on your machine run the following command:
```
NO_DOCKER=1 make test
```
*Note* :
If you run this command inside the machine-api-operator directory, it will run unit tests for machine, machineset, machine health check controllers and vsphere provider.
If this command is run inside a cloud provider repository you will run only cloud provider specific tests.

## How to run a component locally for testing
### Running machine controller
Prerequisites:
```
git checkout github.com/openshift/$repository_name
cd $repository_name
```
Make sure your $KUBECONFIG is set properly, because it will be used to interact with your cluster.

First step is to scale down the cluster version operator.

“At the heart of OpenShift is an operator called the Cluster Version Operator. This operator watches the deployments and images related to the core OpenShift services, and will prevent a user from changing these details. If I want to replace the core OpenShift services I will need to scale this operator down.”

```
oc scale --replicas=0 deployment/cluster-version-operator -n openshift-cluster-version
```

Second step is scaling down the machine-api-operator. The operator watches for machine controller to be running and will prevent it from scaling down.

``` 
oc scale deployment/machine-api-operator -n openshift-machine-api --replicas=0
```

Third step, once MAO is down scaled it is required to remove the machine-controller container from machine-api-controllers Deployment.

```
oc edit deployment/machine-api-controllers -n openshift-machine-api
```

Finally, once all has been scaled down you can compile and run the controller.

```
NO_DOCKER=1 make build
 ./bin/machine-api-operator -v 5
```

*Notes*:
NO_DOCKER=1 will build the controller on your local machine and outside of any containers.
The commands and binary names might slightly differ across providers

## How to build the software in a container for remote testing

The section is inspired by [this](https://notes.elmiko.dev/2020/08/18/tips-experimenting-mapi.html) blog post

Prerequisites:
```
git checkout github.com/openshift/$repository_name
cd $repository_name
```

First you’ll need to build an image with your changes.

```
make images
make push
```
Note: The commands might be slightly different for providers.


Second step, similar to running the component locally we need to scale down CVO.

```
oc scale --replicas=0 deployment/cluster-version-operator -n openshift-cluster-version
```
Next, edit machine-api-operator-images ConfigMap and place a link to your image there.

The openshift-machine-api project contains all the resources and components that are used by the Machine API. There is a ConfigMap named machine-api-operator-images. This ConfigMap contains references to all the images used by the Machine API Operator, it uses these to deploy the controller and ensure they are the proper images. The ConfigMap looks something like this:
```
apiVersion: v1
kind: ConfigMap
data:
  images.json: |
    {
        "machineAPIOperator": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerAWS": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerOpenStack": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerLibvirt": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerBareMetal": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerAzure": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerGCP": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerOvirt": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "clusterAPIControllerVSphere": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "baremetalOperator": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "baremetalIronic": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "baremetalIronicInspector": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "baremetalIpaDownloader": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "baremetalMachineOsDownloader": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:...",
        "baremetalStaticIpManager": "quay.io/openshift-release-dev/ocp-v4.0-art-dev@sha256:..."
    }
```

```
oc edit configmap/machine-api-operator-images -n openshift-machine-api
```

With the new image information loaded into the ConfigMap, the next thing you might do is replace the Machine API operator. This operator controls how the specific cloud controllers are deployed and coordinated. You only change this component if there is something you are testing.

The easiest way to change this operator is to change the image reference in the deployment. The commands you can use:

```
oc edit deployment/machine-api-operator -n openshift-machine-api
```

After changing the download reference in the images ConfigMap an easy way to swap out the controller is to let the Machine API operator do it for you. You can delete the deployment associated with the cloud provider controller and then the Machine API operator will create a new one for you, like this:

```
oc delete deployment/machine-api-controllers -n openshift-machine-api
```

## How to run e2e tests

For running e2e on your cluster refer to this [document](https://github.com/openshift/cluster-api-actuator-pkg#running-the-cluster-autoscaler-operator-e2e-tests-against-an-openshift-cluster)

### Running specific e2e tests
In case you want to disable MHC, autoscaler or infra test, you can remove or comment them here - https://github.com/openshift/cluster-api-actuator-pkg/blob/master/pkg/e2e_test.go#L20

Example of disabling autoscaler tests:
```
…
// _ "github.com/openshift/cluster-api-actuator-pkg/pkg/autoscaler"
_ "github.com/openshift/cluster-api-actuator-pkg/pkg/infra"
_ "github.com/openshift/cluster-api-actuator-pkg/pkg/machinehealthcheck"
_ "github.com/openshift/cluster-api-actuator-pkg/pkg/operators"
)

```

## How to update dependencies
machine-api-operator is vendored in every provider repository.

```
git checkout github.com/openshfit/$provider_repository_name
cd $provider_repository_name
go get github.com/openshift/machine-api-operator@master
make vendor
```

## How to update generated artifacts
The machine-api-operator contains some files which are generated by automation tools. These files occasionally need updating (for example, when resource API fields change).

Checkout the machine-api-operator repo.
Run `make generate`.

## How to use something other than Docker to run make targets
Make targets can be run outside of a container when the `NO_DOCKER=1` is set. 
For running make targets inside containers:
* targets will run `podman` as the default engine,
* if `podman` is not installed, targets will use `docker`
* to run targets with `docker` even when `podman` is installed, set `USE_DOCKER=1` 

### Troubleshooting make targets
Running make targets with `docker`, causes some files to be created with root owner. This means that running make targets with `podman` afterwards will fail, due to permission issues.
To fix this, run:
```
./hack/owner_reset.sh
```
*Note* : Changing owners with this script requires user to have sudo privileges.

Running `make images NO_DOCKER=1` without having `docker-deamon` running on your machine fails. Reason for this is that [openshift/imagebuilder](https://github.com/openshift/imagebuilder) is being used to build images and it requires docker-deamon. 
To solve this, run:
```
sudo podman system service --time=0 unix:///var/run/docker.sock
```
then, to get imagebuilder, run:
```
./hack/imagebuilder.sh
```
and to finally build the images run:
```
sudo $GOPATH/bin/imagebuilder  -t "origin-aws-machine-controllers:$(git describe --always --abbrev=7)" -t "origin-aws-machine-controllers:latest" ./
```
## Some links to the CI configuration
TODO

## Where to file an issue
- Login to bugzilla https://bugzilla.redhat.com/
- Create a new bug for OpenShift Container Platform product, our component is called Cloud Compute.
