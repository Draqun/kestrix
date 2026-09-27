## ADDED Requirements

### Requirement: Variable interpolation in declared fields

A pipeline file MAY reference variables it declares, as `var.<name>` or
inside a string template `"...${var.<name>}..."`, in exactly these
attributes:
- a `ci_task`'s `execute` `image`;
- each element of a `ci_task`'s `runner_tags`;
- the project's `default_image`;
- a `ci_repo`'s `url` and `ref`.

The parser SHALL resolve each reference from the file's own `variable`
blocks. A variable SHALL be eligible only if:
- its scope is unset or `project`;
- it has a literal `value` or `default`;
- it has no `secret_ref`;
- it is neither `masked` nor `protected`.

A reference to an unknown or ineligible variable SHALL fail parsing, naming
the attribute, the variable and the reason. Only variable references and
string templates SHALL be accepted: a function call or any other expression
SHALL fail parsing. Resolved values SHALL pass the same validation as literal
values, including runner-tag normalisation and source URL validation.

Every other attribute SHALL keep rejecting interpolation. For `script` the
error SHALL say that the shell's `$VAR` is available and that `$${` writes a
literal `${`. For `trigger` it SHALL say that triggers use their own
expression syntax. A pipeline file without interpolation SHALL parse exactly
as before.

#### Scenario: Image from a variable
- **WHEN** a pipeline declares `variable "node" { value = "22" }` and a task sets `image = "node:${var.node}"`
- **THEN** the parsed task's image SHALL be `node:22`

#### Scenario: Secret variable rejected in an interpolated field
- **WHEN** a task sets `image = "registry.example/app:${var.TOKEN}"` and `TOKEN` is declared with `secret_ref`
- **THEN** parsing SHALL fail naming `image`, `TOKEN`, and that secret variables cannot be interpolated

#### Scenario: Masked variable rejected
- **WHEN** an interpolated field references a variable declared with `masked = true`
- **THEN** parsing SHALL fail naming the field and the variable

#### Scenario: Script keeps shell syntax
- **WHEN** a task's `script` contains `${var.node}`
- **THEN** parsing SHALL fail with a message pointing to `$VAR` and the `$${` escape

#### Scenario: Interpolated URL is still validated
- **WHEN** a `ci_repo` sets `url = var.repo_url` and the variable's value is a URL the source validator rejects
- **THEN** parsing SHALL fail with that validation error

#### Scenario: Functions are not available
- **WHEN** an interpolated field uses `upper(var.node)`
- **THEN** parsing SHALL fail stating that only variable references are supported
