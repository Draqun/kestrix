## 1. Decisions to confirm before writing

- [ ] 1.1 Confirm the vulnerability reporting address (proposed: `damian.giebas@gmail.com`, the same address as the commercial license) and whether GitLab confidential issues are offered as a second channel; verify by recording the choice in this task before `SECURITY.md` is written
- [ ] 1.2 Confirm the response commitment (proposed: acknowledge within 5 business days, no fix-time promise while Kestrix is pre-1.0) and the supported version line (proposed: only the latest released tag, currently `v0.2.0`); verify by recording both in this task

## 2. SECURITY.md

- [ ] 2.1 Write `SECURITY.md` with the reporting channel, what a report should contain (version from `kestrix version`, deployment mode, reproduction), and the explicit request not to open a public issue for a vulnerability; verify by reading it against the confirmed decisions from task 1
- [ ] 2.2 Add the supported-versions section and the response commitment; verify the stated version matches the latest tag
- [ ] 2.3 Add the product-specific section naming the areas where a finding matters most — credential storage and the master key, webhook signature verification, runner task isolation, authentication and role enforcement — each pointing at the doc that describes it (`docs/repository-credentials.md`, `docs/webhook-receiver.md`, `docs/authentication.md`, `docs/deployment.md`); verify every referenced path exists
- [ ] 2.4 State the known deliberate limitations so they are not reported as vulnerabilities: `local` runner mode executes scripts on the host and is documented as non-production, and the `gitlab-runner` mode is unsupported; verify both statements against `docs/deployment.md`

## 3. CONTRIBUTING.md

- [ ] 3.1 Write the build-and-test section pointing at the existing targets (`make build`, `make test`, `make test-coverage`, `make lint`, `make quality`, `make ui-test`) and the helper scripts (`scripts/preflight.sh`, `scripts/test-all.sh`, `scripts/wait-green.sh`); verify each named target exists in the `Makefile` and each script exists in `scripts/`
- [ ] 3.2 State the mandatory gates: 100% unit coverage is a gate not a goal, every regression layer must pass, and a red pipeline is not mergeable; verify the wording matches what `make test-coverage` actually enforces
- [ ] 3.3 Document the workflow: colocated jj + Git, branch from fetched `main`, conventional commit format, one MR per change, and the manual-verification expectation for behavior changes; verify against the README Development section so the two do not contradict each other
- [ ] 3.4 Document when a change needs an OpenSpec proposal (anything altering observable behavior) and point at `openspec/changes/archive/` for worked examples; verify the referenced directory path
- [ ] 3.5 State the inbound licensing terms: contributions are accepted under the repository's [PolyForm Noncommercial License](LICENSE) and the contributor grants the maintainer the right to relicense their contribution under the commercial license described in `LICENSE-COMMERCIAL.md`; verify the wording is consistent with both files
- [ ] 3.6 Link `SECURITY.md` from `CONTRIBUTING.md` so a security report never arrives as an MR; verify the link resolves

## 4. Wiring and verification

- [ ] 4.1 Link both files from the README Development section; verify by rendering the section and following both links
- [ ] 4.2 Confirm the change is docs-only: `git diff --stat` touches no `.go`, `.jsx`, `.yml`, or `Makefile`; verify the stat output
- [ ] 4.3 Run `make lint`; verify it is clean and no test or pipeline job asserts on the repository file list
