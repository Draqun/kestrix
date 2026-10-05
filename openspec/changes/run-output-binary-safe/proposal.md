## Why

A task that prints a NUL byte or a byte that is not valid UTF-8 (a Latin-1
tool, a binary dumped by mistake) leaves its run `running` for good. So does
`max_output_bytes` cutting a multi-byte character.

- PostgreSQL refuses such text: `runs.output` and `runs.error` with SQLSTATE
  22021, the JSONB `task_results` with 22P05 for NUL.
- In-process, `finishRun` and the per-task flush discard the error. Nothing is
  logged; the run stays `running` until a restart fails it.
- Remotely, the store rejects the runner's trace append and completion, so the
  job cannot complete.
- Reproduced on `main` (`0febb25`) on Postgres: through `RunService.Start`
  with a Latin-1 byte (run `running`, stored output empty); at the store level
  for a NUL, a cut character and the remote writes.

## What Changes

- The store makes every task-derived text storable before it writes it: runs of
  bytes that are not valid UTF-8, and NUL bytes, become U+FFFD. One rule, applied
  to all six writes that carry task output, after redaction.
- `finishRun` and the per-task flush log a failed write instead of discarding
  it, as `finishStartedTask` already does.
- The output is stored as text. Bytes that were not text are not preserved.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `run-scheduling`: stored run output is valid text.
- `run-lifecycle-recovery`: a failed write of a run's outcome is logged.

## Non-goals

- Keeping the raw bytes: no `bytea` column, escaping or download of raw output.
- Cutting at a character boundary. The bound still counts bytes, so a cut
  inside a character stores one U+FFFD before the marker. Remote output already
  cuts at a character start.
- Retrying a failed `FinishRun`, or finishing the run with its output dropped.
  With this cause gone the remaining ones (database down) fail a retry too.
- A data migration: writes of this kind always failed, so no stored row is
  affected.

## Impact

- No schema or port change.
- Code: `internal/store` (`pgstore_run.go`, `pgstore_remote_job.go`),
  `internal/service/service.go`, `docs/usage.md`, `CHANGELOG.md`.
