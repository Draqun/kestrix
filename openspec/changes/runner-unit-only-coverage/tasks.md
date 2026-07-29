## 1. Baseline the unit-only gap

- [ ] 1.1 Run `go test ./internal/runner -coverprofile=/tmp/runner.cover -covermode=atomic`
- [ ] 1.2 List uncovered statements from `/tmp/runner.cover`
- [ ] 1.3 Classify each uncovered branch as pure logic, orchestration, or external execution

## 2. Cover pure helper logic

- [ ] 2.1 Add missing unit tests for pure helpers in `internal/runner/runner_test.go`
- [ ] 2.2 Cover any uncovered error branches in `order`, `assignStage`, or `repoDir`
- [ ] 2.3 Re-run runner coverage and confirm the pure helper surface is complete

## 3. Cover orchestration with fakes

- [ ] 3.1 Add fake executor / command-runner seams only if required
- [ ] 3.2 Add unit tests for task ordering, dependency skips, manual skips, and callbacks
- [ ] 3.3 Add unit tests for `PipelineExecutor` mode dispatch and nil/error branches

## 4. Verify

- [ ] 4.1 `go test ./internal/runner -count=1` passes
- [ ] 4.2 `go test ./... -count=1` passes
- [ ] 4.3 Record final unit-only runner coverage in the change notes
