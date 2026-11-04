# Tasks: task-timeout

## 1. Domain Model and HCL Parser

- [x] 1.1 Add `Timeout` field and `ParsedDuration()` helper to `domain.Task`; verify unit tests in `internal/domain`
- [x] 1.2 Update HCL parser in `internal/parser/hcl_parser.go` to decode and validate optional `timeout` attribute in `ci_task`; verify parser unit tests reject invalid formats and accept valid duration strings

## 2. In-Process Executors (Local & Docker)

- [x] 2.1 Update LocalExecutor in `internal/runner/runner.go` to apply `context.WithTimeout`; verify timed-out task process is killed and reported `failed`
- [x] 2.2 Update DockerExecutor in `internal/runner/runner.go` to stop container on context timeout; verify container is stopped and reported `failed`

## 3. Remote Runner Mode

- [x] 3.1 Carry the declared timeout to remote runners via `Task.Timeout` in `RemoteJobPayload`
- [x] 3.2 Update remote runner execution loop in `cmd/kestrix/runner.go` to enforce task timeout via `context.WithTimeout`; verify runner unit tests assert timeout termination and error reporting

## 4. Verification and Documentation

- [x] 4.1 Add end-to-end integration test verifying downstream tasks transition to `skipped` when an upstream task times out
- [x] 4.2 Document `timeout` attribute in `docs/pipeline-definition.md` with examples
- [x] 4.3 Run `make test` and `make quality` to ensure all tests pass and coverage gates hold
