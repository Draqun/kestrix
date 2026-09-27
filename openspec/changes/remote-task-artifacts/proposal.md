## Why

A pipeline behaves differently in `remote` mode than in `local` or
`docker`, and nothing says so.

- In `local` and `docker` mode every task of a run executes in one
  checkout. `RunLocal`/`Run` build a single executor over one `WorkDir`;
  `docker` bind-mounts that same directory as `/workspace` for every task. A
  `build` task's `dist/` is there for `deploy`.
- In `remote` mode `startRemote` archives the checkout once, at run start.
  `resumeRemoteRun` gives every later job that same pristine archive
  (`GetRemoteRunSnapshot`). The runner unpacks each job into a fresh
  `os.MkdirTemp` directory and deletes it afterwards
  (`cmd/kestrix/runner.go`). The completion report carries only status,
  output and error. So `deploy` never sees what `build` produced, even in a
  plain two-task chain.

`remote` is now the main mode (and the production default with
`safe-production-execution`), so a build → deploy pipeline that works on a
laptop fails, or deploys stale files, on runners. That is a correctness bug.

## What Changes

- A task may declare `artifacts = ["dist/", "out/app.tar"]`: paths,
  relative to its working directory, that it produces for later tasks.
  Paths are validated at parse time: relative, no `..`, at most 32.
- In every mode, a task that passes but lacks one of its declared artifacts
  fails, naming the missing path. The same pipeline is then equally valid
  locally and remotely.
- In `remote` mode, after a job passes, its runner uploads the declared
  paths as one bounded archive (`PUT /api/runner-jobs/{id}/artifacts`,
  epoch- and ownership-fenced like trace, capped by
  `KESTRIX_RUNNER_ARTIFACT_MAX_BYTES`, default 50 MiB).
- A dependent job's payload carries the artifacts of all its passed
  ancestors, in dependency order. The runner unpacks them over the source
  archive, later ancestors winning on the same path. The unpack uses the
  existing tar-slip-safe path.
- Artifacts are deleted when the run reaches a terminal status, like remote
  job secrets.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pipeline-definition`: `ci_task` accepts `artifacts`.
- `remote-runner-execution`: artifacts reach dependent remote jobs.

## Non-goals

- Sharing undeclared files between remote jobs (whole-workspace upload).
  Declared paths keep transfers bounded and intent explicit.
- Downloading artifacts from the UI or API after a run.
- Artifacts for a single-task re-run of a finished run. Its dependencies'
  artifacts are already deleted, and it starts from the source.
- Caching between runs (dependency caches, `node_modules`).

## Impact

- Schema: new table `remote_artifacts(run_id, task_name, archive BYTEA,
  created_at)`, referencing `runs` with `ON DELETE CASCADE`. Idempotent
  `CREATE TABLE IF NOT EXISTS`; the previous image ignores it.
- API: new runner endpoint; the job payload gains `artifacts`.
- Behaviour: a declared artifact that a passing task did not produce now
  fails the task, in every mode.
- Code: `internal/parser`, `internal/runner`, `internal/service`
  (`remote_dispatch.go`), `internal/store`, `internal/web`
  (runner endpoints), `cmd/kestrix/runner.go`, `docs/pipeline-definition.md`,
  `docs/remote-runners.md`.
