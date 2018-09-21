# Reproducible builder image
FROM openshift/origin-release:golang-1.10 as build
WORKDIR /go/src/github.com/openshift/machine-api-operator
# This expects that the context passed to the docker build command is
# the machine-api-operator directory.
# e.g. docker build -t <tag> -f <this_Dockerfile> <path_to_machine-api-operator>
COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o bin/machine-api-operator -a -ldflags '-extldflags "-static"' github.com/openshift/machine-api-operator/cmd

# Final container
FROM openshift/origin-base

COPY --from=build /go/src/github.com/openshift/machine-api-operator/bin/machine-api-operator .
COPY --from=build /go/src/github.com/openshift/machine-api-operator/manifests manifests
COPY --from=build /go/src/github.com/openshift/machine-api-operator/machines machines
