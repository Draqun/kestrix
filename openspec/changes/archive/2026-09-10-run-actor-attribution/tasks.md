## 1. Domain types and ports

- [x] 1.1 Add `StartedBy string \`json:"started_by"\`` and `CanceledBy string \`json:"canceled_by,omitempty"\`` to `domain.Run` (`internal/domain/ports.go:317`), `Actor string` to `domain.RunnerOptions` (`ports.go:172`), and `ApprovedBy string \`json:"approved_by,omitempty"\`` plus `ApprovedAt *time.Time \`json:"approved_at,omitempty"\`` to `domain.TaskResult` (`internal/domain/types.go:162`); verify `go build ./...` compiles and a domain test asserts a `TaskResult` without approval marshals to JSON with neither key present.
- [x] 1.2 Change the `RunStore` port to `CreateRun(projectName string, changed []string, includeManual bool, snap *Project, startedBy string)` and `CancelRun(id int, canceledBy string) error` (`ports.go:132`, `:136`), update `internal/mocks/mocks.go` (`MockRunStore.CreateRun`/`CancelRun`, recording the actor in `createRunCall` and a new `cancelRunCall`), and verify `go vet ./...` and `go test ./internal/domain/... ./internal/mocks/...` pass with the compile-time port assertions intact.

## 2. Store

- [x] 2.1 Add `{"add run started_by", "ALTER TABLE runs ADD COLUMN IF NOT EXISTS started_by TEXT NOT NULL DEFAULT ''"}` and the matching `canceled_by` entry to the `statements` slice (`internal/store/pgstore.go:144`), and verify the integration test that opens a schema created by the previous `CREATE TABLE` block ends with both columns present and existing rows reading `''`.
- [x] 2.2 Write `started_by` in `PGStore.CreateRun` (`pgstore_run.go:13`) and read `started_by`, `canceled_by` in `GetRun`, `ListRuns`, and `ListAllRuns` (`:229`, `:269`, `:312`); verify an integration test round-trips a run created with `user:alice` through all three readers.
- [x] 2.3 Write `canceled_by` in `PGStore.CancelRun` (`pgstore_run.go:85`) inside the same `UPDATE` that sets `status = 'canceled'`, and verify integration tests assert (a) a `running` run gets `canceled_by` set, (b) a `passed` run is untouched including `canceled_by = ''`.
- [x] 2.4 Verify `ApplyTaskResult` (`pgstore_run.go:171`) needs no change: an integration test appends a `running` entry carrying `ApprovedBy`/`ApprovedAt`, then a `passed` entry carrying the same, and reads back both on the last entry via `GetRun`.

## 3. Service

- [x] 3.1 Pass `opts.Actor` from `RunService.Start` through `createRun` to `CreateRun` (`internal/service/service.go:164-193`) and log `run: started actor=%q project=%q run=%d`; verify a unit test with `MockRunStore` asserts the recorded `startedBy` and that the log line contains the actor and no other request data.
- [x] 3.2 In `RunTask` (`service.go:293`) stamp `ApprovedBy: opts.Actor, ApprovedAt: now` on the initial `running` entry when `opts.Actor != ""` and copy both onto the terminal entry written at `:336`; verify unit tests assert the terminal entry carries the same approver and timestamp, and that an empty `opts.Actor` leaves both fields zero.
- [x] 3.3 Log `run: approved actor=%q project=%q run=%d task=%q` in `ApproveManualTask` (`service.go:393`) only after the waiting/dependency checks pass; verify unit tests assert one log line on success and none when approval is refused, and that the refused path leaves `ApplyTaskResult` uncalled.
- [x] 3.4 Add `RunService.Cancel(runID int, actor string) error` that calls `runStore.CancelRun(runID, actor)` and logs `run: canceled actor=%q run=%d`; verify a unit test asserts the mock received the actor and a store error is returned unchanged.

## 4. HTTP handlers

- [x] 4.1 Add `actorFrom(r *http.Request) string` in `internal/web/handler_helpers.go` returning `"user:" + claims.UserName` from `claimsFromContext`, or `""` without claims; verify a unit test covers both branches.
- [x] 4.2 Set `Actor: actorFrom(r)` in the `RunnerOptions` built by `handleRunProject`, `handleRunTask`, and `handleApproveManualTask` (`internal/web/handler_run.go:56`, `:125`, `:283`), and verify handler tests with an authenticated request assert the service received `user:<name>` for each of the three routes.
- [x] 4.3 Switch `handleCancelRun` (`handler_run.go:305`) to `s.runService.Cancel(runID, actorFrom(r))`, keeping the `sourceSessions.Release` call and the `500` on error; verify handler tests assert the actor reaches the service and the `run service not configured` branch answers `500` like its siblings.
- [x] 4.4 Add a handler test asserting a start request whose JSON body contains `"started_by":"user:mallory"` and an `X-Actor` header still records the authenticated account, and verify `go test ./internal/web/ -run Actor -count=1` passes.
- [x] 4.5 Run `make swagger` and verify `docs/swagger.json` lists `started_by`, `canceled_by`, `approved_by`, and `approved_at` on the run and task-result schemas.

## 5. Frontend

- [x] 5.1 In the run detail header (`ui/src/pages/Pipelines.jsx:203`) render `by <started_by>` after the start time and `canceled by <canceled_by>` when present, using a small `actorLabel(s)` helper that maps `''`/undefined to `unknown` and strips the `user:` prefix for display; verify a jsdom component test renders `by alice` for `user:alice` and `unknown` for an empty value.
- [x] 5.2 Show `approved by <approved_by>` and the formatted `approved_at` on a task row/detail whose latest result carries them (`Pipelines.jsx:174-250`), and verify a component test renders it for an approved task and nothing for an unapproved one.
- [x] 5.3 Add `by <started_by>` to each row of the runs list (`Pipelines.jsx:509`), and verify a component test renders it and `npm run lint` passes.

## 6. Documentation

- [x] 6.1 Add an "Attribution" section to `docs/authentication.md` (or a short `docs/run-attribution.md` linked from `docs/overview.md`) describing the actor string, the three recorded actions, `unknown` for legacy runs, and that deleting an account keeps the history; verify the page is included by `make ui-build`'s docs copy.
- [x] 6.2 Add a CHANGELOG `Unreleased → Added` entry naming the three fields, the two new columns (added automatically at startup), and the log lines; verify it states that no manual migration step is needed.

## 7. Verification

- [x] 7.1 Run `make quality` and verify it passes with `internal/web` and `internal/service` at 100% and `internal/domain` ≥ 90%.
- [x] 7.2 Run `make test-integration` against `kestrix_test` and verify the schema statements apply to both a fresh database and one created by the previous binary.
- [x] 7.3 Run `make ui-test` and verify the new `Pipelines.jsx` assertions pass and existing thresholds hold.
- [x] 7.4 Run `npx -y @fission-ai/openspec@latest validate run-actor-attribution --strict` and verify it passes.
