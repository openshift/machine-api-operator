DBG         ?= 0
#REGISTRY    ?= quay.io/openshift/
VERSION     ?= $(shell git describe --always --abbrev=7)
MUTABLE_TAG ?= latest
IMAGE        = $(REGISTRY)machine-api-operator
BUILD_IMAGE ?= registry.ci.openshift.org/openshift/release:golang-1.21
GOLANGCI_LINT = go run ./vendor/github.com/golangci/golangci-lint/cmd/golangci-lint

# Enable go modules and vendoring
# https://github.com/golang/go/wiki/Modules#how-to-install-and-activate-module-support
# https://github.com/golang/go/wiki/Modules#how-do-i-use-vendoring-with-modules-is-vendoring-going-away
GO111MODULE = on
export GO111MODULE
GOFLAGS ?= -mod=vendor
export GOFLAGS

ifeq ($(DBG),1)
GOGCFLAGS ?= -gcflags=all="-N -l"
endif

TOOLS_DIR=./tools
BIN_DIR=bin
TOOLS_BIN_DIR := $(TOOLS_DIR)/$(BIN_DIR)
CONTROLLER_GEN := $(abspath $(TOOLS_BIN_DIR)/controller-gen)

.PHONY: all
all: check build test

NO_DOCKER ?= 1

ifeq ($(shell command -v podman > /dev/null 2>&1 ; echo $$? ), 0)
	ENGINE=podman
else ifeq ($(shell command -v docker > /dev/null 2>&1 ; echo $$? ), 0)
	ENGINE=docker
else
	NO_DOCKER=1
endif

USE_DOCKER ?= 0
ifeq ($(USE_DOCKER), 1)
	ENGINE=docker
endif

ifeq ($(NO_DOCKER), 1)
  DOCKER_CMD =
  IMAGE_BUILD_CMD = imagebuilder
else
  DOCKER_CMD := $(ENGINE) run --env GO111MODULE=$(GO111MODULE) --env GOFLAGS=$(GOFLAGS) --rm -v "$(PWD)":/go/src/github.com/openshift/machine-api-operator:Z  -w /go/src/github.com/openshift/machine-api-operator $(BUILD_IMAGE)
  # The command below is for building/testing with the actual image that Openshift uses. Uncomment/comment out to use instead of above command. CI registry pull secret is required to use this image.
  IMAGE_BUILD_CMD = $(ENGINE) build
endif

PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
ENVTEST = go run ${PROJECT_DIR}/vendor/sigs.k8s.io/controller-runtime/tools/setup-envtest
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.29

$(CONTROLLER_GEN): $(TOOLS_DIR)/go.mod # Build controller-gen from tools folder.
	cd $(TOOLS_DIR); GO111MODULE=on GOFLAGS=-mod=vendor go build -tags=tools -o $(BIN_DIR)/controller-gen sigs.k8s.io/controller-tools/cmd/controller-gen

.PHONY: vendor
vendor:
	$(DOCKER_CMD) ./hack/go-mod.sh

.PHONY: check
check: verify-crds-sync lint fmt vet test ## Run code validations

.PHONY: build
build: machine-api-operator nodelink-controller machine-healthcheck machineset vsphere ## Build binaries

.PHONY: machine-api-operator
machine-api-operator:
	$(DOCKER_CMD) ./hack/go-build.sh machine-api-operator

.PHONY: nodelink-controller
nodelink-controller:
	$(DOCKER_CMD) ./hack/go-build.sh nodelink-controller

.PHONY: machine-healthcheck
machine-healthcheck:
	$(DOCKER_CMD) ./hack/go-build.sh machine-healthcheck

.PHONY: vsphere
vsphere: generate-third-party-deepcopy
	$(DOCKER_CMD) ./hack/go-build.sh vsphere

.PHONY: machineset
machineset:
	$(DOCKER_CMD) ./hack/go-build.sh machineset

.PHONY: test-e2e
test-e2e: ## Run openshift specific e2e tests
	./hack/e2e.sh test-e2e

.PHONY: test-e2e-tech-preview
test-e2e-tech-preview: ## Run openshift specific e2e tech preview tests
	./hack/e2e.sh test-e2e-tech-preview

.PHONY: test-sec
test-sec:
	$(DOCKER_CMD) hack/gosec.sh ./...

.PHONY: test
test: unit

unit:
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(PROJECT_DIR)/bin)" ./hack/ci-test.sh

.PHONY: image
image: ## Build docker image
ifeq ($(NO_DOCKER), 1)
	./hack/imagebuilder.sh
endif
	@echo -e "\033[32mBuilding image $(IMAGE):$(VERSION) and tagging also as $(IMAGE):$(MUTABLE_TAG)...\033[0m"
	$(IMAGE_BUILD_CMD) -t "$(IMAGE):$(VERSION)" -t "$(IMAGE):$(MUTABLE_TAG)" ./

.PHONY: push
push: ## Push image to docker registry
	@echo -e "\033[32mPushing images...\033[0m"
	$(ENGINE) push "$(IMAGE):$(VERSION)"
	$(ENGINE) push "$(IMAGE):$(MUTABLE_TAG)"

.PHONY: lint
lint: ## Run golangci-lint over the codebase.
	 $(call ensure-home, ${GOLANGCI_LINT} run ./pkg/... ./cmd/... --timeout=10m)

.PHONY: fmt
fmt: ## Update and show diff for import lines
	$(DOCKER_CMD) hack/go-fmt.sh .

.PHONY: goimports
goimports: ## Go fmt your code
	$(DOCKER_CMD) hack/goimports.sh .

.PHONY: vet
vet: ## Apply go vet to all go files
	$(DOCKER_CMD) hack/go-vet.sh ./pkg/... ./cmd/...

.PHONY: generate-third-party-deepcopy
generate-third-party-deepcopy: $(CONTROLLER_GEN)
	$(CONTROLLER_GEN) object  paths="./third_party/cluster-api/..."
	$(CONTROLLER_GEN) crd paths="./third_party/cluster-api/..." output\:crd\:artifacts\:config=./third_party/cluster-api/crd


.PHONY: crds-sync
crds-sync: ## Sync crds in install with the ones in the vendored oc/api
	$(DOCKER_CMD) hack/crds-sync.sh .

.PHONY: verify-crds-sync
verify-crds-sync: ## Verify that the crds in install and the ones in vendored oc/api are in sync
	$(DOCKER_CMD) hack/crds-sync.sh . && hack/verify-diff.sh .

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

define ensure-home
	@ export HOME=$${HOME:=/tmp/kubebuilder-testing}; \
	if [ $${HOME} == "/" ]; then \
	  export HOME=/tmp/kubebuilder-testing; \
	fi; \
	$(1)
endef
