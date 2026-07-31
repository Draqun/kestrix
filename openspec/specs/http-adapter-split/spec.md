# http-adapter-split Specification

## Purpose
TBD - created by archiving change hexagonal-refactor. Update Purpose after archive.
## Requirements
### Requirement: Handler files split by domain group
`server.go` SHALL be decomposed into per-group files under `internal/web/handlers/`: `project.go`, `run.go`, `pipeline.go`, `webhook.go`, `system.go`.

#### Scenario: server.go under 300 lines
- **WHEN** counting lines in `server.go` after refactor
- **THEN** it is under 300 lines (wiring only: `NewServer`, `buildRouter`, `ListenAndServe`)

### Requirement: Handlers are thin HTTP adapters
Each handler SHALL only: decode request, call service, encode response. Business logic SHALL live in `service`.

#### Scenario: Handler has no business logic
- **WHEN** a handler needs to validate input or orchestrate a run
- **THEN** it delegates to `ProjectService` or `RunService` rather than doing it inline

### Requirement: writeError helper replaces inline error JSON
A single `writeError(w http.ResponseWriter, code int, msg string)` helper SHALL replace all inline `http.Error(w, fmt.Sprintf(...JSON...), code)` calls.

#### Scenario: Error response uses helper
- **WHEN** any handler returns an error
- **THEN** `writeError` is called instead of inline `http.Error`

### Requirement: webhookLog protected by mutex
`Server.webhookLog` SHALL be accessed under a `sync.RWMutex`. Appends use write lock; reads use read lock.

#### Scenario: Concurrent webhook append is safe
- **WHEN** two webhook requests arrive simultaneously
- **THEN** both appends complete without data race (`go test -race` passes)

### Requirement: Pagination extracted to package
`parsePagination`, `buildPaginationLinks`, and `paginationMeta` SHALL live in `internal/web/pagination` package.

#### Scenario: Pagination is reusable
- **WHEN** a handler needs pagination
- **THEN** it imports `pagination.Parse` and `pagination.Links` instead of defining them inline

### Requirement: Handler tests cover error branches
Each handler file SHALL have tests covering happy path and each error branch (invalid JSON, missing param, store error, service error).

#### Scenario: Handler coverage ≥85%
- **WHEN** running `go test ./internal/web/handlers/ -coverprofile=coverage.out`
- **THEN** each handler file has ≥85% statement coverage

### Requirement: CI pipeline enforces coverage threshold
The CI pipeline SHALL run `go test ./... -coverprofile=coverage.out -covermode=atomic -race -count=1` and fail if total coverage is below 80%.

#### Scenario: Coverage below threshold fails CI
- **WHEN** total coverage is below 80%
- **THEN** the CI pipeline exits non-zero and the MR is blocked

#### Scenario: Coverage report available in MR
- **WHEN** the CI test stage completes
- **THEN** a coverage report artifact is available for GitLab MR coverage display

