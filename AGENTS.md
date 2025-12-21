# AGENTS.md

This file provides guidance to AI Agents when working with the machine-api-operator project.

## Quick Reference

### Essential Commands
```bash
make build          # Build all binaries
make test           # Run all tests (Ginkgo + envtest)
make lint           # Run golangci-lint
make fmt            # Format code
make vet            # Run go vet
make check          # Run all validations (lint, fmt, vet, test)
make crds-sync      # Sync CRDs from vendored openshift/api
```

### Running Locally
```bash
./bin/machine-api-operator start --kubeconfig $KUBECONFIG --images-json=path/to/images.json
```

## Project Overview

The Machine API Operator (MAO) manages the lifecycle of Machine resources in OpenShift clusters, enabling declarative machine management across multiple cloud providers.

### Architecture

| Binary | Location | Purpose |
|--------|----------|---------|
| machine-api-operator | `cmd/machine-api-operator/` | Main operator; deploys platform-specific controllers |
| machineset | `cmd/machineset/` | MachineSet replica management |
| machine-healthcheck | `cmd/machine-healthcheck/` | Health monitoring and remediation |
| nodelink-controller | `cmd/nodelink-controller/` | Links Nodes ↔ Machines |
| vsphere | `cmd/vsphere/` | VSphere machine controller |
| machine-api-tests-ext | `cmd/machine-api-tests-ext/` | Extended E2E tests |

> **Note:** Other cloud providers (AWS, GCP, Azure) live in separate `machine-api-provider-*` repos.

### Key Packages

| Package | Purpose |
|---------|---------|
| `pkg/controller/machine/` | Machine lifecycle (create/delete instances, drain nodes, track phases) |
| `pkg/controller/machineset/` | Replica management, delete policies (Random, Oldest, Newest) |
| `pkg/controller/machinehealthcheck/` | Node condition monitoring, remediation triggers |
| `pkg/controller/nodelink/` | Machine↔Node linking via providerID/IP, label/taint sync |
| `pkg/controller/vsphere/` | VSphere actuator |
| `pkg/operator/` | Platform detection, controller deployment, ClusterOperator status |
| `pkg/webhooks/` | Admission webhooks for Machine/MachineSet validation and mutation |

### Key Patterns
- CRDs: Machine, MachineSet, MachineHealthCheck
- Uses controller-runtime from sigs.k8s.io
- Vendored dependencies (`go mod vendor`, use `GOFLAGS=-mod=vendor`)
- Feature gates controlled via OpenShift's featuregates mechanism
- When bumping `github.com/openshift/api`, run `make crds-sync` to sync CRDs from `/vendor` to `/install` (CVO deploys from there)

## Testing

```bash
make test                    # All unit tests
NO_DOCKER=1 make test        # Run locally without container
make test-e2e                # E2E tests (requires KUBECONFIG)
```

### Running Specific Package Tests
```bash
KUBEBUILDER_ASSETS="$(go run ./vendor/sigs.k8s.io/controller-runtime/tools/setup-envtest use 1.34.1 -p path --bin-dir ./bin --index https://raw.githubusercontent.com/openshift/api/master/envtest-releases.yaml)" \
go run ./vendor/github.com/onsi/ginkgo/v2/ginkgo -v ./pkg/controller/machine/...
```

### Ginkgo Configuration
- Default args: `-r -v --randomize-all --randomize-suites --keep-going --race --trace --timeout=10m`
- Use `GINKGO_EXTRA_ARGS` to add arguments
- Use `GINKGO_ARGS` to override defaults entirely

### Test Patterns
- Tests use **Ginkgo/Gomega** with **envtest** for K8s API simulation
- Prefer **komega** over plain Gomega/Ginkgo where possible
- Each controller has a `*_suite_test.go` for setup
- Follow existing test patterns in the codebase

### Container Engine
- Defaults to `podman`, falls back to `docker`
- `USE_DOCKER=1` to force Docker
- `NO_DOCKER=1` to run locally without containers

## Do

- Run `make lint` before committing
- Run `make test` to verify changes
- Check `pkg/controller/<name>/` for controller logic
- Look at existing controllers as patterns for new code

## Do NOT

- Edit files under `vendor/` directly
- Add new cloud providers here (they belong in `machine-api-provider-*` repos)
- Forget to run `go mod vendor` after changing dependencies
- Skip running tests before submitting changes
