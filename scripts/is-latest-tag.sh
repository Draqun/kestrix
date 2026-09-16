#!/bin/bash
# is-latest-tag.sh <tag> — exit 0 if <tag> (vX.Y.Z) is the highest semver
# among the repository's vX.Y.Z tags, exit 1 otherwise. Used by the release
# pipeline to decide whether to also move the mutable `:latest` image tag,
# so cutting a release for an older maintenance line (or re-running the
# pipeline for an old tag) can't move `latest` backwards.
set -euo pipefail

tag="${1:?Usage: is-latest-tag.sh <tag>}"
newest="$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' | sort -V | tail -n1)"
[ "$newest" = "$tag" ]
