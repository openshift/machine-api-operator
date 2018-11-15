FROM registry.svc.ci.openshift.org/openshift/release:golang-1.10 AS builder
WORKDIR /go/src/github.com/openshift/machine-api-operator
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/machine-api-operator -a -ldflags '-extldflags "-static"' github.com/openshift/machine-api-operator/cmd/machine-api-operator
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/nodelink-controller -a -ldflags '-extldflags "-static"' github.com/openshift/machine-api-operator/cmd/nodelink-controller
RUN CGO_ENABLED=0 GOOS=linux go build -o bin/machine-healthcheck -a -ldflags '-extldflags "-static"' github.com/openshift/machine-api-operator/cmd/machine-healthcheck

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/owned-manifests owned-manifests
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/install manifests
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-api-operator .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/nodelink-controller .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-healthcheck .
LABEL io.openshift.release.operator true
