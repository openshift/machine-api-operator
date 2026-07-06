# Operator

**Platform detection, controller deployment, ClusterOperator status**

## OVERVIEW
Deploys platform-specific controllers, manages ClusterOperator status, handles images configuration.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Platform detection | `platform.go` | Detects cloud provider (vSphere, kubemark, etc.) |
| Controller deployment | `deployment.go` | Deploys machine-controllers Deployment |
| ClusterOperator status | `clusteroperator.go` | Manages MAO ClusterOperator status |
| Images config | `images.go` | Parses images.json ConfigMap |
| Test suite | `operator_suite_test.go` | Ginkgo envtest setup |

## CONVENTIONS

- Platform detection: Check `Infrastructure.cluster` status
- Controller deployment: Always set image from `images.json` ConfigMap
- ClusterOperator: Update status on every reconciliation
- Leader election: Single active operator instance
- Feature gates: Check OpenShift featuregates before enabling

## ANTI-PATTERNS (THIS PACKAGE)

- **NEVER** hardcode container images (use images.json)
- **NEVER** skip leader election
- **ALWAYS** update ClusterOperator status on errors
- **ALWAYS** check feature gates before deploying optional components
- **ALWAYS** validate `images.json` before using

## NOTES

- Images ConfigMap: `0000_30_machine-api-operator_01_images.configmap.yaml`
- CRDs deployed from `/install` (synced via `make crds-sync`)
- Operator manages: machine-controllers, machine-api-tests-ext deployments