## ADDED Requirements

### Requirement: A push to the configured ref starts a run

When a verified push matches a registered project, resolves at least one
triggered task, and the project does not dispatch to GitLab CI, Kestrix SHALL
start a run of that project in its execution mode. The run SHALL use the
pipeline and source revision the webhook loaded and the changed set it
computed. It SHALL go through the same concurrency limit, secret resolution
and output redaction as a run started from the interface.

Only a branch push to the configured ref SHALL start a run:
- the project's `repo_ref` for its primary repository;
- the matched `ci_repo`'s `ref` for a secondary repository, or, when that
  `ref` is unset, the default branch the payload reports.

A push to any other branch, or a tag push, SHALL be resolved and recorded as
before and SHALL NOT start a run. A project that dispatches to GitLab CI
SHALL keep dispatching and SHALL NOT also start a run. The response SHALL
carry the started run's `run_id` in that project's result. A delivery that
matches several projects SHALL start at most one run per project.

#### Scenario: Push to the configured branch starts a run
- **WHEN** a verified push to `main` of a project whose `repo_ref` is `main` triggers the task `build`
- **THEN** a run of that project SHALL be started at the pushed SHA with `build` triggered
- **AND** that project's result in the response SHALL carry the run's `run_id`

#### Scenario: Push to another branch starts nothing
- **WHEN** a verified push to `feature/x` of a project whose `repo_ref` is `main` triggers tasks
- **THEN** the event SHALL be recorded as before
- **AND** no run SHALL be started

#### Scenario: A tag named like the branch starts nothing
- **WHEN** a verified push of the tag `main` arrives for a project whose `repo_ref` is `main`
- **THEN** no run SHALL be started

#### Scenario: Secondary push to its ref starts a run
- **WHEN** a verified push to `release` of a secondary repository declared as a `ci_repo` with `ref = "release"` marks it changed and triggers a task
- **THEN** a run of the project SHALL be started with that `ci_repo` in its changed set

#### Scenario: No triggered task starts nothing
- **WHEN** a verified push to the configured ref triggers no task
- **THEN** the event SHALL be recorded as `skipped`
- **AND** no run SHALL be started

#### Scenario: Dispatch replaces the run
- **WHEN** GitLab dispatch is enabled and a GitLab project ID resolves for the matched project
- **THEN** the pipeline SHALL be dispatched as before
- **AND** no Kestrix run SHALL be started

#### Scenario: Secret resolution failure is recorded on a failed run
- **WHEN** a push starts a run whose pipeline references a secret that cannot be resolved
- **THEN** a failed run naming the secret error SHALL be recorded, as for a run started from the interface

### Requirement: A branch deletion is ignored

A verified push that deletes a branch, whose checkout revision is the
all-zero SHA or which the payload marks as deleted, SHALL be recorded with
status `ignored` and the reason `branch deleted`. It SHALL NOT load a
pipeline, acquire a source, dispatch, or start a run.

#### Scenario: Deleting a branch
- **WHEN** a verified push for a registered project carries `after` `0000000000000000000000000000000000000000`
- **THEN** the event SHALL be recorded as `ignored` with reason `branch deleted`
- **AND** no pipeline SHALL be loaded and no run SHALL be started
