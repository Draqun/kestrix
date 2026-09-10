## Why

`webhook-ingress-hardening` added `KESTRIX_WEBHOOK_METHOD` so an operator can
choose GitLab-token or HMAC signature verification without a command-line
flag, and both the `.env.template` and `README.md` document it as equivalent
to `--method`. It is not equivalent: `internal/config/environment.go:26,365`
parses the variable into `Environment.WebhookMethod`, but nothing reads that
field anywhere else in the codebase (`grep -rn WebhookMethod internal cmd`
matches only those two lines). `cmd/kestrix/main.go:187-188` gives `--secret`
an env fallback (`if *secret == "" { *secret = env.WebhookSecret }`) but
`--method` has no equivalent, and it defaults to `""` (`main.go:134`), which
`verifyOne` (`internal/web/webhook_verify.go:62-67`) silently treats as
`"gitlab"`. Neither Compose file passes `KESTRIX_WEBHOOK_METHOD` into the
container's `environment:` block, and the image's `CMD` never adds `--method`.
An operator who sets `KESTRIX_WEBHOOK_METHOD=hmac` in the documented
deployment path gets every HMAC-signed delivery rejected with a uniform
`401`, with no diagnostic pointing at the cause. The CHANGELOG already claims
this is fixed; it is not.

## What Changes

- `cmd/kestrix/main.go` falls back to `env.WebhookMethod` when `--method` is
  unset, the same idiom already used for `--secret`.
- `docker/docker-compose.yml` and `docker/docker-compose.prod.yml` pass
  `KESTRIX_WEBHOOK_METHOD` through to the `kestrix` service's environment.

## Non-goals

- Adding a third verification method, or changing GitLab/HMAC verification
  logic itself (`webhook_verify.go`) — this is a wiring gap, not a
  verification-logic gap.
- Reconciling any other `KESTRIX_*` variable's env/flag precedence beyond
  this one; only `WebhookMethod` was found disconnected.

## Capabilities

### New Capabilities

(none)

### Modified Capabilities

- `webhook-receiver`: the requirement covering configurable verification
  methods currently only guarantees `--method` selects GitLab-token vs. HMAC
  verification; it must guarantee `KESTRIX_WEBHOOK_METHOD` does too, matching
  the documented equivalence with `--secret`'s own env fallback.

## Impact

- `cmd/kestrix/main.go`: one added fallback line, mirroring the existing
  `--secret`/`WebhookSecret` pattern.
- `docker/docker-compose.yml`, `docker/docker-compose.prod.yml`: one added
  environment passthrough line each.
- No handler or verification code changes; `internal/web/webhook_verify.go`
  already does the right thing once `Config.Signature.Method` is populated.
