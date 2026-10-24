## ADDED Requirements

### Requirement: Task artifacts reach dependent remote jobs

In a `remote`-mode run, after a job whose task declares artifacts passes, its
runner SHALL upload the declared paths as one archive before reporting
completion. Kestrix SHALL accept the upload only for the job's current claim
epoch from the runner holding the claim, SHALL reject an archive larger than
`KESTRIX_RUNNER_ARTIFACT_MAX_BYTES` (default 50 MiB) with `413`, and SHALL
reject an archive containing an entry outside the declared paths. A repeated
upload for the same claim SHALL replace the stored archive.

When Kestrix dispatches a job, its payload SHALL carry the stored artifacts of
every passed ancestor of the task, ordered topologically with ties broken by
declaration order. The runner SHALL unpack them over the source archive in
that order, a later archive replacing a file an earlier one wrote, rejecting
any entry that would escape the job directory. Kestrix SHALL delete a run's
artifacts once the run reaches a terminal status.

#### Scenario: Deploy sees what build produced
- **WHEN** `build` declares `artifacts = ["dist/"]`, writes `dist/app.txt` and passes, and `deploy` depends on `build`
- **THEN** the job dispatched for `deploy` SHALL find `dist/app.txt` in its working directory
- **AND** this SHALL hold when the two jobs execute on different runners

#### Scenario: Oversized artifact
- **WHEN** a runner uploads an artifact archive larger than the configured limit
- **THEN** Kestrix SHALL respond `413` and store nothing
- **AND** the task SHALL be recorded as failed with an error naming the limit

#### Scenario: Stale claim cannot upload
- **WHEN** a runner uploads artifacts using a claim epoch that silent-runner recovery has superseded
- **THEN** the upload SHALL be rejected and SHALL NOT be stored

#### Scenario: Diamond order is deterministic
- **WHEN** `c` depends on `a` and `b`, the pipeline file declares `a` before `b`, and both wrote `out/x`
- **THEN** `c` SHALL see the `out/x` written by `b`

#### Scenario: Artifacts are deleted with the run
- **WHEN** a remote-mode run reaches a terminal status
- **THEN** its stored artifacts SHALL be deleted
