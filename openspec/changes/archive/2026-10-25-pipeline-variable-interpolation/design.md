## Context

`internal/parser/hcl_parser.go` parses with `hclsyntax.ParseConfig` and reads
attributes through `literal()`. That accepts single-part templates, literal
values and tuples of literals. It rejects multi-part templates and scope
traversals with explicit errors. `variable` blocks are decoded in the same
pass (`decodeVariableBlock`) into `proj.Variables`: `value`/`default`/
`secret_ref`, `scope`, `masked`, `protected`, `env`. `service.BuildVarStore`
later turns them into a scoped store for task environments. The parsed
`model.Project` is stored as the run's `pipeline_def` and shown in the UI.

```
parse pipeline.tf
  1. decode variable blocks           ─▶ eligible = {name: cty.String(value|default)}
  2. build hcl.EvalContext{"var": cty.ObjectVal(eligible)}
  3. allowed attribute? ── yes ─▶ expr.Value(ctx) → string (or []string)
                          └ no  ─▶ literal() (unchanged; better message)
  4. model.Project (resolved values) ─▶ pipeline_def, dispatch, UI
```

## Decisions

### D1. Resolve at parse time, from the file only

Every eligible value is a literal in the same file, so the parser can
resolve references before anything else sees the project. Everything
downstream (the stored snapshot, trigger resolution, dispatch payloads)
works on plain strings as today.

Rejected: a post-parse pass after `BuildVarStore`. That would bring in
secret values, the one thing interpolated fields must never contain.

### D2. Eligibility

A variable is eligible when:
- its scope is unset or `project`;
- it has `value` or `default`;
- it has no `secret_ref`;
- it is not `masked`;
- it is not `protected`.

Rationale:
- `masked` and `secret_ref` mean "do not show this", and interpolated fields
  are shown.
- `protected` values depend on the ref being built, which the parser does
  not know.
- Repo and job scopes have no single value for a project-level field.

An ineligible reference fails with
`variable "X" cannot be interpolated into <field>: <reason>`.

### D3. A fixed allowlist of attributes

The allowed attributes are:
- `ci_task` → `execute.image`;
- `ci_task` → `runner_tags` (each element a template);
- `project` → `default_image`;
- `ci_repo` → `url` and `ref`.

These are values Kestrix reads and that differ per environment or version.
Every other attribute keeps using `literal()`, whose error for a template
now names the reason:
- `script`: use `$VAR`, or `$${VAR}` for a literal `${`;
- `trigger`: triggers use their own expression syntax.

Interpolated `runner_tags` still pass through `NormalizeRunnerTags`, and a
`ci_repo` `url` through the existing source validation. Interpolation cannot
bypass either.

### D4. Only references and templates

The evaluation context has no functions and exposes only `var`. An
expression other than a template or a `var.<name>` traversal (arithmetic,
conditionals, function calls) is rejected, so the language surface grows by
exactly one idea.

## Security Considerations

- D1 and D2 guarantee that no secret, masked or protected value reaches a
  field stored with the run, dispatched to a runner, or displayed.
- `ci_repo.url` still goes through the literal-IP and scheme checks after
  interpolation, so a variable cannot smuggle in a URL the validator would
  refuse.

## Risks / Trade-offs

- An eligible variable used in `image` is also exported to task
  environments, as today. That changes nothing about its exposure.
- Users may expect `script` interpolation. D3's message tells them why not
  and what to write instead.

## Migration Plan

No data or schema change. A file that parses today yields the identical
`model.Project`.
