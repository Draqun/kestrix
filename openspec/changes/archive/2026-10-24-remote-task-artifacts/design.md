## Context

`local`/`docker` runs share one workspace across tasks (`internal/runner/runner.go`:
one `LocalExecutor`/`DockerExecutor` over one `WorkDir`, cleaned once after
the run). `remote` runs dispatch each task as a job whose payload holds the
run-start source archive. The runner unpacks it into a per-job temp
directory and removes it when the job ends. Nothing flows back except
status, output and error.

```
build job ──passes──▶ runner tars declared paths ─PUT /artifacts?epoch─▶ remote_artifacts(run, "build")
                                                                              │
resumeRemoteRun ─ deploy eligible ─▶ payload{source archive, artifacts:[build]} ◀┘
                                         │
runner: unpack source ─▶ unpack build's artifact over it ─▶ run deploy
run terminal ─▶ DELETE remote_artifacts WHERE run_id = …
```

## Decisions

### D1. Declared paths, not the whole workspace

`artifacts` names what a task produces. Uploading the whole workspace after
every job would ship the full source tree (and `node_modules`) on every
edge, unbounded by intent.

A declared path is relative to the task's working directory, which is where
its script runs. It is stored relative to the workspace root, so it is
restored in the same place for any dependent. Validation happens at parse
time:
- relative;
- no `..` component;
- not empty;
- at most 32 entries per task.

### D2. Missing artifacts fail the task in every mode

After a task exits 0, every declared path must exist, or the task fails
with `artifact "dist/" not found`. In `local`/`docker` nothing is
transferred, but the check still runs. Otherwise a pipeline could pass on a
laptop and fail only on a runner, which is the class of bug this change
removes.

### D3. Upload before completion, fenced like trace

The runner uploads with `PUT /api/runner-jobs/{id}/artifacts?epoch=N` a
gzip tar of the declared paths, only for a passed task, then reports
completion. Kestrix accepts it only for the job's current epoch and owning
runner, the same checks as `PATCH .../trace`. The body is capped by
`KESTRIX_RUNNER_ARTIFACT_MAX_BYTES` (default 50 MiB, validated positive);
over the cap answers `413`, and the runner fails the task naming the limit.
A retried upload for the same epoch replaces the stored archive.

### D4. Ancestors in dependency order

When `resumeRemoteRun` dispatches a task, its payload's `artifacts` lists
the stored archives of every passed ancestor (transitive `depends_on`).
They are ordered topologically, ties broken by declaration order in the
pipeline file. The runner unpacks the source archive, then each artifact in
that order, a later one overwriting the same path. That mirrors what a
shared checkout would contain after the ancestors ran in that order. A task
with no ancestors gets none.

### D5. Artifacts live only as long as the run

`remote_artifacts` rows are deleted by the same periodic sweep that deletes
terminal runs' remote secrets (`DeleteTerminalRemoteSecrets`). Run
retention's `ON DELETE CASCADE` is the backstop.

## Security Considerations

- Unpacking reuses `safeJoin`/`safeSymlinkTarget`, which already reject
  tar-slip paths and escaping symlinks. The upload is additionally
  validated server-side: every entry must be under a declared path.
- Artifacts are stored unencrypted and not redacted (they are files, not
  output), but only until the run ends. A task that writes a secret into a
  declared path exposes it to its dependents, exactly as a shared checkout
  does today. The docs say not to declare such paths.
- Only the job's current claim can upload, so a stale or foreign runner
  cannot plant files into another task's inputs.

## Risks / Trade-offs

- Large artifacts sit in Postgres for the run's lifetime. The cap and D5
  bound that; object storage is a later option if 50 MiB proves too small.
- A diamond whose two branches write the same path resolves by D4's order,
  not by timing. That is deterministic, and documented.

## Migration Plan

`CREATE TABLE IF NOT EXISTS remote_artifacts (...)` in the migration list.
No existing row changes. Rollback: the previous image ignores the table, and
its runners never upload.
