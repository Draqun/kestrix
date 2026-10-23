## 1. Reproduce

- [x] 1.1 Store integration tests on Postgres for each of the six writes in the design, with three inputs: a Latin-1 byte, a multi-byte character cut after its first byte, a NUL. Each write succeeds and reads back with U+FFFD in place of the bytes; a clean input reads back unchanged. Red on `main`: SQLSTATE 22021 or 22P05.
- [x] 1.2 Service test on Postgres through `RunService.Start`: a pipeline runner writes `caf\xe9` and a NUL to `opts.Stdout` and returns a task result with the same text. The run reaches `passed`, and its output and the task result hold U+FFFD. Red on `main`: the run stays `running` and its stored output is empty.

## 2. Store

- [x] 2.1 The rule of design D2 and its `TaskResult` form (D3) in the store package, with table-driven unit tests: clean text returned as it is, a Latin-1 byte, a cut character, a NUL, adjacent NULs and invalid bytes collapsing into one U+FFFD, the empty string, and that the caller's result slice is not modified.
- [x] 2.2 Apply it at the entry of `FlushRunOutput`, `FinishRun`, `ApplyTaskResult` and the shared `appendTaskResult` (which `ClaimNextJob` also uses) in `pgstore_run.go`. Task 1.1 is green for these.
- [x] 2.3 Apply it at the entry of `AppendJobTrace` and `CompleteJob` in `pgstore_remote_job.go`. Task 1.1 is green for these; the existing trace and completion tests, which bound and join chunks, pass unchanged.

## 3. Service

- [x] 3.1 `finishRun` logs a failed `FinishRun`, and `OnTaskComplete` a failed `AppendTaskResult` or `FlushRunOutput` (design D4). Unit tests with a store whose writes fail and a captured log: one warning with the run id and the error, a secret value in the error redacted, and the run is still unregistered and its slot released as before.
- [x] 3.2 Task 1.2 is green.

## 4. Docs

- [x] 4.1 `docs/usage.md`, next to `KESTRIX_RUNNER_MAX_OUTPUT_BYTES`: output is stored as text, bytes that are not valid UTF-8 and NUL bytes are stored as U+FFFD, and the bound limits what is taken from the task rather than the bytes stored. `CHANGELOG.md` `[Unreleased]`, `Fixed`: a task printing such bytes no longer leaves its run `running`.

## 5. Verification

- [x] 5.1 `scripts/test-all.sh` all layers and `make lint`; the 100% unit coverage gate holds. Mutation check: removing the call from `FinishRun` turns tasks 1.1 and 1.2 red.
- [x] 5.2 Manual, on a local-mode project:
  - a task running `printf 'caf\351\000x\n'` passes, the run reads `passed`, and its output reads `caf`, U+FFFD, `x`;
  - with `KESTRIX_RUNNER_MAX_OUTPUT_BYTES=1000`, a task running `yes é | head -n 2000` passes (1000 bytes ends inside the 334th `é`), the run reads `passed` and its output ends with the truncation marker.
