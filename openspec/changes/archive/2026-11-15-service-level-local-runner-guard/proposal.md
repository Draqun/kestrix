# Proposal: service-level-local-runner-guard

## Why

In MR !73, Kestrix made local task execution opt-in via `runner.allow_local` (`KESTRIX_RUNNER_ALLOW_LOCAL`, default `false`) to prevent arbitrary script execution in the control-plane container holding the master encryption key and JWT signing key. However, this check was placed exclusively at the HTTP layer (`handleRunProject`, `handleRunTask`, `handleApproveManualTask`). The core service methods (`RunService.Start`, `RunService.StartTask`, `RunService.ApproveManualTask`) do not enforce `CheckLocalAllowed` internally. This leaves a defense-in-depth gap if a future CLI command, gRPC transport, or internal orchestrator invokes the service directly.

## What Changes

- Embed `CheckLocalAllowed` inside `RunService.Start`, `RunService.StartTask`, and `RunService.ApproveManualTask` before source acquisition and execution.
- If a project resolves to `local` runner mode and `allow_local` is false, return `ErrLocalExecutionDisabled`.
- Update the ~76 `RunService` unit test call sites across 6 files that previously assumed unrestricted local execution to configure `allow_local = true` in their test service setups.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `task-execution-isolation`: mandates service-level rejection of local execution when `runner.allow_local` is false.

## Non-goals

- Altering the HTTP handler error mapping (which already returns 400 with a descriptive error).
- Modifying Docker or remote runner execution modes.

## Impact

- Modifies `internal/service/service.go`.
- Updates unit test setups in `internal/service/service_test.go`, `run_test.go`, and related fixtures.
