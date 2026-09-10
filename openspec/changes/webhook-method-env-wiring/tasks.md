## 1. Wire the environment fallback

- [ ] 1.1 In `cmd/kestrix/main.go`, add `if *method == "" { *method = env.WebhookMethod }` immediately after the existing `--secret`/`WebhookSecret` fallback, before `Config.Signature.Method` is populated; verify a CLI/server-construction test asserts `Config.Signature.Method` reflects `KESTRIX_WEBHOOK_METHOD` when `--method` is not passed, and reflects `--method` when both are set.
- [ ] 1.2 Add `KESTRIX_WEBHOOK_METHOD` to the `kestrix` service's `environment:` block in `docker/docker-compose.yml` and `docker/docker-compose.prod.yml`, alongside the existing `KESTRIX_WEBHOOK_SECRET` line; verify `docker compose config` (or `docker-compose config` on this host) renders the variable into the service's resolved environment when set in `.env`.

## 2. Regression coverage

- [ ] 2.1 Add a test delivering an HMAC-signed webhook to a server constructed with `KESTRIX_WEBHOOK_METHOD=hmac` and no `--method` flag, asserting it verifies (200) and a GitLab-token-only delivery to the same server is rejected (401) — reproducing the "Method configured by environment variable" scenario end to end, not just at the config-parsing layer.
- [ ] 2.2 Run `go build ./... && go vet ./...` and the existing `internal/web` webhook test suite; confirm no regression to the `gitlab`-default behavior when neither flag nor env var is set.
