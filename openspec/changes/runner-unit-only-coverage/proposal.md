## Why

`internal/runner` still contains execution-oriented code that is hard to measure with unit coverage alone. The current test suite relies on Docker-dependent paths for several branches, while the desired metric is strict unit coverage only. We want the runner package to become easier to exercise with pure Go tests, without counting integration or e2e coverage.

## What Changes

- Add a change focused on **unit-only testability** of `internal/runner`
- Cover pure orchestration branches with fake executors / test doubles
- Keep Docker, bash, and gitlab-runner execution paths out of the coverage target
- Preserve existing runtime behavior while making the non-external logic easier to unit test
- Add/extend tests around task ordering, stage assignment, dependency handling, and mode dispatch

## Non-goals

- No Docker-backed integration tests
- No e2e coverage credit
- No change to runner runtime semantics unless needed to isolate pure logic
- No new product feature
- No auth work
