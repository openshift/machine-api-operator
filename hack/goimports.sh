#!/bin/sh

for TARGET in "${@}"; do
  find "${TARGET}" -name '*.go' ! -path '*/third_party/*' ! -path '*/vendor/*' ! -path '*/.build/*' -exec goimports -w {} \+
done
git diff --exit-code

