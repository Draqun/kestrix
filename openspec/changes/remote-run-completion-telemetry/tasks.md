# Tasks: remote-run-completion-telemetry

## 1. Remote Completion Telemetry Integration

- [x] 1.1 In `internal/service/remote_dispatch.go` (`resumeRemoteRun`), invoke `recordRunFinished` with calculated duration when a remote run reaches status `passed` or `failed`; verify with a unit test that the Prometheus counter and duration histogram are incremented upon the last job's completion.
- [x] 1.2 In `internal/service/remote_dispatch.go`, ensure that early aborts (e.g. initial dispatch failure or run cancellation) invoke `recordRunFinished` exactly once; verify with a cancellation unit test.

## 2. Verification

- [x] 2.1 Run `go test ./internal/service -run TestRemoteRunTelemetry -v` and assert 100% statement coverage.
- [x] 2.2 Verify full test suite passes with `make test`.

## Workflow follow-up

- Archive change via `openspec archive remote-run-completion-telemetry -y` upon implementation.
