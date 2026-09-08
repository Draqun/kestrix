## Why

`POST /webhook` is the only unauthenticated write path into Kestrix, and it
verifies a delivery only when a global secret happens to be set. Reviewing the
ingress found five holes that let an anonymous caller drive pipeline
resolution, Git fetches, and GitLab dispatch, or tie up the process with a slow
or oversized request.

- `internal/web/handler_webhook.go:55` skips verification when
  `KESTRIX_WEBHOOK_SECRET` is empty, and both Compose files default it to the
  literal `dev-secret`. `projects.webhook_secret` is stored
  (`pgstore_project.go:42`) but never compared, so the per-project secret is a
  field, not a control.
- `handler_webhook.go:56` compares with `!=`, not constant time, and only
  honours the `gitlab` header; `KESTRIX_WEBHOOK_METHOD=hmac` is accepted by
  config and ignored. The middleware in `webhook_verify.go` is mounted nowhere.
- `GET /api/projects` and `GET /api/projects/{name}` encode
  `domain.ProjectConfig` verbatim (`handler_project.go:140,317`), so every
  `viewer` reads every project's webhook secret in plaintext.
- `internal/web/server.go:402` is a bare `http.ListenAndServe`: no timeouts and
  no body cap; `io.ReadAll` at `handler_webhook.go:62` and ten
  `json.NewDecoder(r.Body)` sites read whatever arrives.
- `validateProject` (`internal/service/service.go:25`) rejects embedded
  credentials but accepts any scheme and host, so `file:///`, `http://`, or a
  loopback / link-local `repo_url` turns source acquisition into a filesystem
  or metadata-endpoint probe.

## What Changes

- **BREAKING**: webhook verification fails closed. A delivery is accepted only
  if it verifies against the matched project's own `webhook_secret` or, failing
  that, the global `KESTRIX_WEBHOOK_SECRET`. With neither available it is
  rejected with `401` and, per `webhook-log-history`, never recorded.
  Deployments with an empty or `dev-secret` secret stop accepting deliveries
  until a real one is set.
- Per-project secrets become real: verified per delivery, constant time, with
  the configured method (`gitlab` header or `hmac` body signature), and
  write-only on the project API - reads carry `has_webhook_secret`, never the
  value.
- The HTTP server gains read, write, and idle timeouts and request-body caps;
  an oversized body answers `413`. Webhook and API bodies have separate bounds.
- `repo_url` is restricted to `https://`, `ssh://`, and `git@host:path`; other
  schemes, plaintext `http://`, and loopback or link-local hosts are refused
  with `400` before any network access.

## Non-goals

- Webhook formats beyond GitLab's header and the GitHub-style HMAC signature
  already modelled by `KESTRIX_WEBHOOK_METHOD`.
- Clearing a per-project secret through the API (rotate by setting a new one).
- Blocking RFC 1918 addresses: self-hosted Git servers live there.
- DNS resolution or egress filtering at fetch time; the host check is
  registration-time and syntactic (design D6).
- Rate limiting `/webhook` per client.

## Capabilities

### New Capabilities

- `http-server-hardening`: connection timeouts and request-body bounds on the
  HTTP listener, with `413` for an oversized body.

### Modified Capabilities

- `webhook-receiver`: "HTTP Endpoint" - fail-closed, per-project, constant-time
  verification before the payload is trusted.
- `project-registration`: REQ-1 and REQ-5 - `repo_url` scheme and host
  restrictions; `webhook_secret` write-only with `has_webhook_secret` on reads.

## Impact

- `internal/web/handler_webhook.go`, `webhook_verify.go`: verification rebuilt
  around a once-read, size-capped body and a project-secret lookup.
- `internal/web/server.go`: `http.Server` with timeouts; one body-cap
  middleware and one shared JSON decode helper replace ten raw decoders.
- `internal/service/service.go`: URL scheme and host validation.
- `internal/domain/ports.go`, `handler_project.go`: `webhook_secret` out of
  responses, `has_webhook_secret` in.
- `internal/config/environment.go`, `.env.template`, Compose: `KESTRIX_HTTP_*`
  knobs; `dev-secret` default removed. `internal/web` stays at 100% coverage.
