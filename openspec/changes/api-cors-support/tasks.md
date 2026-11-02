# Tasks: api-cors-support

## 1. Configuration & Middleware Implementation

- [ ] 1.1 In `internal/config/config.go`, add `HTTP.CORSAllowedOrigins` (`KESTRIX_HTTP_CORS_ALLOWED_ORIGINS`); verify unit test parses comma-separated origins.
- [ ] 1.2 In `internal/web/server.go`, register CORS middleware on `/api/*` routes applying origin matching, allowed methods, and preflight handling; verify with handler test.

## 2. Verification

- [ ] 2.1 Write integration tests in `internal/web/cors_test.go` verifying preflight `OPTIONS` and simple `GET` requests with valid and invalid `Origin` headers.
- [ ] 2.2 Verify `go test ./internal/web/...` passes with 100% statement coverage.

## Workflow follow-up

- Archive change via `openspec archive api-cors-support -y` upon implementation.
