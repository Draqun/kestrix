#!/bin/bash
set -euo pipefail

# is-latest-tag_test.sh — tests for is-latest-tag.sh
# Runs against a scratch repo with a handful of semver tags.

PASS_COUNT=0
FAIL_COUNT=0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$SCRIPT_DIR/is-latest-tag.sh"

TMPDIR=$(mktemp -d)
cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

git init -q -b main "$TMPDIR"
git -C "$TMPDIR" config user.name t
git -C "$TMPDIR" config user.email t@example.com
git -C "$TMPDIR" commit -q --allow-empty -m seed
for t in v0.2.0 v0.10.0 v0.3.0; do
  git -C "$TMPDIR" tag "$t"
done

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

run_test "v0.10.0 is the newest (numeric, not lexicographic, ordering)" \
  bash -c "cd '$TMPDIR' && bash '$SCRIPT' v0.10.0"

run_test "v0.3.0 is not the newest" \
  bash -c "cd '$TMPDIR' && ! bash '$SCRIPT' v0.3.0"

run_test "v0.2.0 is not the newest" \
  bash -c "cd '$TMPDIR' && ! bash '$SCRIPT' v0.2.0"

echo ""
echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
