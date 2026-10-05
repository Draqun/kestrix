## ADDED Requirements

### Requirement: A remote run does not hang at start

When a remote-mode run starts, Kestrix SHALL either dispatch a job for every
task that is ready or finish the run. A dependency whose trigger is not
satisfied is not part of the run and SHALL NOT delay or block the tasks that
depend on it, as in an in-process run. A run in which no task is triggered
SHALL finish `passed`, with no task result and no job. If starting fails after
the run was created, Kestrix SHALL finish the run `failed` with an error naming
the failure; when only the job of one task cannot be created, Kestrix SHALL
record that task `failed` with that error instead. The error SHALL be redacted
like an error a runner reports.

#### Scenario: A dependency outside the run does not hold a task back
- **WHEN** `deploy` (trigger `repo.app.changed`) depends on `lint` (trigger `repo.docs.changed`) and a run starts with only `app` changed
- **THEN** a job for `deploy` SHALL be available to a runner

#### Scenario: A dependency inside the run still holds a task back
- **WHEN** the same run starts with `app` and `docs` changed
- **THEN** a job for `lint` SHALL be available and none for `deploy`
- **AND** a job for `deploy` SHALL become available once `lint` passes

#### Scenario: Nothing is triggered
- **WHEN** a run starts and no task's trigger is satisfied
- **THEN** the run SHALL be `passed`, with no task result and no job

#### Scenario: The source archive cannot be built
- **WHEN** a run starts and its source archive cannot be built
- **THEN** the run SHALL be `failed` with an error naming the cause
- **AND** no job SHALL exist for it

#### Scenario: One job cannot be created
- **WHEN** a run starts, the job of one task cannot be created, and no other job exists
- **THEN** that task SHALL be recorded `failed` with an error naming the failure
- **AND** the run SHALL be `failed`
