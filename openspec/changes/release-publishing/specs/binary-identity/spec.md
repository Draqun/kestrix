## ADDED Requirements

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
