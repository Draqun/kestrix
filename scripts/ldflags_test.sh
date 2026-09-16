#!/bin/bash
set -euo pipefail

# ldflags_test.sh — tests for ldflags.sh

PASS_COUNT=0
FAIL_COUNT=0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$SCRIPT_DIR/ldflags.sh"

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

prints_expected_flags() {
  [ "$(sh "$SCRIPT" v0.3.0)" = "-s -w -X kestrix/internal/version.Version=v0.3.0" ]
}

fails_without_version() {
  ! sh "$SCRIPT" 2>/dev/null
}

run_test "prints the expected -ldflags string" prints_expected_flags
run_test "fails without a version argument" fails_without_version

echo ""
echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
