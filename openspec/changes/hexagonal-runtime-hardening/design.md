## Context

The repository already follows the main hexagonal split: `domain` for ports, `service` for use cases, `web` for HTTP adapters, and `cmd/kestrix` for wiring. The remaining work is runtime hardening: ensure the wiring stays explicit, avoid runtime fallback paths, and keep webhook logging connected.

## Goals / Non-Goals

**Goals:**
- Composition root stays in `cmd/kestrix/main.go`
- `web` stays a thin adapter and does not repair missing dependencies at runtime
- `RunService` validates required dependencies explicitly
- Webhook logging remains wired and nil-safe

**Non-Goals:**
- New features
- Auth
- Runner coverage changes
- Database schema changes

## Risks / Trade-offs

- Overly defensive fallback wiring can hide misconfiguration; this change prefers explicit failure.
- Nil-safety must not silently swallow real errors from the webhook path.
- The design should remain compatible with the existing hexagonal structure.
