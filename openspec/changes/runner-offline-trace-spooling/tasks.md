# Tasks: runner-offline-trace-spooling

## 1. Trace Spool Buffer Core

- [ ] 1.1 Implement thread-safe in-memory `TraceSpool` with capacity limit and monotonic offset tracking; verify with unit tests in `cmd/kestrix/trace_spool_test.go`
- [ ] 1.2 Add truncation policy when buffered chunks exceed capacity limit; verify with unit test asserting truncation marker and byte calculation

## 2. Runner Integration and Retry Flusher

- [ ] 2.1 Integrate `TraceSpool` into `traceWriter` in `cmd/kestrix/runner.go`; verify chunks are queued on HTTP network errors instead of discarded
- [ ] 2.2 Implement background retry flusher with exponential backoff and jitter; verify retried chunks maintain contiguous sequential offsets
- [ ] 2.3 Handle HTTP 409 Conflict response by clearing spool and stopping runner loop; verify unit test asserting no further network retries on stale epoch
- [ ] 2.4 Add pre-completion flush in `reportComplete`; verify completion waits for spool to empty before sending `PUT /complete`

## 3. Configuration and Verification

- [ ] 3.1 Expose `KESTRIX_RUNNER_TRACE_SPOOL_MAX_BYTES` flag and env var in runner configuration; verify flag parsing tests pass
- [ ] 3.2 Add end-to-end simulation test with mock HTTP server returning transient failures during task trace streaming; verify all output arrives intact
- [ ] 3.3 Run `make test` and `make quality` to ensure full test suite passes and coverage thresholds are preserved
