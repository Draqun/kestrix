## Why

The GitLab CI pipeline fails on `fmt-check` and `staticcheck` jobs. The `fmt-check` target runs `gofmt -l .` which scans downloaded dependencies in `.gomodcache/` (hundreds of files), producing false positives. Additionally, several Go source files in `internal/` are not properly formatted. The `staticcheck` target fails because (1) `GOSUMDB=off` in `GOENV` blocks toolchain downloads needed by staticcheck, and (2) there are 4 legitimate staticcheck warnings in the codebase.

## What Changes

- **Fix `fmt-check` target**: Exclude `.gomodcache/`, `.gocache/`, `.gopath/`, `node_modules/`, and `ui/dist/` from `gofmt -l .`
- **Format all Go source files**: Run `gofmt -w` on all files in `internal/` and `cmd/`
- **Fix `staticcheck` target**: Allow toolchain downloads by not setting `GOSUMDB=off` for staticcheck invocation (or pin staticcheck version compatible with Go 1.25.0)
- **Fix 4 staticcheck warnings**:
  - `internal/mocks/mocks.go:164,181` — empty critical section SA2001
  - `internal/web/handler_run.go:459` — use type conversion instead of struct literal S1016
  - `internal/web/server.go:70` — unused field U1000

## Capabilities

### New Capabilities
- None (this is a CI/lint fix, not a feature)

### Modified Capabilities
- None (no spec-level behavior changes)

## Impact

- **Build system**: `Makefile` `fmt-check` and `staticcheck` targets
- **Go source files**: `internal/mocks/mocks.go`, `internal/web/handler_run.go`, `internal/web/server.go`, plus ~15 files needing formatting
- **CI pipeline**: GitLab CI `fmt-check` and `staticcheck` jobs will pass

## Non-goals

- Refactoring the codebase beyond what staticcheck requires
- Changing the `GOENV` configuration (needed for self-contained builds)
- Adding new lint rules
