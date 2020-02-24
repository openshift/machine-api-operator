#!/bin/sh

REPO_NAME=$(basename "${PWD}")
if [ "$IS_CONTAINER" != "" ]; then
  for TARGET in "${@}"; do
    find "${TARGET}" -name '*.go' ! -path '*/vendor/*' ! -path '*/.build/*' -exec goimports -w {} \+
  done
  git diff --exit-code
else
  docker run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/machine-api-operator:z" \
    --workdir /go/src/github.com/openshift/machine-api-operator \
    openshift/origin-release:golang-1.13 \
    ./hack/goimports.sh "${@}"
fi
