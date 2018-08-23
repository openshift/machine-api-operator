# Reproducible builder image
FROM golang:1.10.0 as build
WORKDIR /go/src/github.com/openshift/machine-api-operator
# This expects that the context passed to the docker build command is
# the machine-api-operator directory.
# e.g. docker build -t <tag> -f <this_Dockerfile> <path_to_machine-api-operator>
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go install -a -ldflags '-extldflags "-static"' cmd/

# Final container
FROM openshift/origin-base
RUN yum install -y ca-certificates

COPY --from=build /go/src/github.com/openshift/machine-api-operator/bin/machine-api-operator .
COPY --from=build /go/src/github.com/openshift/machine-api-operator/manifests manifests
COPY --from=build /go/src/github.com/openshift/machine-api-operator/machines machines
