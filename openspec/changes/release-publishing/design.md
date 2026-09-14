## Context

See proposal.md - Why. The real CI is `.gitlab-ci.yml` (stages analysis/test/build). `ci/pipeline.tf` is the dogfood pipeline for Kestrix itself and is not what publishes anything. `internal/version.Version` is wired for `-ldflags -X` but neither `docker/Dockerfile` nor `Makefile` sets it. `scripts/docker-build.sh` already passes `--build-arg VERSION`, which the Dockerfile ignores.

```
git tag v0.3.0 ──push──▶ GitLab CI
   analysis ─▶ test ─▶ build ─▶ release (rules: if $CI_COMMIT_TAG =~ /^v/)
                                   ├─ docker build --build-arg VERSION=$CI_COMMIT_TAG
                                   ├─ docker push $CI_REGISTRY_IMAGE:$CI_COMMIT_TAG, :latest
                                   ├─ go build -ldflags "-X ...Version=$CI_COMMIT_TAG" ─▶ kestrix (artifact)
                                   └─ release-cli create --tag-name --description "$(changelog section)"
```

## Goals / Non-Goals

**Goals:**
- One place (`CI_COMMIT_TAG`) is the source of the version string everywhere.
- Publishing needs no secrets beyond GitLab's built-in `CI_REGISTRY_USER`/`CI_REGISTRY_PASSWORD`/`CI_JOB_TOKEN`.

**Non-Goals:**
- Replacing `.gitlab-ci.yml` with the Kestrix dogfood pipeline.
- Multi-arch, signing, SBOM.

## Decisions

- **D1: GitLab Container Registry, not Docker Hub.** Zero new credentials, same host as the repo and wiki, works for a private project if the repo ever goes private. Alternative Docker Hub: needs a token secret and a second account to manage.
- **D2: Release stage uses `docker:27` image with `docker:27-dind` service**, the same pattern `runner-integration` already uses, plus `registry.gitlab.com/gitlab-org/release-cli` for the Release object. Alternative kaniko: avoids dind but adds an unfamiliar tool for a two-line job.
- **D3: `VERSION` flows via `ARG` into the Dockerfile's Go build stage.** Smallest change to make `docker-build.sh`'s existing `--build-arg` effective. `make build-release` computes `VERSION ?= $(shell git describe --tags --always --dirty)`.
- **D4: `kestrix version` is a plain `case "version"` in `main.go`** printing `version.Version`. No cobra, no `--json`.
- **D5: `scripts/release.sh` is the only entry point for cutting a release.** It enforces clean tree + `main`, edits `CHANGELOG.md` with `sed`/`awk`, commits `chore: release vX.Y.Z`, tags, pushes commit and tag. Alternative: a manual checklist - rejected, checklists drift; the wiki history shows several "MR not opened" slips.
- **D6: `latest` tracks the newest tag, not `main`.** A customer running `latest` gets released, changelog-described code only.

## Security considerations

- Registry push uses the job token scoped to this project; no long-lived token is stored.
- Images are built from the tagged commit only after all test jobs pass (`needs`), so `latest` never carries a red build.
- The binary asset is stripped (`-s -w`) as today; no debug symbols or build paths leak.

## Risks / Trade-offs

- [`latest` is mutable; a customer who pulled it cannot tell which version without asking the binary] → `kestrix version` and `/api/version` answer that; docs recommend pinning.
- [Tag pushed by mistake publishes a broken release] → `needs:` on every test job; deleting a tag and Release is a documented manual rollback in `docs/usage.md`.
- [dind flakiness on shared runners] → Same exposure `runner-integration` already has; retry once via `retry: 1`.

## Migration Plan

1. Merge this change (pipeline, Dockerfile, Makefile, subcommand, compose default, docs).
2. Run `scripts/release.sh 0.3.0` on `main`; watch the release job go green.
3. Verify `docker pull registry.gitlab.com/kestrix/kestrix:v0.3.0` from a machine outside the runner and `docker run --rm <image> version`.
Rollback: delete the tag and the Release; re-push `latest` from the previous tag if needed.
