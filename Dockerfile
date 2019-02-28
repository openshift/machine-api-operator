FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
RUN git clone https://github.com/redhat-nfvpe/machine-api-operator $GOPATH/src/github.com/redhat-nfvpe/machine-api-operator
WORKDIR /go/src/github.com/redhat-nfvpe/machine-api-operator
RUN git checkout remotes/origin/add_baremetal
COPY . .
RUN NO_DOCKER=1 make build

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/redhat-nfvpe/machine-api-operator/owned-manifests owned-manifests
COPY --from=builder /go/src/github.com/redhat-nfvpe/machine-api-operator/install manifests
COPY --from=builder /go/src/github.com/redhat-nfvpe/machine-api-operator/bin/machine-api-operator .
COPY --from=builder /go/src/github.com/redhat-nfvpe/machine-api-operator/bin/nodelink-controller .
COPY --from=builder /go/src/github.com/redhat-nfvpe/machine-api-operator/bin/machine-healthcheck .
LABEL io.openshift.release.operator true
