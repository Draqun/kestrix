## Context

The Kestrix Makefile uses `GOENV` with `GOSUMDB=off` for self-contained builds (no external checksum database). This works for building and testing, but causes two problems:

1. **fmt-check**: `gofmt -l .` walks ALL directories including `.gomodcache/` which contains thousands of third-party files. This produces false positives and fails CI.

2. **staticcheck**: The `staticcheck` tool (v0.7.0) requires Go >= 1.25.0 and triggers a toolchain switch to go1.25.12. With `GOSUMDB=off`, the toolchain download fails because checksum verification is disabled but the toolchain module still needs verification.

Additionally, 4 legitimate staticcheck warnings exist in the codebase.

## Goals / Non-Goals

**Goals:**
- Make `make fmt-check` pass without false positives
- Make `make staticcheck` pass with zero warnings
- Keep `GOENV` with `GOSUMDB=off` for build/test targets (needed for self-contained builds)

**Non-Goals:**
- Remove `GOSUMDB=off` from the general `GOENV` (it's needed for builds in restricted environments)
- Pin staticcheck to an older version (we want latest compatibility)
- Introduce new linting tools

## Decisions

### Decision 1: Exclude vendored/cache directories from fmt-check

**Choice**: Use `gofmt -l $(shell find . -name '*.go' -not -path './.gomodcache/*' -not -path './.gocache/*' -not -path './.gopath/*' -not -path './node_modules/*' -not -path './ui/dist/*')`

**Rationale**: `gofmt -l .` is too broad. Using `find` with exclusion patterns is the standard approach in Go projects with local Go module caches.

**Alternative**: Use `.gitignore`-aware tools. Rejected because `gofmt` doesn't respect `.gitignore`.

### Decision 2: Use GONOSUMCHECK for toolchain downloads

**Choice**: For `staticcheck` target, use `GONOSUMCHECK='*'` or `GOSUMDB=off GOFLAGS=-mod=mod` to allow toolchain downloads without checksum verification.

Actually, the real issue is that `GOSUMDB=off` already disables verification, but the toolchain download mechanism still tries to verify. The fix is to set `GOTOOLCHAIN=local` to prevent toolchain switching, OR install a compatible staticcheck version.

**Final approach**: Pin `staticcheck` to a version compatible with Go 1.25.0 (the installed version), or set `GOTOOLCHAIN=local` to use the local Go instead of downloading a new toolchain.

**Decision**: Set `GOTOOLCHAIN=local` in the `staticcheck` target's environment. This prevents the tool from trying to switch to a newer Go version.

### Decision 3: Fix staticcheck warnings (4 items)

1. **`internal/mocks/mocks.go:164,181` (SA2001)**: Empty critical section — the `sync.Mutex` lock/unlock pair protects nothing. Remove the locks or add the intended operation between them.

2. **`internal/web/handler_run.go:459` (S1016)**: Convert `runner.TaskResult` to `domain.TaskResult` via type conversion instead of field-by-field struct literal, IF the types are compatible. Otherwise, keep the literal but add a `//nolint` comment.

3. **`internal/web/server.go:70` (U1000)**: `runService` field is unused. Either remove it or mark it with `//nolint` if it's intended for future use.

## Risks / Trade-offs

| Risk | Mitigation |
|------|------------|
| `GOTOOLCHAIN=local` may fail if local Go is too old for staticcheck | Pin staticcheck version in Makefile |
| Removing `runService` field may break future code | Check if it's used implicitly |
| Changing mock locking may affect test concurrency | Run tests with `-race` flag |

## Migration Plan

1. Fix Makefile targets
2. Run `gofmt -w` on affected files
3. Fix staticcheck warnings
4. Verify both targets pass locally
5. Push and verify GitLab CI passes
