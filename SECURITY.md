# Security Policy

## Reporting a vulnerability

Email **damian.giebas@gmail.com** with a description of the issue. If you
would rather not use email, open a
[GitLab confidential issue](https://gitlab.com/kestrix/kestrix/-/issues/new?issue%5Bconfidential%5D=true) —
confidential issues are visible only to project maintainers.

**Do not open a public GitHub/GitLab issue for a vulnerability.** Kestrix
stores repository credentials and executes pipeline scripts; a public report
gives an attacker a head start before a fix ships.

Include, where you can:

- The Kestrix version (`kestrix version`).
- The deployment mode (Docker Compose, bare binary, etc.) and runner mode in
  use (`local`, `docker`, or `gitlab-runner`).
- Steps to reproduce, or a minimal pipeline/request that triggers the issue.

### Response commitment

We aim to acknowledge new reports within **5 business days**. Kestrix is
pre-1.0; we do not commit to a fix timeline, but we will keep you updated as
the report is triaged.

### Supported versions

Only the latest released tag receives security fixes. As of this writing
that is `v0.2.0`. Check the latest tag on the
[repository's tags page](https://gitlab.com/kestrix/kestrix/-/tags) or with
`git tag --sort=-v:refname | head -1` before reporting against an older build.

## Areas that matter most

A finding in one of these areas is the highest priority to report privately:

- **Credential storage and the master key** — repository credentials are
  encrypted at rest under a master key; losing or exposing that key exposes
  every stored credential. See [Repository credentials](docs/repository-credentials.md).
- **Webhook signature verification** — `/webhook` must reject any delivery
  that doesn't verify against a configured secret. See
  [Webhook receiver: Authentication](docs/webhook-receiver.md#authentication).
- **Runner task isolation** — pipeline definitions are untrusted input, and
  task processes must only receive an explicit environment allowlist. See
  [CI Tools: Runner](docs/ci-tools.md#runner).
- **Authentication and role enforcement** — every `/api` route other than
  health, login/logout, and first-run setup must require a signed-in
  account with the correct role. See [Authentication and access control](docs/authentication.md).

## Known limitations (not vulnerabilities)

- **`local` runner mode** runs task scripts as the Kestrix service account
  on the host. The environment allowlist removes obvious secrets, but this
  does not make `local` mode safe for mutually untrusted pipelines — `docker`
  mode's container isolation is the boundary to rely on for that. Running
  untrusted pipeline definitions under `local` mode and reporting the
  resulting host access as a vulnerability is not necessary; it's documented
  behavior. See [CI Tools: Runner](docs/ci-tools.md#runner).
- **`gitlab-runner` mode** delegates execution to an external GitLab Runner;
  its isolation is whatever that runner is configured to provide, which is
  outside Kestrix's control.
