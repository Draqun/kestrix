#!/bin/sh
# ldflags.sh <version> — print the -ldflags value every kestrix Go build
# uses to embed its version (Makefile's build-release, docker/Dockerfile's
# backend stage, and the release-binary CI job), so the embedded import
# path and variable name can't silently drift out of sync between the
# three copies.
set -eu

version="${1:?Usage: ldflags.sh <version>}"
printf -- '-s -w -X kestrix/internal/version.Version=%s' "$version"
