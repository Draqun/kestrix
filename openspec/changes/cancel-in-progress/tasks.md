# Tasks

## 1. Domain Model and HCL Parser

- [ ] 1.1 Add `CancelInProgress bool` to `domain.Project` and update HCL parsing in `internal/parser/hcl_parser.go` with unit tests covering true, false, omitted, and invalid non-boolean types.
- [ ] 1.2 Verify `go test ./internal/parser/ -v` passes for all project block parsing cases.

## 2. Store Layer Lookup

- [ ] 2.1 Add `FindActiveRunsByRef(projectName, ref string) ([]int, error)` to `domain.RunStore` and implement in `internal/store/pgstore_run.go` querying non-terminal runs (`running`, `queued`) matching the primary source ref.
- [ ] 2.2 Add unit and integration tests for `FindActiveRunsByRef` in `internal/store/` asserting that only matching ref and active runs are returned.

## 3. Service Layer Auto-Cancellation

- [ ] 3.1 In `RunService.Start`, check if `proj.CancelInProgress` is enabled and a primary source ref is present; invoke `FindActiveRunsByRef` and call `s.Cancel(runID, "system:cancel-in-progress")` for each superseded run before slot acquisition.
- [ ] 3.2 Ensure canceled runs record `canceled_by: "system:cancel-in-progress"` and transition appropriately in `local`, `docker`, and `remote` modes.
- [ ] 3.3 Add unit tests in `internal/service/` verifying that in-flight and queued runs on the same branch are canceled, while runs on different branches remain untouched.

## 4. Webhook and E2E Integration

- [ ] 4.1 In `internal/web/handler_webhook.go`, verify webhook deliveries on consecutive pushes to the same branch trigger auto-cancellation when `cancel_in_progress = true`.
- [ ] 4.2 Add integration tests in `internal/web/` asserting that consecutive pushes cancel earlier runs and only the newest run completes.
- [ ] 4.3 Update documentation in `docs/` and add changelog fragment via `make changelog-new`.
