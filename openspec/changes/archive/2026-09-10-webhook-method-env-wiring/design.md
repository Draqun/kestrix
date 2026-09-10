## Context

See proposal.md — Why. This is a wiring fix, not an architectural one: no
new module, no data-model change. Included because `tasks.md` depends on it,
per the schema's artifact graph.

```
.env / KESTRIX_WEBHOOK_METHOD ──┐
                                 ├──► *method (cmd/kestrix/main.go) ──► Config.Signature.Method ──► verifyOne()
--method flag ───────────────────┘        (flag wins if both set)
```

`cmd/kestrix/main.go:187-188` already implements exactly this precedence for
`--secret`/`KESTRIX_WEBHOOK_SECRET`:
```go
if *secret == "" {
    *secret = env.WebhookSecret
}
```
`--method` has no equivalent line today; `*method` stays `""` when unset,
which `verifyOne` (`internal/web/webhook_verify.go:62-67`) silently reads as
`"gitlab"`.

## Goals / Non-Goals

**Goals:** `KESTRIX_WEBHOOK_METHOD` and `--method` become equivalent, exactly
as `.env.template`/`README.md` already document.

**Non-Goals:** changing verification logic, adding a third method, or
touching any other `KESTRIX_*` variable's precedence.

## Decisions

**D1: Mirror the existing `--secret`/`WebhookSecret` fallback idiom exactly,
rather than introducing a generic "flag-or-env" helper.** One more `if`
matching the established pattern is a smaller, more consistent diff than a
new abstraction for two call sites.

## Security Considerations

None beyond what already applies to the existing `--secret` fallback: an
environment variable is no less trusted than a command-line flag on the same
host. Silently defaulting to `gitlab` when nothing is configured is
unchanged behavior; this only adds a second way to reach the `hmac` setting
an operator already believed they had configured.

## Migration Plan

Deploy as a normal code + Compose-file change. No data migration. An
operator who has been passing `--method` explicitly (working around the
env-var bug) sees no change: the flag still wins. Rollback is a plain
revert.
