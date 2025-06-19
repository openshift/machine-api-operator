#!/usr/bin/env bash

set -euo pipefail

# map names of CRD files between the vendored openshift/api repository and the ./install directory
CRDS_MAPPING=(
    "0000_10_machine-api_01_machines-Default.crd.yaml:0000_30_machine-api-operator_02_machine.Default.crd.yaml"
    "0000_10_machine-api_01_machines-CustomNoUpgrade.crd.yaml:0000_30_machine-api-operator_02_machine.CustomNoUpgrade.crd.yaml"
    "0000_10_machine-api_01_machines-DevPreviewNoUpgrade.crd.yaml:0000_30_machine-api-operator_02_machine.DevPreviewNoUpgrade.crd.yaml"
    "0000_10_machine-api_01_machines-TechPreviewNoUpgrade.crd.yaml:0000_30_machine-api-operator_02_machine.TechPreviewNoUpgrade.crd.yaml"
    "0000_10_machine-api_01_machinesets-Default.crd.yaml:0000_30_machine-api-operator_03_machineset.Default.crd.yaml"
    "0000_10_machine-api_01_machinesets-CustomNoUpgrade.crd.yaml:0000_30_machine-api-operator_03_machineset.CustomNoUpgrade.crd.yaml"
    "0000_10_machine-api_01_machinesets-DevPreviewNoUpgrade.crd.yaml:0000_30_machine-api-operator_03_machineset.DevPreviewNoUpgrade.crd.yaml"
    "0000_10_machine-api_01_machinesets-TechPreviewNoUpgrade.crd.yaml:0000_30_machine-api-operator_03_machineset.TechPreviewNoUpgrade.crd.yaml"
    "0000_10_machine-api_01_machinehealthchecks.crd.yaml:0000_30_machine-api-operator_07_machinehealthcheck.crd.yaml"
           )

# Fetch the local directory which holds machine/v1beta1, whether it's vendored
# or not, or accessed from another directory via a module override, workspace,
# or any future mechanism introduced by Go.
packagedir=$(go list -f '{{.Dir}}' github.com/openshift/api/machine/v1beta1)
srcdir="${packagedir}/zz_generated.crd-manifests"

for crd in "${CRDS_MAPPING[@]}" ; do
    SRC="${srcdir}/${crd%%:*}"
    DES="${crd##*:}"
    cp "$SRC" "install/$DES"
done
