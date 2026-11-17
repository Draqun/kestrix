# Spec Delta: service-layer

## ADDED Requirements

### Requirement: Cohesive per-service file modularization
The `internal/service` package SHALL organize `ProjectService`, repository URL validation, `RunService`, `WebhookService`, and `BuildVarStore` into dedicated source files rather than a single monolithic file, while preserving all exported types and ≥90% statement coverage.

#### Scenario: Service responsibilities separated by file
- **WHEN** inspecting `internal/service/` source files
- **THEN** `project_service.go`, `repo_url_validation.go`, `run_service.go`, `webhook_service.go`, and `var_store.go` each contain their respective domain logic and `go test ./internal/service/...` passes with ≥90% coverage
