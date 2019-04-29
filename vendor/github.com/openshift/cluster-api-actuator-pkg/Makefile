.PHONY: all
all: check

NO_DOCKER ?= 0
ifeq ($(NO_DOCKER), 1)
  DOCKER_CMD =
  IMAGE_BUILD_CMD = imagebuilder
  CGO_ENABLED = 1
else
  DOCKER_CMD := docker run --rm -v "$(PWD)":/go/src/github.com/openshift/cluster-api-actuator-pkg:Z -w /go/src/github.com/openshift/cluster-api-actuator-pkg openshift/origin-release:golang-1.10
  IMAGE_BUILD_CMD = docker build
endif

ifeq ($(KUBECONFIG),)
KUBECONFIG := ~/.kube/config
endif

# This definition is make-3.81 compatible which is the default stock
# verion on Darwin.
define ginkgo_test
	go test -timeout 60m \
		-v ./pkg/e2e \
		-kubeconfig $1 \
		-machine-api-namespace $2 \
		-args -v 5 -logtostderr \
		$3 $4 $5 $6 $7 $8 $9
endef

.PHONY: depend
depend:
	dep version || go get -u github.com/golang/dep/cmd/dep
	dep ensure

.PHONY: depend-update
depend-update:
	dep ensure -update

.PHONY: check
check: fmt vet lint test ## Check your code

.PHONY: test
test: # Run unit test
	$(DOCKER_CMD) go test -race -cover `go list ./... | grep -v github.com/openshift/cluster-api-actuator-pkg/pkg/e2e`

.PHONY: lint
lint: ## Go lint your code
	hack/go-lint.sh -min_confidence 0.3 $(go list -f '{{ .ImportPath }}' ./...)

.PHONY: fmt
fmt: ## Go fmt your code
	hack/go-fmt.sh .

.PHONY: vet
vet: ## Apply go vet to all go files
	hack/go-vet.sh ./...

.PHONY: build-e2e
build-e2e:
	go test -c -o bin/e2e github.com/openshift/cluster-api-actuator-pkg/pkg/e2e

.PHONY: test-e2e
# Run operator tests first to preserve logs for troubleshooting test
# failures and flakes. Feature:Operator tests remove deployments. Thus
# loosing all the logs previously acquired.
test-e2e:
	$(call ginkgo_test,$(KUBECONFIG),openshift-machine-api,-ginkgo.v,-ginkgo.noColor=true,-ginkgo.focus "Feature:Operators")
	$(call ginkgo_test,$(KUBECONFIG),openshift-machine-api,-ginkgo.v,-ginkgo.noColor=true,-ginkgo.skip "Feature:Operators")

.PHONY: k8s-e2e k8s-e2e-operators k8s-e2e-features

.PHONY: k8s-e2e
# Run k8s specific e2e test Run operator tests first to preserve logs
# for troubleshooting test failures and flakes. Feature:Operator tests
# remove deployments. Thus loosing all the logs previously acquired.
k8s-e2e:
	$(call ginkgo_test,$(KUBECONFIG),kube-system,-ginkgo.v,-ginkgo.noColor=true,-ginkgo.focus "Feature:Operators")
	$(call ginkgo_test,$(KUBECONFIG),kube-system,-ginkgo.v,-ginkgo.noColor=true,-ginkgo.skip "Feature:Operators")

.PHONY: help
help:
	@grep -E '^[a-zA-Z/0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
