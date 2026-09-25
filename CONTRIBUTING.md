# Contributing to Kestrix

Found a security vulnerability? Do not open an issue or MR for it — see
[SECURITY.md](SECURITY.md) for the private reporting channel.

## Build and test

```bash
make build          # Build the binary
make test           # Run the test suite
make test-coverage  # Unit coverage (mandatory 100% gate)
make lint           # fmt-check, vet, staticcheck
make quality        # fmt-check, vet, test — the full local quality gate
make ui-test        # UI test suite
```

Helper scripts wrap the fetch/test/CI-poll sequences used across most
sessions in this repo — see the [README's Development
section](README.md#development) for what each one does:

```bash
scripts/preflight.sh
scripts/test-all.sh
scripts/wait-green.sh
```

### Mandatory gates

See CLAUDE.md's "Verification Standards" section for the coverage and
pipeline gates (100% unit coverage, every regression layer, a red pipeline
is not mergeable) — this file doesn't re-derive them so the two don't drift.

## Workflow

This repository is worked on with [Jujutsu (`jj`)](https://jj-vcs.dev/),
colocated with Git. See CLAUDE.md's "Git / jj Workflow" section for the
fetch-before-branching rule and why a stale `main` is dangerous.
`scripts/preflight.sh` does this fetch and checks the working copy is on a
bookmark for you.

- Branch from a freshly fetched `main`.
- Use [Conventional Commits](https://www.conventionalcommits.org/) format
  for commit messages (`feat:`, `fix:`, `docs:`, etc.).
- Open one merge request per change — avoid bundling unrelated work.
- For any change that alters observable behavior, manually verify it (run
  the affected feature, not just the test suite) before opening the MR.

See the [README's Development section](README.md#development) for the full
command reference.

### When a change needs an OpenSpec proposal

Anything that alters Kestrix's observable behavior — a new capability, a
changed API response, a changed CLI flag, a changed default — needs an
OpenSpec change proposed and approved before implementation starts. Pure
refactors, docs, and test-only changes don't. See
`openspec/changes/archive/` for worked examples of past proposals, specs,
and task lists.

## Licensing of contributions

Kestrix is licensed under the [PolyForm Noncommercial License](LICENSE),
with a separate [commercial license](LICENSE-COMMERCIAL.md) available. By
submitting a contribution, you agree it is licensed under the PolyForm
Noncommercial License like the rest of the repository, and you grant the
maintainer the right to relicense your contribution under the commercial
license described in `LICENSE-COMMERCIAL.md`.
