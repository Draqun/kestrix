## 1. Decisions to confirm before writing

- [x] 1.1 Confirm the vulnerability reporting address (proposed: `damian.giebas@gmail.com`, the same address as the commercial license) and whether GitLab confidential issues are offered as a second channel; verify by recording the choice in this task before `SECURITY.md` is written
  - Decision: `damian.giebas@gmail.com` (matches `LICENSE-COMMERCIAL.md`) as primary channel. Repo is hosted on GitLab (`gitlab.com:kestrix/kestrix`), so a confidential GitLab issue is offered as second channel.
- [x] 1.2 Confirm the response commitment (proposed: acknowledge within 5 business days, no fix-time promise while Kestrix is pre-1.0) and the supported version line (proposed: only the latest released tag, currently `v0.2.0`); verify by recording both in this task
  - Decision: acknowledge within 5 business days, no fix-time promise (pre-1.0). Supported version: latest released tag only, currently `v0.2.0` (confirmed via `git tag`).

## 2. SECURITY.md

- [x] 2.1 Write `SECURITY.md` with the reporting channel, what a report should contain (version from `kestrix version`, deployment mode, reproduction), and the explicit request not to open a public issue for a vulnerability; verify by reading it against the confirmed decisions from task 1
- [x] 2.2 Add the supported-versions section and the response commitment; verify the stated version matches the latest tag
- [x] 2.3 Add the product-specific section naming the areas where a finding matters most — credential storage and the master key, webhook signature verification, runner task isolation, authentication and role enforcement — each pointing at the doc that describes it. Corrected from the proposed path list: runner task isolation is documented in `docs/ci-tools.md#runner`, not `docs/deployment.md` (deployment.md has no runner-mode content); verified all four referenced paths/anchors exist.
- [x] 2.4 State the known deliberate limitations so they are not reported as vulnerabilities. Corrected against actual doc/code content, since `docs/deployment.md` says nothing about runner modes and the proposed statements didn't hold: `docs/ci-tools.md#runner` says `local` mode is not safe for mutually untrusted pipelines (not "non-production" — that label appears nowhere), and `gitlab-runner` mode is fully implemented (`internal/runner/runner.go:283-303,531-571`), config-wired, and tested — not unsupported. `SECURITY.md` states the real limitation (local mode isn't an isolation boundary; docker mode is) and notes gitlab-runner's isolation depends on the external runner's own config.

## 3. CONTRIBUTING.md

- [x] 3.1 Write the build-and-test section pointing at the existing targets (`make build`, `make test`, `make test-coverage`, `make lint`, `make quality`, `make ui-test`) and the helper scripts (`scripts/preflight.sh`, `scripts/test-all.sh`, `scripts/wait-green.sh`); verify each named target exists in the `Makefile` and each script exists in `scripts/`
- [x] 3.2 State the mandatory gates: 100% unit coverage is a gate not a goal, every regression layer must pass, and a red pipeline is not mergeable; verify the wording matches what `make test-coverage` actually enforces
- [x] 3.3 Document the workflow: colocated jj + Git, branch from fetched `main`, conventional commit format, one MR per change, and the manual-verification expectation for behavior changes; verify against the README Development section so the two do not contradict each other
- [x] 3.4 Document when a change needs an OpenSpec proposal (anything altering observable behavior) and point at `openspec/changes/archive/` for worked examples; verify the referenced directory path
- [x] 3.5 State the inbound licensing terms: contributions are accepted under the repository's [PolyForm Noncommercial License](LICENSE) and the contributor grants the maintainer the right to relicense their contribution under the commercial license described in `LICENSE-COMMERCIAL.md`; verify the wording is consistent with both files
- [x] 3.6 Link `SECURITY.md` from `CONTRIBUTING.md` so a security report never arrives as an MR; verify the link resolves

## 4. Wiring and verification

- [x] 4.1 Link both files from the README Development section; verify by rendering the section and following both links
- [x] 4.2 Confirm the change is docs-only: `git diff --stat` touches no `.go`, `.jsx`, `.yml`, or `Makefile`; verify the stat output
- [x] 4.3 Run `make lint`; verify it is clean and no test or pipeline job asserts on the repository file list
