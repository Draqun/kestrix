# Design: swagger-api-completeness

## Context

Kestrix uses `swaggo/swag` (`cmd/kestrix/main.go` and `internal/web/*.go`) to generate `docs/swagger.yaml`, `docs/swagger.json`, and `docs/docs.go`, which are served at `/docs/`. While `docs/docs_test.go` verifies that the Swagger spec parses and contains a few sample paths, 15 routes wired in `internal/web/server.go` were added without swaggo godoc comments.

```
+---------------------------+       swag init        +-------------------------+
| internal/web/*.go         | ---------------------> | docs/swagger.yaml       |
| - handler_run.go          |                        | docs/swagger.json       |
| - handler_project.go      |                        | docs/docs.go            |
| - handler_system.go       |                        +------------+------------+
| - handler_credentials.go  |                                     |
| - handler_runner_jobs.go  |                                     |
+-------------+-------------+                                     |
              |                                                   v
              |             docs/docs_test.go        +-------------------------+
              +------------------------------------> | Route Parity Assertion  |
                   compare Chi routes vs Swagger     | (fails on missing path) |
                                                     +-------------------------+
```

## Goals / Non-Goals

**Goals:**
- Document 100% of `/api/*` and `/webhook` HTTP endpoints in `docs/swagger.yaml` and `docs/swagger.json`.
- Enforce route parity automatically during `go test ./...` so future endpoints cannot omit OpenAPI annotations.

**Non-Goals:**
- Replacing `swaggo/swag` with a code-first router wrapper.
- Altering HTTP handler signatures or JSON payloads.

## Security Considerations

- Credential management endpoints (`/api/credentials*`) must document only metadata/write-only secret input structs (`credentialInput`, `credentialView`) and never expose encrypted material or raw secret values in response schemas.
- Runner poll endpoints (`/api/runner-jobs/*`) must clearly document `Authorization: Bearer <runner-token>` header requirements as distinct from control-plane user JWT/API tokens.

## Decisions

### D1: Automated Chi-to-Swagger route parity test in `docs/docs_test.go`
- **Choice:** Parse `internal/web/server.go` (or walk the constructed Chi router via `chi.Walk`) in a test and assert every `(method, path)` under `/api/*` and `/webhook` exists in `docs/swagger.yaml`.
- **Alternatives considered:** Relying on manual code review. Rejected because 14 routes already drifted across multiple MRs.

### D2: Keep Swagger annotations on handler functions in `internal/web/`
- **Choice:** Annotate each handler directly above its function declaration using explicit request/response struct references.
- **Alternatives considered:** Centralizing dummy annotations in `cmd/kestrix/main.go`. Rejected because co-locating annotations with handlers keeps parameter and status-code changes synchronized.

## Risks / Trade-offs

- **[Risk]** Unexported request structs in `internal/web/` may require exported fields or swaggo struct resolution.
  → *Mitigation:* Verify `swag init -g cmd/kestrix/main.go -o docs` succeeds cleanly and inspect generated definitions in `docs/swagger.yaml`.
