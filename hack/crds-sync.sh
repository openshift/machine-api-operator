#!/usr/bin/env bash

set -euo pipefail

# map names of CRD files between the vendored openshift/api repository and the ./install directory
CRDS_MAPPING=( "vendor/github.com/openshift/api/machine/v1beta1/0000_10_machine.crd.yaml:0000_30_machine-api-operator_02_machine.crd.yaml"
               "vendor/github.com/openshift/api/machine/v1beta1/0000_10_machineset.crd.yaml:0000_30_machine-api-operator_03_machineset.crd.yaml"
               "vendor/github.com/openshift/api/machine/v1beta1/0000_10_machinehealthcheck.yaml:0000_30_machine-api-operator_07_machinehealthcheck.crd.yaml")

for crd in "${CRDS_MAPPING[@]}" ; do
    SRC="${crd%%:*}"
    DES="${crd##*:}"
    cp "$SRC" "install/$DES"
done
