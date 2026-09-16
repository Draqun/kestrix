#!/bin/bash
set -euo pipefail

# changelog-section_test.sh — tests for changelog-section.sh
# Verifies section extraction against a fixture changelog.

PASS_COUNT=0
FAIL_COUNT=0

TMPDIR=$(mktemp -d)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$SCRIPT_DIR/changelog-section.sh"

cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

FIXTURE="$TMPDIR/CHANGELOG.md"
cat > "$FIXTURE" <<'EOF'
# Changelog

All notable changes to Kestrix are documented here.

## [Unreleased]

### Added

- Nothing yet.

## [v0.3.0] - 2026-09-16

### Added

- Release publishing.
- Version subcommand.

## [v0.2.0] - 2026-01-01

### Added

- First tracked release.
EOF

run_test() {
  local name="$1"
  shift
  if "$@"; then
    echo "PASS: $name"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    echo "FAIL: $name"
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

section_equals() {
  local version="$1" expected="$2"
  [ "$(bash "$SCRIPT" "$version" "$FIXTURE")" = "$expected" ]
}

section_empty() {
  [ -z "$(bash "$SCRIPT" "$1" "$FIXTURE")" ]
}

fails_without_arg() {
  ! bash "$SCRIPT" 2>/dev/null
}

honors_changelog_file_env() {
  [ "$(CHANGELOG_FILE="$FIXTURE" bash "$SCRIPT" v0.2.0)" = "$(printf '\n### Added\n\n- First tracked release.\n')" ]
}

# --- Test 1: extracts a middle section, stopping at the next heading ---
run_test "extracts v0.3.0 section" section_equals "v0.3.0" \
  "$(printf '\n### Added\n\n- Release publishing.\n- Version subcommand.\n')"

# --- Test 2: extracts the last section, stopping at end of file ---
run_test "extracts trailing v0.2.0 section" section_equals "v0.2.0" \
  "$(printf '\n### Added\n\n- First tracked release.\n')"

# --- Test 3: unknown version yields empty output, not an error ---
run_test "unknown version yields empty output" section_empty "v9.9.9"

# --- Test 4: missing argument fails fast ---
run_test "fails without a version argument" fails_without_arg

# --- Test 5: CHANGELOG_FILE env var is honored, same convention as release.sh ---
run_test "honors CHANGELOG_FILE when no positional arg is given" honors_changelog_file_env

echo ""
echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
