# MachineSet Controller

**Replica management for MachineSet CRD**

## OVERVIEW
Ensures expected number of Machine replicas with consistent provider config; implements delete policies (Random/Oldest/Newest).

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Reconcile loop | `machineset_controller.go` | Main reconciliation logic |
| Delete policy | `machineset_delete_policy.go` | Random/Oldest/Newest implementations |
| Replica sync | `machineset_replica.go` | Scale up/down logic |
| Machine selection | `machineset_selector.go` | Label-based machine matching |
| Test suite | `machineset_suite_test.go` | Ginkgo envtest setup |

## CONVENTIONS

- Prefer komega over plain Gomega
- Each test uses `*_suite_test.go` pattern
- Tests run with `--race --trace --timeout=10m`
- Delete policy: Never delete machines with `Deleting` finalizer
- Replica sync: Always check MachineSet status before scaling

## ANTI-PATTERNS (THIS PACKAGE)

- **NEVER** skip status update after replica change
- **NEVER** delete machines without checking delete policy
- **ALWAYS** use `komega.Eventually()` for async state checks
- **ALWAYS** respect `MachineSet.DeletionTimestamp` during reconciliation

## NOTES

- MachineSet controller does NOT create instances (Machine controller does)
- Delete policy only applies when scaling down
- Machines with `machine.openshift.io/deletion-in-progress` are protected