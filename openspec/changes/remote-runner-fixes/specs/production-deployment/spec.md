## ADDED Requirements

### Requirement: Runtime image runs local-mode tasks

The published runtime image SHALL include the shell `local`-mode execution
invokes, so both a `local`-mode project run by the server container and
`kestrix runner --mode local` started from the image can execute tasks.

#### Scenario: Runner from the image in local mode
- **WHEN** `kestrix runner --mode local` runs in a container started from the published image and claims a job
- **THEN** the task's script SHALL execute rather than fail because its shell is missing
