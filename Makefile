all: check build test

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
	go vet cmd/
#	go vet ./... #TODO fix when vendoring is complete

.PHONY: build
build: ## Build binary
	@echo -e "\033[31mNO BUILD :-(\033[0m"


.PHONY: test
test: ## Run tests
	@echo -e "\033[31mNO TESTS :-(\033[0m"



.PHONY: help
help:
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'
