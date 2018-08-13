# machine-api-operator
Operator for rendering, deploying and updating cluster-api components:
- Aggregated API server
- Controller manager
- Machine controller (AWS actuator)

The cluster-api is levereaged by OpenShift for running machines under the machine-api control.

# CI & tests

Tests are located in [machine-api-operator repository][1] and executed with `make test` in prow CI system. A link to failing tests is published as a comment in PR by @openshift-ci-robot. Current test status for all OpenShift components can be found in https://deck-ci.svc.ci.openshift.org.

CI configuration is stored in [openshift/release][2] repository and is split into 3 files:
  - [cluster/ci/config/prow/plugins.yaml][3] - says which prow plugins are available and where job config is stored
  - [ci-operator/config/openshift/machine-api-operator/master.json][4] - configuration for machine-api-operator component repository
  - [ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-presubmits.yaml][5] - prow jobs configuration for presubmits
  - [ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-postsubmits.yaml][6] - prow jobs configuration for postsubmits

More information about those files can be found in [ci-operator onboarding file][7]

Initial configuration for machine-api-operator CI pipeline can be found in [PR #1095][8].

[1]: https://github.com/openshift/machine-api-operator
[2]: https://github.com/openshift/release
[3]: https://github.com/openshift/release/blob/master/cluster/ci/config/prow/plugins.yaml
[4]: https://github.com/openshift/release/blob/master/ci-operator/config/openshift/machine-api-operator/master.json
[5]: https://github.com/openshift/release/blob/master/ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-postsubmits.yaml
[6]: https://github.com/openshift/release/blob/master/ci-operator/jobs/openshift/machine-api-operator/openshift-machine-api-operator-postsubmits.yaml
[7]: https://github.com/openshift/ci-operator/blob/master/ONBOARD.md
[8]: https://github.com/openshift/release/pull/1095
