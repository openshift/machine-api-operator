# Machine API Operator
An Operator for managing the cluster-api stack and the Openshift owned machineSets:
- Aggregated API server
- Controller manager
- Machine controller (AWS/Libvirt actuator)

# Deployment on top of an existing Installer cluster
The fastest method to deploy a custom image of machine-api-operator is to deploy it on top on existing installer cluster.

1. Deploy a cluster using the [openshift installer][installer].
2. Build and push your `machine-api-operator` image to a test registry:
```
REGISTRY=quay.io/<your repo>/machine-api-operator:test make image
REGISTRY=quay.io/<your repo>/machine-api-operator:test make push
```
3. Edit the machine-api-operator deployment to switch to your newly created image:
```
kubectl edit deployment machine-api-operator -n openshift-cluster-api
```
4. Delete the pre-existing `machine-api-operator` pod, and it will re-deploy using the custom image.

[installer]: https://github.com/openshift/installer "openshift installer"

# Manual deployment (for Kubernetes cluster)

When running the mao on the installer it assumes some existing resources that make it work as expected.
To deploy the machine-api-operator in a vanilla kubernetes environment, one needs to precreate these assumptions:

- Create the `openshift-machine-api-operator` namespace
- Create the [CRD Status definition](tests/e2e/manifests/status-crd.yaml)
- Create the [mao config](tests/e2e/manifests/mao-config.yaml)
- Create the [apiserver certs](tests/e2e/manifests/clusterapi-apiserver-certs.yaml)
- Create a given [ign config](tests/e2e/manifests/ign-config.yaml)
- You can run it as a deployment with [this manifest](tests/e2e/manifests/operator-deployment.yaml)
- Build:
    ```sh
    make build
    ```
- Run:
    ```sh
    ./bin/machine-api-operator --kubeconfig ${HOME}/.kube/config  --config tests/e2e/manifests/mao-config.yaml --manifest-dir manifests
    ```
- Image:
    ```
    make image
    ```
For running all this steps automatically check [e2e test](tests)


# CI & tests

[e2e test in a vanilla Kubernetes environment](tests/e2e)

Tests are located in [machine-api-operator repository][1] and executed with `make test` in prow CI system. A link to failing tests is published as a comment in PR by @openshift-ci-robot. Current test status for all OpenShift components can be found in https://deck-ci.svc.ci.openshift.org.

CI configuration is stored in [openshift/release][2] repository and is split into 3 files:
  - [cluster/ci/config/prow/plugins.yaml][3] - says which prow plugins are available and where job config is stored
  - [ci-operator/config/openshift/machine-api-operator/master.yaml][4] - configuration for machine-api-operator component repository
  - [ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-presubmits.yaml][5] - prow jobs configuration for presubmits
  - [ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-postsubmits.yaml][6] - prow jobs configuration for postsubmits

More information about those files can be found in [ci-operator onboarding file][7]

Initial configuration for machine-api-operator CI pipeline can be found in [PR #1095][8].

[1]: https://github.com/openshift/machine-api-operator
[2]: https://github.com/openshift/release
[3]: https://github.com/openshift/release/blob/master/cluster/ci/config/prow/plugins.yaml
[4]: https://github.com/openshift/release/blob/master/ci-operator/config/openshift/machine-api-operator/master.yaml
[5]: https://github.com/openshift/release/blob/master/ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-presubmits.yaml
[6]: https://github.com/openshift/release/blob/master/ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-master-postsubmits.yaml
[7]: https://github.com/openshift/ci-operator/blob/master/ONBOARD.md
[8]: https://github.com/openshift/release/pull/1095
