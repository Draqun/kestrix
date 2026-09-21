## Why

The repository has no `SECURITY.md` and no `CONTRIBUTING.md`. Someone who finds
a vulnerability in Kestrix — a CI control plane that stores repository
credentials and runs arbitrary pipeline scripts — has no stated way to report it
except a public GitLab issue, which is the worst possible channel for that
class of bug. Someone who wants to send a patch has to infer the workflow (jj,
the 100% coverage gate, conventional commits, `make quality`) from `CLAUDE.md`,
which is written for agents, or from the README's Development section, which
documents commands and not expectations.

The licensing change makes the second file more urgent: Kestrix is now
noncommercial-licensed with a separate commercial license, so the terms under
which an outside contribution is accepted need to be stated before the first
one arrives, not after.

## What Changes

- New `SECURITY.md`: private reporting channel, what to include, expected
  response time, the supported version line, and the classes of finding that
  matter most for this product (credential storage and the master key, webhook
  signature verification, the runner's task isolation, authentication and role
  enforcement).
- New `CONTRIBUTING.md`: how to build and test, the mandatory gates
  (`make test`, 100% unit coverage, `make lint`, `make quality`), the jj/Git
  workflow and conventional commit format, the OpenSpec change workflow for
  anything that alters behavior, MR expectations, and the inbound licensing
  terms for contributions under the dual license.
- `README.md` links both files from the Development section.
- Both files are referenced from the webhook receiver's default exclude
  patterns discussion only if they change it — they do not; they are Markdown
  at the repository root and are already excluded.

## Capabilities

### New Capabilities
<!-- none -->

### Modified Capabilities
<!-- none: documentation only, no observable product behavior changes.
     `.openspec.yaml` sets skip_specs: true accordingly. -->

## Non-goals

- No `CODE_OF_CONDUCT.md`, no issue or MR templates, no CODEOWNERS.
- No CLA tooling, no bot, no signature check in CI.
- No change to how vulnerabilities are handled once reported (no embargo
  policy, no CVE process, no security advisories workflow) beyond naming the
  channel and the expected response time.
- No change to product code, CI, or any spec.

## Impact

Two new root Markdown files and a short README edit. No code, no CI job, no
spec delta, no dependency. The only decision with consequences outside the
repository is the reporting address and the response-time commitment stated in
`SECURITY.md`.
