#!/bin/bash
# release.sh <X.Y.Z> — cut a Kestrix release: move CHANGELOG.md's
# [Unreleased] section under a new version heading dated today, leaving a
# fresh empty [Unreleased] section, then commit, tag, and push. Refuses to
# run on a dirty working copy or off main, so a release only ever ships a
# clean changelog cut of what main actually has.
set -euo pipefail

version="${1:?Usage: release.sh <X.Y.Z>}"
if ! [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "release: version must look like X.Y.Z, got '${version}'" >&2
  exit 1
fi
tag="v${version}"
changelog="${CHANGELOG_FILE:-CHANGELOG.md}"

if [ -n "$(git status --porcelain)" ]; then
  echo "release: working copy is dirty; commit or stash changes first" >&2
  exit 1
fi

branch="$(git branch --show-current)"
if [ "$branch" != "main" ]; then
  echo "release: must be run on main (current: ${branch:-detached HEAD})" >&2
  exit 1
fi

if ! grep -q '^## \[Unreleased\]' "$changelog"; then
  echo "release: no [Unreleased] section found in ${changelog}" >&2
  exit 1
fi

date="$(date +%Y-%m-%d)"
awk -v tag="$tag" -v date="$date" '
  /^## \[Unreleased\]/ {
    print "## [Unreleased]"
    print ""
    print "## [" tag "] - " date
    next
  }
  { print }
' "$changelog" > "${changelog}.tmp"
mv "${changelog}.tmp" "$changelog"

git add "$changelog"
git commit -m "chore: release ${tag}"
git tag "$tag"
git push origin main "$tag"

echo "release: cut ${tag}"
