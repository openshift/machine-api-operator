# Machine Controller

**Machine lifecycle management across cloud providers**

## OVERVIEW
Creates/deletes provider instances, tracks Machine phases, drains nodes before deletion.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Reconcile loop | `machine_controller.go` | Core lifecycle logic |
| Instance creation | `machine_instance.go` | Provider instance provisioning |
| Node draining | `machine_drain.go` | Pre-delete node drain |
| Phase tracking | `machine_phase.go` | Provisioning/Running/Failed states |
| Test suite | `machine_suite_test.go` | Ginkgo envtest setup |

## CONVENTIONS

- Machine phases: `Provisioning` → `Running` → `Failed`/`Terminating`
- Always check `Machine.Status.Phase` before operations
- Node drain: Skip for master machines (not recommended)
- Instance deletion: Always wait for provider cleanup
- ProviderID: Required for Node ↔ Machine linking

## ANTI-PATTERNS (THIS PACKAGE)

- **NEVER** change Machine.Spec (has no effect)
- **NEVER** skip draining when deleting worker machines
- **NEVER** remove finalizer manually (causes orphaned VMs)
- **ALWAYS** check `Machine.DeletionTimestamp` before provisioning
- **ALWAYS** set `Machine.Status.ProviderID` after instance creation

## NOTES

- Provider logic lives in `machine-api-provider-*` repos (except vSphere)
- Machine controller is platform-agnostic; actuators are external
- Use `machine.openshift.io/cluster-api-cluster` label for cluster scoping