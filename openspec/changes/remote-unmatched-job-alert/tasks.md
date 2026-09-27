## 1. Detection

- [ ] 1.1 Store method `UnmatchedPendingJobs(runID *int)` (design D1) on the remote job store port. Integration tests:
  - tagged job with no matching runner;
  - matched by a superset runner;
  - matching runner revoked;
  - untagged job with zero runners, and with one runner;
  - claimed jobs excluded.

## 2. Surfacing

- [ ] 2.1 Run responses (`GET` run detail and run lists that include task results) set `waiting_reason` per D2 on the latest result of each unmatched pending task. Handler tests: reason present, cleared after registering a matching runner, absent for local/docker runs.
- [ ] 2.2 UI (`Pipelines.jsx`): a warning alert with `waiting_reason` for a pending task that carries it. Component test. Run vitest memory-capped, one file at a time; compare booleans, never DOM nodes, inside `waitFor`.
- [ ] 2.3 Metrics: `kestrix_remote_jobs_unmatched` gauge with `HELP`/`TYPE`, computed at scrape (D3). Handler/metrics test asserts the value tracks the query.
- [ ] 2.4 Reclaim tick logs each unmatched job once (D4). Unit test with a fake store: logged once across two ticks, and again after the job leaves and re-enters the set.

## 3. Docs

- [ ] 3.1 `docs/remote-runners.md` (what "unmatched" means, where it shows, the gauge with a sample alert rule), `docs/deployment.md` metrics list. `CHANGELOG.md` `[Unreleased]`: `Added` entry.

## 4. Verification

- [ ] 4.1 `scripts/test-all.sh` all layers, `make lint`, `make ui-lint`; 100% unit coverage gate holds.
- [ ] 4.2 Manual, on the remote-mode stack:
  - a task with `runner_tags = ["gpu"]` and only an untagged runner shows the warning in the run view, the gauge reads 1, and the log line appears;
  - registering a runner tagged `gpu` clears the warning on the next poll, and the job is claimed.
