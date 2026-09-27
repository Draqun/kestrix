## Context

`handleWebhook` works in this order (`internal/web/handler_webhook.go`):
1. read the bounded body;
2. `probeWebhook` extracts the GitLab `project` object (ID and URLs) without
   trusting anything;
3. `matchProjects` finds registered projects by URL match key, then by GitLab
   project ID;
4. `verifyDelivery` checks each match's secret, or the global one, with the
   instance method (`gitlab` token or `hmac`);
5. only then `ParseGitLabPushEvent` parses the payload.

Everything after parsing works on a provider-neutral `PushEvent`.

```
body + headers
   │
   ├─ X-GitHub-Event? ── yes ─▶ provider github: probe repository.* URLs
   │                            verify: HMAC X-Hub-Signature-256 (always)
   │                            event: ping → 200, push → ParseGitHubPushEvent, other → 400
   │
   └─ no ───────────────────▶ provider gitlab: probe project.* (unchanged)
                                verify: configured method (unchanged)
                                ParseGitLabPushEvent (unchanged)
                  │
                  ▼
            PushEvent ─▶ match / classify / record / dispatch / run (shared)
```

## Decisions

### D1. The provider is chosen by header, before verification

`X-GitHub-Event` selects the GitHub probe, verifier and parser. The header
is unauthenticated, but every choice it makes still requires the secret:
- with the header: HMAC over the body;
- without it: the configured method.

A forged header therefore downgrades nothing: an attacker without the secret
fails either way. Choosing by payload shape instead would mean trusting the
body before it is verified.

### D2. GitHub deliveries are always HMAC-verified

GitHub only signs; it never sends a plain token header. Tying GitHub
deliveries to HMAC lets one instance serve both hosts without changing
`--method`, which keeps governing GitLab deliveries. The existing
`verifyHMAC` and the project-then-global secret order are reused unchanged.

### D3. A provider-neutral probe

`probeWebhook` returns match data: repository URLs, plus the GitLab project
ID when the provider is GitLab. The GitHub probe reads `repository.clone_url`,
`ssh_url` and `html_url`. The GitLab-ID fallback in `matchProjects` applies
only to GitLab deliveries.

### D4. Mapping onto `PushEvent`

| `PushEvent` | GitHub push |
|---|---|
| Repo | `repository.full_name` |
| RepoURLs | `clone_url`, `ssh_url`, `html_url` |
| Branch / is tag | `ref` (`refs/heads/`, `refs/tags/`) |
| CheckoutSHA | `after` |
| Deleted | `deleted` |
| DefaultBranch | `repository.default_branch` |
| FilesChanged | union of `commits[].added/modified/removed` |
| CommitCount | `len(commits)` |
| GitLabProjectID | 0 |

The deletion and branch-vs-tag fields come from `webhook-starts-run`. This
change fills them for GitHub.

### D5. Ping and content type

`ping` is verified like any delivery, so a misconfigured secret shows up as
a failed ping in GitHub's UI. It then answers `200 {"status":"pong"}` and is
not recorded: it is not a push, and recording it would let a caller holding
the secret evict history with pings.

GitHub's default content type is `application/x-www-form-urlencoded`. A
verified GitHub delivery with that content type answers `400` with
`set the webhook content type to application/json`. That turns the most
common setup mistake into a readable error instead of a JSON parse failure.

## Security Considerations

- Verification still precedes parsing and fails closed. D1 explains why
  selecting the provider by an unauthenticated header is safe.
- `ping` needs a valid signature too and writes nothing.
- The 401 body stays identical across providers and causes, so it does not
  reveal whether a repository is registered.

## Risks / Trade-offs

- GitHub lists at most 2048 commits per push; files changed only in later
  commits are not detected. GitLab already has a stricter limit (20
  commits). Documented, not mitigated (non-goal: compare API).
- `--method hmac` now matters only for GitLab deliveries. An operator who
  set it for GitHub keeps the same behaviour.

## Migration Plan

No schema or configuration change. Rollback: the previous image answers 400
to GitHub deliveries again.
