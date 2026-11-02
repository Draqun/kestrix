# Design: service-level-local-runner-guard

## Context

`RunService.CheckLocalAllowed(proj)` checks whether a project's resolved mode is `local` and if `runner.allow_local` is set. In MR !73, this helper was called inside `internal/web/handler_run.go` before invoking `s.Start`. In `internal/service/service.go`, dozens of test suites construct a `RunService` with a zero-value `Config` (where `allow_local` defaults to `false`). Moving the check into `Start` broke those test cases during MR !73, leading to the deferral of service-level enforcement.

## Goals / Non-Goals

**Goals:**
- Enforce the `allow_local` policy boundary directly within `RunService` port implementation methods.
- Provide a standard test helper (`newTestRunServiceWithLocalAllowed`) to cleanly update existing test fixtures.

**Non-Goals:**
- Changing default configuration (`allow_local` remains `false` by default).

## Decisions

### D1: Enforce in `Start`, `StartTask`, and `ApproveManualTask`
- **Choice:** Call `s.CheckLocalAllowed(proj)` immediately after validating project configuration in all three service entry points.
- **Alternatives considered:** Calling it inside `PipelineExecutor`. Rejected because source acquisition (git clone) happens before `PipelineExecutor`, and unallowed runs should not fetch sources.

### D2: Standardize test service constructor
- **Choice:** Introduce `newTestRunService(t *testing.T, opts ...RunServiceOption)` that defaults `allow_local = true` for unit tests testing local pipelines.
- **Alternatives considered:** Manually updating all 76 call sites with `cfg.Runner.AllowLocal = true`. Using a helper eliminates future boilerplate.

## Risks / Trade-offs

- **[Risk]** Large diff across test files.  
  → *Mitigation:* The change is purely mechanical across test setups, with zero runtime risk to production (where handlers already reject disallowed calls).
