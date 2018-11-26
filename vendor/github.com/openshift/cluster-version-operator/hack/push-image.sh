#!/usr/bin/env bash

set -eu


function print_info {
	echo "INFO: $1" >&2
}

REPO=${REPO:-"openshift"}

if [ -z ${VERSION+a} ]; then
        print_info "Using version from git..."
        VERSION=$(git describe --abbrev=8 --dirty --always)
fi

set -x
podman push "cluster-version-operator:${VERSION}" "${REPO}/origin-cluster-version-operator:${VERSION}"
podman push "cluster-version-operator:${VERSION}" "${REPO}/origin-cluster-version-operator:latest"
