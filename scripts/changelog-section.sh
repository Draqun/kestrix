#!/bin/bash
# changelog-section.sh <version> [changelog-file] — print the CHANGELOG.md
# section for a "## [<version>] ..." heading, excluding the heading line
# itself, up to (but not including) the next "## " heading or end of file.
# Used by CI to fill a GitLab Release description from the tag's changelog
# entry. The file defaults to $CHANGELOG_FILE, then CHANGELOG.md - the same
# override convention release.sh uses - with the positional argument, when
# given, taking precedence over both.
set -euo pipefail

version="${1:?Usage: changelog-section.sh <version> [changelog-file]}"
file="${2:-${CHANGELOG_FILE:-CHANGELOG.md}}"

awk -v version="$version" '
  BEGIN { found = 0 }
  $0 ~ "^## \\[" version "\\]" { found = 1; next }
  found && /^## / { exit }
  found { print }
' "$file"
