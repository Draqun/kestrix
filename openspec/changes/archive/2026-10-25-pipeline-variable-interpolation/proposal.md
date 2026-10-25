## Why

Variables in a pipeline file reach only task processes, as environment
variables. Fields that Kestrix itself reads cannot use them:
- a task's container image;
- the project's default image;
- runner tags;
- a `ci_repo`'s URL and ref.

A pipeline that pins `node:22` in ten tasks repeats it ten times. The parser
rejects every `${...}` with "interpolated strings are not supported in
pipeline config", and a bare `var.x` with "variable/scoped references are
not supported" (`internal/parser/hcl_parser.go`). It deliberately evaluates
literals only, with no `hcl.EvalContext`.

`hashicorp/hcl/v2` and `go-cty`, already dependencies, evaluate templates
against an `EvalContext` natively. The work is in choosing where
interpolation is safe.

## What Changes

- `var.<name>` references and `"...${var.<name>}..."` templates become valid
  in a fixed set of fields:
  - `ci_task` → `execute.image` and `runner_tags` items;
  - the project's `default_image`;
  - `ci_repo` `url` and `ref`.

  They are resolved at parse time from the file's own `variable` blocks.
- A variable is eligible when it has a literal `value` or `default`, project
  scope, and is neither `masked`, `protected`, nor a `secret_ref`.
  Interpolated fields are stored with the run and shown in the UI, so a
  hidden value must never reach them. Referencing an ineligible, unknown or
  scoped variable fails parsing, naming the field and the variable.
- Every other field keeps rejecting interpolation, with a clearer message.
  - `trigger` has its own expression grammar.
  - `script` is passed to `bash`, where `${VAR}` already means a shell
    variable. HCL's escape `$${VAR}` stays available for writing it
    literally.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `pipeline-definition`: variable interpolation in declared fields.

## Non-goals

- Interpolation in `script`, `trigger`, task names, `depends_on` or
  `execute.repo`.
- HCL functions (`upper`, `format`, ...), `locals` blocks, or expressions
  beyond variable references and string templates.
- Values from outside the file (project settings, environment, secrets).
- Repo- or job-scoped variables in interpolated fields. Their meaning would
  depend on which task reads them.

## Impact

- Pipelines: files that parse today parse identically. Files using the new
  syntax were rejected before.
- API: none. The stored pipeline snapshot carries resolved values.
- No schema change.
- Code: `internal/parser/hcl_parser.go` (an `EvalContext` for the allowed
  attributes), parser tests, `docs/pipeline-definition.md`, an example
  pipeline.
