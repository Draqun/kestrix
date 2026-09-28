# production-deployment Specification

## Purpose

Defines the safety and completeness guarantees of the production Compose deployment a customer installs from the repository or the release image.
## Requirements
### Requirement: No shared default database credential

The production Compose file SHALL refuse to start unless `POSTGRES_PASSWORD` is set, and SHALL use that value for both the database container and the Kestrix `DATABASE_URL`. No default password SHALL be embedded in the file.

#### Scenario: Password not provided
- **WHEN** an operator runs the production Compose file without `POSTGRES_PASSWORD`
- **THEN** Compose exits with an error naming the variable and starts no container

#### Scenario: Password provided
- **WHEN** `POSTGRES_PASSWORD` is set
- **THEN** Kestrix connects to the database with that password and `GET /api/health` answers `200`

### Requirement: Database is not exposed by default

The production Compose file SHALL NOT publish the database port on the host unless the operator opts in, and an opt-in SHALL bind to the loopback interface unless explicitly overridden.

#### Scenario: Default start
- **WHEN** the production deployment is started with default settings
- **THEN** `docker compose ps` shows no host port for the database service

### Requirement: Only Kestrix and its database start by default

Services that are not part of a customer deployment (the marketing site) SHALL be behind a Compose profile and SHALL NOT start without it.

#### Scenario: Plain start
- **WHEN** an operator runs `up -d` without profiles
- **THEN** exactly the Kestrix and database services are running

### Requirement: A documented, working TLS path

The repository SHALL ship a reverse-proxy configuration that terminates HTTPS for a configured domain and forwards to Kestrix with the client address header, enabled by a Compose profile, and the deployment guide SHALL describe both the TLS profile and the plain-HTTP evaluation settings (including the cookie `Secure` flag).

#### Scenario: TLS profile
- **WHEN** the operator sets `KESTRIX_DOMAIN` and starts with the TLS profile
- **THEN** the UI is served over HTTPS on that domain, login works with the secure cookie, and failed logins are throttled by the real client address

#### Scenario: HTTP evaluation
- **WHEN** the operator follows the guide's plain-HTTP section
- **THEN** browser login works without a certificate because the cookie `Secure` mark is disabled as the guide instructs

### Requirement: Deployment lifecycle is documented

A deployment guide SHALL cover install, first administrator, webhook exposure, upgrade, backup, restore, and rollback to a pinned image tag, and SHALL be referenced from the README.

#### Scenario: Operator upgrades
- **WHEN** an operator follows the upgrade section
- **THEN** the sequence is backup, pull the new tag, `up -d`, verify health, and they are told which release notes to read for breaking changes

### Requirement: Runtime image runs local-mode tasks

The published runtime image SHALL include the shell `local`-mode execution
invokes, so `kestrix runner --mode local` started from the image can execute
tasks, as can a `local`-mode project run by the server container where
`local` execution is explicitly allowed.

#### Scenario: Runner from the image in local mode
- **WHEN** `kestrix runner --mode local` runs in a container started from the published image and claims a job
- **THEN** the task's script SHALL execute rather than fail because its shell is missing

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

