## Why

A customer has no way to obtain Kestrix. `.gitlab-ci.yml` stops at `go build` with a 30-day artifact; `docker/docker-compose.prod.yml` defaults to `kestrix:latest`, an image that exists in no registry; `docker/Dockerfile` never passes `VERSION` into `-ldflags`, so every build reports `dev`; there is no `kestrix version` subcommand; and 81 commits with several BREAKING changes sit unreleased after `v0.2.0`. Go-to-market needs a URL a customer can `docker pull` and a version they can name in a support request.

## What Changes

- A `release` stage in `.gitlab-ci.yml` that runs only on `v*` tags: builds the production image with `VERSION=<tag>`, pushes `registry.gitlab.com/kestrix/kestrix:<tag>` and `:latest` using the job's `CI_REGISTRY_*` credentials, and attaches the stripped Linux binary to a GitLab Release created from the tag with the matching `CHANGELOG.md` section as notes.
- `docker/Dockerfile` accepts `ARG VERSION` and passes `-X kestrix/internal/version.Version=$VERSION`; `make build-release` does the same from `git describe --tags`.
- New `kestrix version` subcommand printing the embedded version; `GET /api/version` keeps returning it and the UI footer shows it (the client function already exists).
- `docker/docker-compose.prod.yml` default image becomes `registry.gitlab.com/kestrix/kestrix:latest`; docs show pinning to a tag.
- A `scripts/release.sh` helper that checks the working copy is clean and on `main`, moves the `[Unreleased]` CHANGELOG section under the new version with today's date, commits, tags, and pushes the tag.
- First execution: cut `v0.3.0`.

## Non-goals

- Multi-arch images (amd64 only until a customer asks).
- Publishing to Docker Hub or GitHub Container Registry.
- Signed images / SBOM.
- Homebrew, `.deb` or other OS packages (`script.deb.sh` stays as is).
- Making the custom `ci/pipeline.tf` dogfood pipeline the release mechanism; it stays a demo.

## Capabilities

### New Capabilities

- `release-publishing`: what a tagged release produces (image tags, binary asset, release notes) and how a customer identifies the running version.

### Modified Capabilities

- `binary-identity`: gains a requirement that the binary reports its own version (`kestrix version`, `GET /api/version`) and that a released build never reports `dev`.

## Impact

- `.gitlab-ci.yml` (new stage, tag rules), `docker/Dockerfile`, `Makefile` (`build-release`, `VERSION`), `scripts/docker-build.sh` (already passes `--build-arg VERSION`), new `scripts/release.sh`.
- `cmd/kestrix/main.go` (`version` case), `internal/version` unchanged, `ui/src/components/Layout.jsx` (footer).
- `docker/docker-compose.prod.yml`, `README.md` (install from registry), `docs/usage.md`, `CHANGELOG.md` (cut `v0.3.0`).
