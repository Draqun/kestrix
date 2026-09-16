# binary-identity Specification

## Purpose
TBD - created by archiving change rename-binary-to-kestrix. Update Purpose after archive.
## Requirements
### Requirement: User-facing binary name is Kestrix

The project SHALL produce a single executable named `kestrix` in the `bin/` directory.

#### Scenario: Default build target

- **WHEN** a developer runs `make build`,
- **THEN** the build system SHALL produce `bin/kestrix`.

#### Scenario: Existing binary no longer produced

- **WHEN** the build completes,
- **THEN** no file named `bin/metaci` SHALL be created by the standard build targets.

### Requirement: Process identity identifies as Kestrix

Runtime logs and CLI help text SHALL identify the tool as **Kestrix** rather than `metaci` or `MetaCI`.

#### Scenario: Server startup log

- **WHEN** the `serve` command starts,
- **THEN** the startup log message SHALL contain the text "Kestrix server".

#### Scenario: CLI usage header

- **WHEN** a user runs the binary with `--help` or with no arguments,
- **THEN** the usage header SHALL contain the text "Kestrix".

### Requirement: Swagger index uses the Kestrix name

The Swagger/OpenAPI documentation served by the application SHALL display "Kestrix API" as the API title.

#### Scenario: Swagger JSON title

- **WHEN** a client requests `/docs/swagger.json`,
- **THEN** the `info.title` field SHALL be "Kestrix API".

#### Scenario: Swagger UI page header

- **WHEN** a user opens `/docs/index.html`,
- **THEN** the Swagger UI header SHALL display "Kestrix API" instead of "MetaCI API".

### Requirement: Binary reports its version

The binary SHALL expose its embedded version through `kestrix version` (one line on stdout, exit `0`) and through `GET /api/version`. A build produced by the release pipeline or `make build-release` SHALL report the Git tag; only an unversioned local `go build` MAY report `dev`.

#### Scenario: Released binary

- **WHEN** the binary from a `v0.3.0` release runs `kestrix version`
- **THEN** it prints `v0.3.0`

#### Scenario: API version matches the binary

- **WHEN** a client calls `GET /api/version` on a running released instance
- **THEN** the returned `version` equals what `kestrix version` prints for that binary

#### Scenario: UI shows the version

- **WHEN** a signed-in user views any page
- **THEN** the layout footer shows the server version returned by `GET /api/version`

