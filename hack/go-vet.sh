#!/bin/sh
if [ "$IS_CONTAINER" != "" ]; then
  go vet "${@}"
else
  docker run --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/github.com/openshift/machine-api-operator:z" \
    --workdir /go/src/github.com/openshift/machine-api-operator \
    --env GO111MODULE="$GO111MODULE" \
    --env GOFLAGS="$GOFLAGS" \
    openshift/origin-release:golang-1.12 \
    ./hack/go-vet.sh "${@}"
fi;
