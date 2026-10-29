# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Git / jj Workflow

This repository is worked on with [Jujutsu (`jj`)](https://jj-vcs.dev/), colocated with
Git — see the README's Development section for the basics.

- Run `jj git fetch` before creating a new bookmark or rebasing onto `main`. Never rebase
  or branch off a bookmark you haven't just fetched — a stale local `main` silently
  reintroduces already-merged conflicts (see the `openspec-roadmap` rebase in this repo's
  history: 10 of 12 commits turned out to already be in `main` under different hashes).
- Before starting any review or fix loop (`/mr-local-review`, `/mr-local-fix`), confirm the
  working copy sits on a named bookmark, not just an anonymous commit. `jj log -r '@ | (::@
  & bookmarks())' -T 'bookmarks'` (or plain `jj st` / `jj log -r @-::@`) shows it. If there's
  no bookmark reachable from `@`, stop and ask instead of guessing which branch is intended.
- `REVIEW.md` (written by `/mr-local-review`) is a local, untracked artifact. Never `git add`
  or `jj file track` it, but also never assume it survived a `jj new`/`jj commit` boundary
  without checking — untracked files can be swept up in a snapshot the same way tracked ones
  are. `jj status` before trusting its content is cheap insurance.
- End on an empty working copy: `jj commit`, or `jj describe` + `jj new`, especially after a
  push — this keeps the next session's `jj st` unambiguous.

## Verification Standards

- Never call something "verified" or "working" from a dry run, a `plan`-only step, or partial
  coverage (e.g. only `go vet`/`gofmt`). Run the real thing: `make test` (or the specific
  regression layer — `make test-integration`, `make test-e2e`, `make runner-unit-coverage`)
  and read its actual exit code.
- Never pipe a test/build command through `| tail`, `| head`, or `| grep` without
  `set -o pipefail` (or checking `${PIPESTATUS[0]}`) — it silently hides a non-zero exit
  code. `scripts/test-all.sh` and `scripts/wait-green.sh` already do this correctly; prefer
  them over ad hoc pipelines.
- 100% unit coverage is a gate here (`make test-coverage`), not a suggestion — a change that
  drops it is not done, even if `make test` itself is green.
- Before claiming a merge request is mergeable, check the pipeline for real
  (`scripts/wait-green.sh` or `glab ci status`) — a pushed branch is not a green branch.

## Multi-Task Change Tracking

For an OpenSpec change (or any task list with more than a handful of items), keep the task
list itself as the progress record — OpenSpec's own `tasks.md` checkboxes — and check items
off immediately after each one lands, not in a batch at the end. That way a session cut short
(usage limit, crash) resumes by reading `tasks.md` instead of re-deriving what's done from the
diff.

## Changelog Fragments (Changie)

This repository uses [Changie](https://changie.dev) (`.changie.yaml`, `.changes/`) for change fragments to prevent merge conflicts.
- **Never edit `CHANGELOG.md` directly** in a feature branch or MR.
- **Always provide a change fragment** in `.changes/unreleased/` before pushing or creating an MR:
  - Run `make changelog-new KIND=Added BODY="<description>"` (allowed kinds: `Added`, `Changed`, `Fixed`, `Deprecated`, `Removed`, `Security`).
  - Or manually write a file `.changes/unreleased/<name>.yaml` with `kind:` and `body:`.
- **Run the changelog gate**: `make changelog-check` (or `make changelog-check TARGET_REF=origin/<target-branch>`) before pushing.
- CI blocks MRs missing fragments unless `[skip changelog]` is present in the commit message.

## Development Scripts

`scripts/preflight.sh`, `scripts/test-all.sh`, and `scripts/wait-green.sh` wrap the common
fetch/test/CI-poll sequences used across sessions in this repo. See the README's Development
section for what each one does; use them instead of re-typing the underlying `jj`/`make`/`glab`
chains by hand so exit codes and pipeline state are checked consistently.
