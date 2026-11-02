# Spec Delta: task-execution-isolation

## ADDED Requirements

### Requirement: Service-level local runner execution gate
The execution service layer (`RunService`) SHALL reject execution of any project or task resolving to `local` runner mode unless `runner.allow_local` is explicitly configured to `true`.

#### Scenario: Service rejects local run when allow_local is false
- **WHEN** `RunService.Start` is called on a project configured with runner mode `local`
- **AND** `runner.allow_local` is `false`
- **THEN** execution terminates immediately with `ErrLocalExecutionDisabled`
- **AND** no source repositories are acquired or cloned
