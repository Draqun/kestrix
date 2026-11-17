# Tasks: swagger-api-completeness

## 1. Annotate Undocumented Handlers

- [ ] 1.1 Add swaggo godoc annotations to `handleRunProject`, `handleCancelRun`, and `handleApproveManualTask` in `internal/web/handler_run.go`, `handleGetProjectDefinition` in `internal/web/handler_project.go`, and `handleVersion` in `internal/web/handler_system.go`; verify with `swag init -g cmd/kestrix/main.go -o docs`.
- [ ] 1.2 Add swaggo godoc annotations to `handleCreateCredential`, `handleListCredentials`, `handleGetCredential`, `handleRotateCredential`, and `handleRevokeCredential` in `internal/web/handler_credentials.go` and `handleRevokeRunner` in `internal/web/handler_runner_jobs.go`; verify with `swag init -g cmd/kestrix/main.go -o docs`.
- [ ] 1.3 Add swaggo godoc annotations to `handleRunnerJobNext`, `handleRunnerJobTrace`, `handleRunnerJobComplete`, and `handleRunnerJobArtifacts` in `internal/web/handler_runner_jobs.go`; regenerate `docs/swagger.yaml`, `docs/swagger.json`, and `docs/docs.go`.

## 2. Route Parity Enforcement & Verification

- [ ] 2.1 Add a route-parity test in `docs/docs_test.go` verifying that every `/api/*` and `/webhook` route and HTTP method registered in `internal/web/server.go` is present in `docs/swagger.yaml`.
- [ ] 2.2 Run `go test ./docs/... ./internal/web/... -count=1` and verify all tests pass with zero undocumented routes.
