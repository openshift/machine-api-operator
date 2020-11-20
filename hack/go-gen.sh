#!/bin/sh

REPO_NAME=$(basename "${PWD}")
if [ "$IS_CONTAINER" != "" ]; then
  go generate ./pkg/apis/...
else
  docker run -it --rm \
    --env IS_CONTAINER=TRUE \
    --volume "${PWD}:/go/src/sigs.k8s.io/${REPO_NAME}:z" \
    --workdir "/go/src/sigs.k8s.io/${REPO_NAME}" \
    openshift/origin-release:golang-1.15 \
    ./hack/go-gen.sh
fi
