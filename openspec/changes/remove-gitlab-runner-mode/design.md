## Context

`PipelineExecutor.Execute` (`internal/runner/pipeline_executor.go`) picks a
backend from the project mode: `local`/empty, `docker`, `gitlab-runner`, and
an error for anything else. The `gitlab-runner` branch generates a
`.gitlab-ci.yml` into a temporary directory and runs
`GitLabRunnerExecutor.RunTask`, which executes `gitlab-runner exec <stage>
<task>` in the checkout. Project modes are validated against
`validProjectModes` in `internal/service/service.go`. The instance
`runner.mode` is not validated anywhere; it becomes the default mode stored
projects backfill to (`store.NewPG(databaseURL, defaultMode)`).

## Decisions

### D1. Delete, don't repair

There is nothing to repair towards: the command no longer exists, and the
old invocation was wrong on two counts (stage passed as executor; generated
file written elsewhere than the working directory). Running tasks on other
machines is `remote`; running a real GitLab pipeline is dispatch.

### D2. No migration, no deprecation

Kestrix has not been published, so nothing in the field stores the mode.
The schema and `migrate()` are unchanged. A row that still carries the mode
(a local development database) hits D4's error on its next run; editing the
project to another mode fixes it.

### D3. Validate `runner.mode` where the serve configuration is final

`config.RunnerConfig.Validate()` accepts `""`, `local`, `docker`, `remote`,
and names the setting and accepted values for anything else. `cmdServe` calls it next to
`SourceCache.Validate()`, after flag/file/env precedence has been applied.
The mode list lives once, unexported, in `domain` (`IsValidMode`, `Modes`):
the project API and `runner.mode` validation both read it, so the two
cannot drift.

### D4. The `PipelineExecutor` keeps its unsupported-mode error

With the branch gone, a `gitlab-runner` value that still reaches the
executor (a row in a development database, see D2) hits the existing
`unsupported mode` error instead of a missing binary.

## Risks / Trade-offs

- An instance configured with `runner.mode: gitlab-runner` no longer starts.
  Intended: that configuration could not run a task, and the message names
  the accepted modes.
