all: check build test

DOCKER_CMD := docker run --rm -v "$(PWD)":/go/src/github.com/openshift/machine-api-operator:Z -w /go/src/github.com/openshift/machine-api-operator golang:1.10

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
	mkdir -p bin
	$(DOCKER_CMD) go build -v -o bin/machine-api-operator cmd/main.go

.PHONY: test
test: ## Run tests
	$(DOCKER_CMD) go test ./...

.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
