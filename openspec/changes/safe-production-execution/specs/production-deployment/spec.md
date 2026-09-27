## MODIFIED Requirements

### Requirement: Runtime image runs local-mode tasks

The published runtime image SHALL include the shell `local`-mode execution
invokes, so `kestrix runner --mode local` started from the image can execute
tasks, as can a `local`-mode project run by the server container where
`local` execution is explicitly allowed.

#### Scenario: Runner from the image in local mode
- **WHEN** `kestrix runner --mode local` runs in a container started from the published image and claims a job
- **THEN** the task's script SHALL execute rather than fail because its shell is missing

## ADDED Requirements

### Requirement: Production tasks run outside the control plane

The production Compose file SHALL configure Kestrix with runner mode
`remote`, so no task executes in the container that holds the master key and
the token signing key. It SHALL define a `runner` service, behind its own
profile, that runs `kestrix runner` from the same image with no key file, no
database credential, and a network it shares only with the Kestrix service.
The deployment documentation SHALL describe registering the runner and
starting that profile.

#### Scenario: Default start runs no task in the control plane
- **WHEN** an operator starts the production Compose file without extra profiles and starts a project run
- **THEN** no task script SHALL execute in the `kestrix` container
- **AND** the run's tasks SHALL wait for a runner

#### Scenario: The runner has no keys and no database
- **WHEN** a task executing on the `runner` service reads the configured key paths or connects to the database service
- **THEN** the key files SHALL be absent there
- **AND** the database service SHALL NOT be reachable from it

#### Scenario: Runner profile executes tasks
- **WHEN** an operator registers a runner, sets its token, and starts the `runner` profile
- **THEN** a project run's tasks SHALL execute on the runner and the run SHALL complete
