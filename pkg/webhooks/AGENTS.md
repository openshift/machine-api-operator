# Webhooks

**Admission webhooks for Machine/MachineSet validation and mutation**

## OVERVIEW
Validates and mutates Machine/MachineSet resources on create/update; enforces cluster scoping and default labels.

## WHERE TO LOOK

| Task | File | Notes |
|------|------|-------|
| Machine validation | `admission_machine.go` | ValidateMachine webhook |
| Machine mutation | `admission_machine.go` | MutateMachine webhook |
| MachineSet validation | `admission_machineset.go` | ValidateMachineSet webhook |
| Webhook server | `webhook.go` | Server setup, TLS config |
| Test suite | `webhook_suite_test.go` | Ginkgo envtest setup |

## CONVENTIONS

- Validation: Check cluster label, required fields, immutable fields
- Mutation: Add default labels, validate provider config
- TLS: Always use valid certificates (not self-signed in production)
- Timeout: Webhooks must respond within 10 seconds
- Error messages: Always actionable, reference CRD docs

## ANTI-PATTERNS (THIS PACKAGE)

- **NEVER** skip cluster label validation
- **NEVER** allow Machine.Spec changes after creation
- **NEVER** block deletions in validation webhook
- **ALWAYS** return detailed validation errors
- **ALWAYS** set default labels in mutation (not validation)

## NOTES

- Webhooks run as separate Deployment (machine-webhook)
- Certificates managed by cert-manager or operator
- Use `--kubeconfig` for local testing, webhook for cluster