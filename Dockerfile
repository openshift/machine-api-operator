FROM registry.ci.openshift.org/openshift/release:golang-1.24 AS builder
WORKDIR /go/src/github.com/openshift/machine-api-operator
COPY . .
RUN NO_DOCKER=1 make build && \
    mkdir -p /tmp/build && \
    cp /go/src/github.com/openshift/machine-api-operator/bin/machine-api-tests-ext /tmp/build/machine-api-tests-ext && \
    gzip /tmp/build/machine-api-tests-ext

FROM registry.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/install manifests
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-api-operator .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/nodelink-controller .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-healthcheck .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machineset ./machineset-controller
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/vsphere ./machine-controller-manager
COPY --from=builder /tmp/build/machine-api-tests-ext.gz .

LABEL io.k8s.display-name="OpenShift Machine API Operator" \
      io.openshift.release.operator=true \
      io.openshift.tags="openshift,tests,e2e,e2e-extension"
