# vSphere Controller

**vSphere-specific Machine actuator (only provider in this repo)**

## OVERVIEW
Implements Machine lifecycle for vSphere infrastructure; manages VM creation, deletion, and state tracking.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Reconcile loop | `vsphere_controller.go` | Main vSphere reconciliation |
| VM creation | `vm_provision.go` | vSphere VM provisioning |
| Session management | `session/` | vCenter connection handling |
| MachineSet support | `machineset/` | vSphere-specific MachineSet logic |
| Test suite | `vsphere_suite_test.go` | Ginkgo envtest setup |

## STRUCTURE

```
vsphere/
├── session/          # vCenter connection/session management
├── machineset/       # vSphere-specific MachineSet controller
└── [root]           # Core vSphere actuator
```

## CONVENTIONS

- vCenter connection: Always use session pool from `session/`
- VM naming: `{cluster}-{machine-name}-{uuid}`
- Power state: Check before operations (powered on/off/suspended)
- Network: Always attach to configured port groups
- Disk: Use thin provisioning by default

## ANTI-PATTERNS (THIS PACKAGE)

- **NEVER** create VMs outside Machine API (causes orphaned resources)
- **NEVER** skip VM power state checks
- **ALWAYS** use session pool (not direct vCenter connections)
- **ALWAYS** handle vSphere API rate limiting
- **ALWAYS** clean up orphaned VMs on Machine deletion

## NOTES

- Only cloud provider implemented directly in MAO repo
- Other providers (AWS/GCP/Azure) live in `machine-api-provider-*` repos
- Session pool prevents vCenter connection exhaustion