# ci-lint-fixes Specification

## Purpose
TBD - created by archiving change fix-fmt-check-staticcheck-ci. Update Purpose after archive.
## Requirements
### Requirement: fmt-check excludes vendored and cache directories
The `make fmt-check` target SHALL only check Go source files in the project (excluding downloaded dependencies, module cache, and build artifacts), and SHALL pass when all project files are properly formatted.

#### Scenario: fmt-check with unformatted files
- **WHEN** a project Go source file has incorrect formatting
- **THEN** `make fmt-check` SHALL fail and list that file

#### Scenario: fmt-check ignores cache directories
- **WHEN** `make fmt-check` runs and `.gomodcache/`, `.gocache/`, `.gopath/`, `node_modules/`, `ui/dist/` exist
- **THEN** files in those directories SHALL NOT be checked
- **AND** the target SHALL NOT report false positives from third-party code

### Requirement: staticcheck target works with local toolchain
The `make staticcheck` target SHALL set `GOTOOLCHAIN=local` to prevent toolchain switching, allowing staticcheck to run with the locally installed Go version.

#### Scenario: staticcheck with GOSUMDB=off
- **WHEN** `make staticcheck` runs with `GOSUMDB=off` set in `GOENV`
- **THEN** the target SHALL set `GOTOOLCHAIN=local` to avoid toolchain download failures
- **AND** staticcheck SHALL execute successfully using the local Go 1.25.0 toolchain

### Requirement: Go source files are gofmt-compliant
All Go source files in `cmd/` and `internal/` SHALL be formatted with `gofmt -w` and pass `gofmt -l` checks.

#### Scenario: All project files formatted
- **WHEN** `make fmt-check` runs after all files have been formatted
- **THEN** the target SHALL exit with code 0

### Requirement: staticcheck warnings are resolved
All staticcheck warnings in project source SHALL be resolved to zero warnings.

#### Scenario: No empty critical sections
- **WHEN** `make staticcheck` runs
- **THEN** there SHALL be no SA2001 (empty critical section) warnings in `internal/mocks/mocks.go`

#### Scenario: No unused struct fields
- **WHEN** `make staticcheck` runs
- **THEN** there SHALL be no U1000 (unused field) warnings in `internal/web/server.go`

#### Scenario: No unnecessary struct literal conversions
- **WHEN** `make staticcheck` runs
- **THEN** there SHALL be no S1016 warnings in `internal/web/handler_run.go`

