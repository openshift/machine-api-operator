#REGISTRY    ?= quay.io/openshift/
VERSION     ?= $(shell git describe --always --abbrev=7)
MUTABLE_TAG ?= latest
IMAGE        = $(REGISTRY)machine-api-operator

.PHONY: all
all: check build test

ifndef NO_DOCKER
  DOCKER_CMD := docker run --rm -v "$(PWD)":/go/src/github.com/openshift/machine-api-operator:Z -w /go/src/github.com/openshift/machine-api-operator golang:1.10
endif

.PHONY: check
check: ## Lint code
	@echo -e "\033[32mRunning golint...\033[0m"
#	go get -u github.com/golang/lint # TODO figure out how to install when there is no golint
	golint ./...
	@echo -e "\033[32mRunning yamllint...\033[0m"
	@for file in $(shell find $(CURDIR) -name "*.yaml" -o -name "*.yml"); do \
		yamllint --config-data \
		'{extends: default, rules: {indentation: {indent-sequences: consistent}, line-length: {level: warning, max: 120}}}'\
		$$file; \
	done
	@echo -e "\033[32mRunning go vet...\033[0m"
	$(DOCKER_CMD) go vet ./...

.PHONY: build
build: ## Build binary
	@echo -e "\033[32mBuilding package...\033[0m"
	mkdir -p bin
	$(DOCKER_CMD) go build -v -o bin/machine-api-operator cmd/main.go

.PHONY: test
test: ## Run tests
	@echo -e "\033[32mTesting...\033[0m"
	$(DOCKER_CMD) go test ./...

.PHONY: image
image: build ## Build docker image
	@echo -e "\033[32mBuilding image $(IMAGE):$(VERSION)...\033[0m"
	docker build -t "$(IMAGE):$(VERSION)" -f ./cmd/Dockerfile ./
	@echo -e "\033[32mTagging image as $(IMAGE):$(MUTABLE_TAG)...\033[0m"
	docker tag "$(IMAGE):$(VERSION)" "$(IMAGE):$(MUTABLE_TAG)"

.PHONY: push
push: ## Push image to docker registry
	@echo -e "\033[32mPushing images...\033[0m"
	docker push "$(IMAGE):$(VERSION)"
	docker push "$(IMAGE):$(MUTABLE_TAG)"

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
