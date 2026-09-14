## 1. Version plumbing

- [ ] 1.1 Add `ARG VERSION=dev` to the Go build stage of `docker/Dockerfile` and pass `-X kestrix/internal/version.Version=${VERSION}` in `-ldflags`; verify `docker build --build-arg VERSION=test . && docker run --rm <img> version` prints `test`.
- [ ] 1.2 Add `VERSION ?= $(shell git describe --tags --always --dirty)` to `Makefile` and use it in `build-release`; verify `make build-release && bin/kestrix version` prints a tag-derived string, and plain `make build` still prints `dev`.
- [ ] 1.3 Add `case "version"` to `cmd/kestrix/main.go` printing `version.Version` and list it in `help`; verify a unit test in `main_test.go` captures stdout and asserts the value, keeping `cmd/kestrix` at 100% coverage.
- [ ] 1.4 Render the version from the existing `getVersion` client function in the `Layout.jsx` footer; verify a component test asserts the text and `make ui-test` passes.

## 2. CI release stage

- [ ] 2.1 Add a `release` stage to `.gitlab-ci.yml` with a `publish-image` job (`docker:27` + `docker:27-dind`, `rules: if $CI_COMMIT_TAG =~ /^v\d+\.\d+\.\d+$/`, `needs` on every test job) that logs in with `CI_REGISTRY_USER`/`CI_REGISTRY_PASSWORD`, builds with `--build-arg VERSION=$CI_COMMIT_TAG`, and pushes `$CI_REGISTRY_IMAGE:$CI_COMMIT_TAG` and `:latest`; verify with `glab ci lint` (or the GitLab CI lint API) that the file is valid.
- [ ] 2.2 Add a `release-binary` job in the same stage that builds the stripped Linux amd64 binary with the version ldflag and a `create-release` job using `registry.gitlab.com/gitlab-org/release-cli` that extracts the `## [$CI_COMMIT_TAG]` section from `CHANGELOG.md` as the description and links the binary artifact; verify `glab ci lint` passes and the changelog extraction script is unit-tested with a fixture (`scripts/changelog-section.sh` + a bats-free shell test in `scripts/`).

## 3. Release helper and install defaults

- [ ] 3.1 Add `scripts/release.sh <X.Y.Z>` that refuses a dirty tree or a non-`main` HEAD, rewrites `## [Unreleased]` into `## [vX.Y.Z] - YYYY-MM-DD` plus a fresh empty `[Unreleased]`, commits `chore: release vX.Y.Z`, tags, and pushes commit and tag; verify by running it in a scratch clone with a fake remote and asserting the resulting CHANGELOG and tag, and that the dirty-tree case exits non-zero.
- [ ] 3.2 Change the production Compose default to `${KESTRIX_IMAGE:-registry.gitlab.com/kestrix/kestrix:latest}` and update `scripts/deploy.sh`'s default registry accordingly; verify `docker compose -f docker/docker-compose.prod.yml config` renders the registry image.
- [ ] 3.3 Update `README.md` (install from the registry, pinning a tag, `kestrix version`), `docs/usage.md` (release/rollback procedure), and `CHANGELOG.md` Unreleased/Added; verify `make test` (docs tests in `docs/docs_test.go`) passes.

## 4. First release

- [ ] 4.1 After merge to `main`, run `scripts/release.sh 0.3.0`, wait for the tag pipeline with `scripts/wait-green.sh`, then verify from outside the runner: `docker pull registry.gitlab.com/kestrix/kestrix:v0.3.0`, `docker run --rm registry.gitlab.com/kestrix/kestrix:v0.3.0 version` prints `v0.3.0`, and the Releases page shows `v0.3.0` with the binary asset.
