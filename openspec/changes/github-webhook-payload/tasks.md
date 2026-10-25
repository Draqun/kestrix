## 0. Prerequisite

- [x] 0.1 `webhook-starts-run` is implemented and on `main`. This change fills its branch/tag and deletion fields for GitHub, and relies on its run start.

## 1. Provider selection and verification

- [x] 1.1 Provider from the `X-GitHub-Event` header (design D1). `verifyDelivery` takes the provider: GitHub → `verifyHMAC` always; GitLab → the configured method (D2). Unit tests:
  - GitHub delivery with a valid signature passes under `gitlab` and under `hmac`;
  - GitHub delivery with only `X-Gitlab-Token` fails;
  - GitLab behaviour is unchanged for both methods;
  - project secret, then global secret, for both providers.
- [x] 1.2 Provider-aware probe (D3): the GitHub probe reads `repository.clone_url`, `ssh_url`, `html_url`; the GitLab-ID fallback in `matchProjects` applies only to GitLab. Test: a GitHub delivery matches a project registered with `https://github.com/org/repo.git`, including by its SSH URL.

## 2. Parsing

- [x] 2.1 `ParseGitHubPushEvent` builds a `PushEvent` per the D4 table. Parser tests use a recorded GitHub push payload fixture:
  - branch push;
  - tag push (tag flag set);
  - `deleted: true`;
  - files unioned across several commits;
  - missing optional URLs omitted rather than failing.
- [x] 2.2 Handler (D5):
  - `ping` → `200 {"status":"pong"}` after verification, with no history row;
  - other GitHub events → `400`;
  - form-encoded GitHub body → `400` naming `application/json`;
  - unverified ping → `401`.

  Handler tests for each.
- [x] 2.3 End-to-end handler test: a signed GitHub push to a registered project's `repo_ref` is recorded with the right changed set and starts a run (via `webhook-starts-run`). A push to a secondary GitHub `ci_repo` marks it changed.

## 3. UI and docs

- [x] 3.1 `WebhookSetupPanel.jsx`: GitHub steps (payload URL, `application/json`, secret, "Just the push event"), and a note that GitHub deliveries always use `X-Hub-Signature-256`. Component test. Run vitest memory-capped, one file at a time; compare booleans, never DOM nodes, inside `waitFor`.
- [x] 3.2 `docs/webhook-receiver.md`: GitHub setup, provider selection, ping and content type, the 2048-commit limit next to GitLab's 20. `CHANGELOG.md` `[Unreleased]`: `Added` entry.

## 4. Verification

- [x] 4.1 `scripts/test-all.sh` all layers, `make lint`, `make ui-lint` green; 100% unit coverage gate holds.
- [ ] 4.2 Manual:
  - point a real GitHub repository's webhook at a reachable Kestrix (e.g. through a tunnel);
  - the ping shows as delivered in GitHub;
  - a push to `repo_ref` starts a run;
  - a wrong secret shows a `401` delivery;
  - content type left at form-encoded shows the `application/json` message.
