## 1. Evaluation

- [ ] 1.1 Build the `var` evaluation context from eligible variables (design D2) after decoding `variable` blocks, before decoding tasks, repos and the project. Unit tests for each eligibility rule and its error message.
- [ ] 1.2 Evaluate the D3 allowlist with that context. Accept only templates and `var.<name>` traversals (D4). Re-run `NormalizeRunnerTags` and source URL validation on the results. Parser tests:
  - `image = "node:${var.node}"`;
  - `runner_tags = ["${var.arch}", "gpu"]`;
  - `ref = var.release`;
  - an unknown variable;
  - a masked, `secret_ref`, `protected` or job-scoped variable;
  - a function call rejected;
  - an interpolated URL failing validation.
- [ ] 1.3 Non-allowlisted fields: `literal()`'s error names `script` (`$VAR` / `$${VAR}`) and `trigger` specifically. Tests. Every existing parser test passes unchanged, and an existing example pipeline parses byte-identically.

## 2. Docs

- [ ] 2.1 `docs/pipeline-definition.md`: an "Interpolation" section (allowed fields, eligibility, why not `script`/`trigger`, the `$${` escape). Update one example under `examples/` to use `var.` in `image`. `CHANGELOG.md` `[Unreleased]`: `Added` entry.

## 3. Verification

- [ ] 3.1 `scripts/test-all.sh` all layers, `make lint`; 100% unit coverage gate holds.
- [ ] 3.2 Manual: a project whose pipeline pins `image = "alpine:${var.alpine}"` runs a `docker`-mode task on that image. The run detail's stored pipeline shows the resolved image. Changing the variable changes the image on the next run.
