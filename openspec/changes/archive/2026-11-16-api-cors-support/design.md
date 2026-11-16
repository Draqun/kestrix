# Design: api-cors-support

## Context

Kestrix uses Chi (`github.com/go-chi/chi/v5`). Currently, all `/api/*` endpoints are registered directly without CORS middleware. When the web UI is embedded and served statically from `:18080`, browsers treat it as same-origin. If deployed behind separate domains or accessed from third-party tools, browser clients fail preflight requests.

## Goals / Non-Goals

**Goals:**
- Provide secure, configurable CORS handling for API endpoints.
- Allow credentials (`Access-Control-Allow-Credentials: true`) when explicit origins are matched.

**Non-Goals:**
- Enabling wildcard `Access-Control-Allow-Origin: *` with credentials.

## Decisions

### D1: Use `cors.Handler` from standard library or `go-chi/cors`
- **Choice:** Implement a lightweight custom middleware or use `github.com/go-chi/cors` wired into `internal/web/server.go`.
- **Alternatives considered:** Static Nginx/Caddy proxy headers. Rejected because local development and container-only deployments need native support.

## Risks / Trade-offs

- **[Risk]** Misconfigured origins allowing credential theft via malicious third-party origins.  
  → *Mitigation:* Explicitly disallow `*` origin when credentials/session cookies are enabled.
