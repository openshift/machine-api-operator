# machine-api-operator
Operator for rendering, deploying and updating cluster-api components:
- Aggregated API server
- Controller manager
- Machine controller (AWS actuator)

The cluster-api is levereaged by Openshift for running machines under the machine-api control.

# CI & tests

Tests are located in machine-api repository and executed with `make test` in prow CI system. A link to failing tests is published as a comment in PR by @openshift-ci-robot. Current test status for all openshift components can be found in https://deck-ci.svc.ci.openshift.org.

CI configuration is stored in [openshift/release](https://github.com/openshift/release) repository and is split into 3 files:
  - cluster/ci/config/prow/plugins.yaml - says which prow plugins are available and where is job config stored
  - ci-operator/config/openshift/machine-api-operator/master.json - configuration for machine-api component repository
  - cluster/ci/config/prow/config.yaml - prow jobs configuration

More information about those files can be found in [ci-operator onboarding file](https://github.com/openshift/ci-operator/blob/master/ONBOARD.md)  

Initial configuration for machine-api CI pipeline can be found in [PR #1095](https://github.com/openshift/release/pull/1095).
