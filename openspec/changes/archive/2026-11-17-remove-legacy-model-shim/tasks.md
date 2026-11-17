# Tasks: remove-legacy-model-shim

## 1. Migrate Core Packages (`dag`, `parser`, `runner`, `variables`, `store`)

- [x] 1.1 Replace `kestrix/internal/model` imports and `model.*` references with `kestrix/internal/domain` and `domain.*` in `internal/dag/`, `internal/parser/`, `internal/runner/`, `internal/variables/`, and `internal/store/`; verify with `go test ./internal/dag/... ./internal/parser/... ./internal/runner/... ./internal/variables/... ./internal/store/...`.

## 2. Migrate Application & Entry-Point Packages (`service`, `web`, `cmd`)

- [x] 2.1 Replace `kestrix/internal/model` imports and `model.*` references with `kestrix/internal/domain` and `domain.*` in `internal/service/`, `internal/web/`, and `cmd/kestrix/`; verify with `go test ./internal/service/... ./internal/web/... ./cmd/...`.

## 3. Remove Shim Package & Enforce Boundary

- [x] 3.1 Delete `internal/model/model.go` and extend `internal/domain/portscheck/portscheck_test.go` to fail if any package imports `kestrix/internal/model`.
- [x] 3.2 Update `docs/architecture.md` and `docs/be.md` to reference `internal/domain` instead of `internal/model`, regenerate Swagger docs if needed, and run `go test ./... -count=1 -short`.
