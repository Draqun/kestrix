# Tasks: service-and-ui-modularization

## 1. Modularize `internal/service`

- [x] 1.1 Extract `ProjectService` and URL/SSRF validation helpers from `internal/service/service.go` into `internal/service/project_service.go` and `internal/service/repo_url_validation.go`; verify `go test ./internal/service/...` passes.
- [x] 1.2 Extract `RunService`, `WebhookService`, and `BuildVarStore` into `internal/service/run_service.go`, `internal/service/webhook_service.go`, and `internal/service/var_store.go`, removing `internal/service/service.go`; verify `go test ./internal/service/ -coverprofile=coverage.out` maintains ≥90% (and 100% existing) coverage.

## 2. Modularize `ui/src/pages/Pipelines.jsx`

- [x] 2.1 Extract run detail header/action controls and task log/manual-gate panel from `ui/src/pages/Pipelines.jsx` into subcomponents under `ui/src/components/pipelines/`, ensuring all hooks follow Rules of Hooks.
- [x] 2.2 Run `cd ui && npm test && npm run build` to verify all frontend unit tests and production build succeed.
