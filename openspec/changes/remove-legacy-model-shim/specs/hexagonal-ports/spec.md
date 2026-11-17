# Spec Delta: hexagonal-ports

## ADDED Requirements

### Requirement: Single canonical domain package without legacy model shim
All packages in the module SHALL import domain entities (`Repo`, `ExecuteSpec`, `Task`, `Project`, `ProjectFile`, `Variable`, `AcquiredSource`) directly from `kestrix/internal/domain`, and the legacy `kestrix/internal/model` alias package SHALL NOT exist.

#### Scenario: No package imports internal/model
- **WHEN** running `go test ./internal/domain/portscheck/...` or inspecting package dependencies across `cmd/...` and `internal/...`
- **THEN** zero packages import `kestrix/internal/model` and `internal/model` does not exist in the repository
