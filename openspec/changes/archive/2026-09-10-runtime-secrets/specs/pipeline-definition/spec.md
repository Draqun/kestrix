## ADDED Requirements

### Requirement: Variable Declaration

A project MAY declare **variable** blocks. Each `variable` block SHALL have
exactly one label, the variable key, and SHALL supply its value in exactly one
of three ways: a literal `value`, a literal `default`, or a `secret_ref`
naming a `secret` credential in the credential store. `secret_ref` SHALL NOT
be combined with `value` or `default`. A variable MAY set `scope` to
`project`, `repo`, or `job`; `repo` SHALL be required when `scope = "repo"`
and both `repo` and `job` SHALL be required when `scope = "job"`. A variable
MAY set `env`, `protected`, and `masked`. A variable with `secret_ref` SHALL
be masked regardless of the `masked` attribute. Secret values SHALL NOT be
written in HCL.

#### Scenario: Plaintext variable

- **WHEN** a user writes `variable "DOCKER_REGISTRY" { value = "registry.example.test" scope = "project" }`
- **THEN** the system SHALL parse it with the literal value and project scope

#### Scenario: Secret variable

- **WHEN** a user writes `variable "REGISTRY_PASSWORD" { secret_ref = "cred_abc" scope = "job" repo = "app" job = "publish" }`
- **THEN** the system SHALL parse it with the reference, job scope, and masked state
- **AND** the parsed definition SHALL contain no value for it

#### Scenario: Secret reference with a literal

- **WHEN** a `variable` block sets `secret_ref` and also `value` or `default`
- **THEN** validation SHALL fail with an error naming the variable and the conflicting attributes
- **AND** Kestrix SHALL not start the pipeline

#### Scenario: Scope without its target

- **WHEN** a `variable` block sets `scope = "job"` without `repo` and `job`
- **THEN** validation SHALL fail with an actionable declaration error

#### Scenario: Masked literal is documented as unmasked

- **WHEN** a `variable` block sets `masked = true` on a literal `value`
- **THEN** the system SHALL accept the declaration
- **AND** the documentation SHALL state that a literal value in Git is not a secret and is not redacted
