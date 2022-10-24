#!/bin/bash

set -euo pipefail

OPENSHIFT_CI=${OPENSHIFT_CI:-""}
E2E_RELEASE_BRANCH=${E2E_RELEASE_BRANCH:-""}
GIT_BRANCH_ARGS=""

if [ "$OPENSHIFT_CI" == "true" ]; then # detect ci environment there
  E2E_RELEASE_BRANCH=${PULL_BASE_REF} # use target branch as E2E_RELEASE_BRANCH for handling backports correctly
fi;

if [ "$E2E_RELEASE_BRANCH" != "" ]; then
  echo "cloning branch $E2E_RELEASE_BRANCH"
  GIT_BRANCH_ARGS="--branch ${E2E_RELEASE_BRANCH} --single-branch"
fi;

unset GOFLAGS
tmp="$(mktemp -d)"

pushd "$tmp"
  git clone ${GIT_BRANCH_ARGS} --depth 1 "https://github.com/openshift/cluster-api-actuator-pkg.git" .
  echo "git branch: $(git status -b -s)"
  echo "latest git commit: $(git --no-pager log --oneline -1)"
popd

exec make -C "$tmp" test-e2e
