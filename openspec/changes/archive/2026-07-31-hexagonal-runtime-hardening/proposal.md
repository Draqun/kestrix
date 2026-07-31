## Why

The hexagonal refactor removed the biggest architecture smells, but a few runtime hardening decisions were made afterward and need to be tracked in OpenSpec too: the composition root now lives in `cmd/kestrix/main.go`, `RunService` no longer self-heals missing dependencies at runtime, and webhook logging is kept wired instead of being silently dropped.

## What Changes

- Keep the composition root in `cmd/kestrix/main.go`
- Make `RunService` require explicit dependencies instead of fallback wiring
- Keep webhook logging wired through the server so logs still flow to the sink
- Make webhook log processing nil-safe instead of failing at runtime
- Preserve the HTTP adapter as a thin transport layer

## Non-goals

- No auth work
- No new product behavior
- No Docker/runtime changes
- No runner coverage work
