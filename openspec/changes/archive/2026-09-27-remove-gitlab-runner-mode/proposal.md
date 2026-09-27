## Why

The `gitlab-runner` project mode cannot run a single task. It shells out to
`gitlab-runner exec <stage> <task>` (`internal/runner/runner.go`), and:

- current GitLab Runner releases have no `exec` command - `gitlab-runner
  --help` on 17.8.3 lists none, and the call fails with `FATAL: Command exec
  not found`;
- even when `exec` existed, the call was wrong: the command took an
  executor (`gitlab-runner exec shell|docker <job>`), not a stage, and it
  read `.gitlab-ci.yml` from its working directory, while Kestrix writes the
  generated file to a separate temporary directory and runs the command in
  the checkout.

The mode is still selectable, so a project or instance configured with it
fails every run. What it was meant to provide already exists: `remote` runs
tasks on other machines, and dispatch triggers real GitLab CI pipelines
through the API. `docs/ci-tools.md` has marked it unsupported since !55.

## What Changes

- Remove the `gitlab-runner` execution path: `GitLabRunnerExecutor`,
  `RunGitLabRunner`, its `PipelineExecutor` branch, and their tests.
- `gitlab-runner` is no longer a valid project mode: the project API
  rejects it like any unknown mode.
- Validate `runner.mode` at startup at all - today any string is accepted;
  an unknown value (including `gitlab-runner`) fails naming the accepted ones.
- Docs, security notes, CHANGELOG and specs stop naming the mode. Kestrix
  has not been published, so there is no deprecation path, no removal
  message and no data migration.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runtime-configuration`: `runner.mode` validated.
- `remote-runner-execution`, `task-execution-isolation`,
  `runner-unit-only-coverage`: requirements no longer list the mode.

## Non-goals

- Removing the `.gitlab-ci.yml` generator (`ci-generator`) - after this
  change only the `POST /api/generate` preview uses it.
- Any other GitLab Runner integration.
- Migrating stored projects that carry the mode - no published install has
  any.

## Impact

- Only configurations that could never run a task change behavior.
- No schema change: the database is migrated exactly as before, so rolling
  back to the previous image stays safe.
- Code: `internal/runner`, `internal/service`, `internal/config`,
  `cmd/kestrix`; docs: `docs/ci-tools.md`,
  `docs/ci-generator.md`, `SECURITY.md`, `AGENTS.md`, `CHANGELOG.md`.
