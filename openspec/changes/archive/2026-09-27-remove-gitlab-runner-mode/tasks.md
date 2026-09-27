## 1. Remove the execution path

- [x] 1.1 Delete `GitLabRunnerExecutor`, `RunGitLabRunner` and the `gitlab-runner` case in `PipelineExecutor.Execute`; drop what only they used (e.g. the `GenerateYAML` field, stage assignment) if nothing else needs it; remove or rewrite the tests that exercised them
- [x] 1.2 Drop `gitlab-runner` from the valid project modes: `service.validateProject` now checks `domain.IsValidMode` (`internal/domain/ports.go`), the single mode list shared with `runner.mode` validation; the existing unknown-mode tests cover the rejection

## 2. Configuration

- [x] 2.1 `config.RunnerConfig.Validate()` (via `domain.IsValidMode`: local, docker, remote, unset; anything else fails naming `domain.Modes()`), called from `cmdServe`; unit tests for each case
- [x] 2.2 No schema or data migration: `migrate()` stays as on `main`; verify migrate → roll back to the `main` binary → migrate again on one database, with identical schema dumps

## 3. Docs

- [x] 3.1 `docs/ci-tools.md` (mode table, cancellation text), `docs/ci-generator.md`, `SECURITY.md`, `AGENTS.md`, code comments naming the mode; regenerate Swagger if an annotation names it
- [x] 3.2 `CHANGELOG.md`: drop the mode from the `[Unreleased]` entries that name it; note `runner.mode` validation under `Changed`

## 4. Verification

- [x] 4.1 `scripts/test-all.sh` all layers, `make lint` green; 100% coverage gate holds
- [x] 4.2 `grep -rn gitlab-runner` over code and docs finds nothing outside this change, archived changes, and `script.deb.sh`; every hit in `openspec/specs/` sits in a requirement this change's deltas replace, and a simulated `openspec archive` on a copy leaves `openspec/specs/` with none
