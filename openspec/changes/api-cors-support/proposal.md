# Proposal: api-cors-support

## Why

Currently, Kestrix provides no Cross-Origin Resource Sharing (CORS) handling. While the default React frontend is served from the same origin as the backend API on `:18080` (making lack of CORS secure by default), operators wishing to integrate external developer portals, micro-frontends, browser extensions, or custom monitoring dashboards from distinct origins are blocked by browser CORS enforcement (`No 'Access-Control-Allow-Origin' header is present on the requested resource`).

## What Changes

- Add an opt-in configuration setting: `KESTRIX_HTTP_CORS_ALLOWED_ORIGINS` (comma-separated list of origins, default empty/disabled).
- When configured, register a CORS middleware on the Chi router allowing specified origins, standard methods (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`), and required headers (`Authorization`, `Content-Type`, `X-Kestrix-Request`).
- Automatically respond to preflight `OPTIONS` requests for all API routes with appropriate headers.
- Keep CORS strictly disabled by default to maintain existing origin security.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `http-server-hardening`: defines origin validation and preflight handling when CORS is enabled.

## Non-goals

- Allowing wildcard (`*`) origins when credentials/cookies are enabled.
- Applying CORS headers to internal runner poll routes (`/api/runner-jobs/*`).

## Impact

- Modifies `internal/config/config.go` and `internal/web/server.go`.
