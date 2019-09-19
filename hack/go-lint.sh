#!/bin/sh
# Example:  ./hack/go-lint.sh cmd/... pkg/...

if [ "$IS_CONTAINER" != "" ]; then
  golint -set_exit_status "${@}"
else
  docker run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/machine-api-operator:z" \
    --workdir /go/src/github.com/openshift/machine-api-operator \
    --env GO111MODULE="$GO111MODULE" \
    --env GOFLAGS="$GOFLAGS" \
    openshift/origin-release:golang-1.12 \
    ./hack/go-lint.sh "${@}"
fi
