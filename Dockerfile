# Reproducible builder image
FROM golang:1.10.0 as builder
WORKDIR /go/src/github.com/openshift/machine-api-operator
# This expects that the context passed to the docker build command is
# the machine-api-operator directory.
# e.g. docker build -t <tag> -f <this_Dockerfile> <path_to_machine-api-operator>
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go install -a -ldflags '-extldflags "-static"' cmd/

# Final container
FROM debian:stretch-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

COPY --from=builder /go/src/github.com/openshift/machine-api-operator/bin/machine-api-operator .
COPY --from=builder /go/src/github.com/openshift/machine-api-operator/manifests manifests
