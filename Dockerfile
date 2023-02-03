FROM registry.ci.openshift.org/openshift/release:golang-1.19 AS builder
WORKDIR /go/src/github.com/openshift/machine-api-operator
COPY . .
RUN NO_DOCKER=1 make build

FROM registry.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/install manifests
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-api-operator .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/nodelink-controller .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-healthcheck .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machineset ./machineset-controller
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/vsphere ./machine-controller-manager

LABEL io.openshift.release.operator true
