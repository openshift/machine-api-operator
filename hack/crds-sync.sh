#!/usr/bin/env bash

set -euo pipefail

REPO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..

YQ_BIN=yq
YQ_PATH=tools/bin/${YQ_BIN}

cd "${REPO_ROOT}" && make ${YQ_BIN} >/dev/null

# map names of CRD files between the vendored openshift/api repository and the ./install directory
CRDS_MAPPING=( "vendor/github.com/openshift/api/machine/v1beta1/0000_10_machine.crd.yaml:0000_30_machine-api-operator_02_machine.crd.yaml"
               "vendor/github.com/openshift/api/machine/v1beta1/0000_10_machineset.crd.yaml:0000_30_machine-api-operator_03_machineset.crd.yaml"
               "vendor/github.com/openshift/api/machine/v1beta1/0000_10_machinehealthcheck.yaml:0000_30_machine-api-operator_07_machinehealthcheck.crd.yaml"
               "third_party/cluster-api/crd/ipam.cluster.x-k8s.io_ipaddressclaims.yaml:0000_30_machine-api-operator_03_ipaddressclaims.crd.yaml"
               "third_party/cluster-api/crd/ipam.cluster.x-k8s.io_ipaddresses.yaml:0000_30_machine-api-operator_03_ipaddresses.crd.yaml")

for crd in "${CRDS_MAPPING[@]}" ; do
    SRC="${crd%%:*}"
    DES="${crd##*:}"
    cp "$SRC" "install/$DES"
    # Inject needed annotation if not found
    ${YQ_PATH} -i -N '.metadata.annotations."exclude.release.openshift.io/internal-openshift-hosted" |= "true"' "install/$DES"
    ${YQ_PATH} -i -N '.metadata.annotations."include.release.openshift.io/self-managed-high-availability" |= "true"' "install/$DES"
    ${YQ_PATH} -i -N '.metadata.annotations."include.release.openshift.io/single-node-developer" |= "true"' "install/$DES"
done
