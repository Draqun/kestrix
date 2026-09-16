#!/bin/bash
set -euo pipefail

# release_test.sh — tests for release.sh
# Runs the real script against a scratch clone with a fake remote, so it
# exercises actual git plumbing instead of mocking it.

PASS_COUNT=0
FAIL_COUNT=0

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RELEASE_SCRIPT="$SCRIPT_DIR/release.sh"

TMPDIR=$(mktemp -d)
cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

pass() { echo "PASS: $1"; PASS_COUNT=$((PASS_COUNT + 1)); }
fail() { echo "FAIL: $1"; FAIL_COUNT=$((FAIL_COUNT + 1)); }

UNRELEASED_CHANGELOG='# Changelog

All notable changes to Kestrix are documented here.

## [Unreleased]

### Added

- Something worth shipping.

## [v0.2.0] - 2026-01-01

### Added

- First tracked release.
'

init_repo() {
  local dir="$1"
  local remote="${dir}-remote.git"
  git init -q --bare "$remote"
  git init -q -b main "$dir"
  git -C "$dir" config user.name "Release Test"
  git -C "$dir" config user.email "release-test@example.com"
  printf '%s' "$UNRELEASED_CHANGELOG" > "$dir/CHANGELOG.md"
  git -C "$dir" add CHANGELOG.md
  git -C "$dir" commit -q -m "seed"
  git -C "$dir" remote add origin "$remote"
  git -C "$dir" push -q origin main
}

# --- Test 1: clean tree on main cuts the release ---
work1="$TMPDIR/work1"
init_repo "$work1"

if (cd "$work1" && bash "$RELEASE_SCRIPT" 0.3.0) >"$TMPDIR/out1.log" 2>&1; then
  pass "release.sh exits 0 on a clean main"
else
  fail "release.sh exits 0 on a clean main"
  cat "$TMPDIR/out1.log"
fi

today="$(date +%Y-%m-%d)"
if grep -q "^## \[Unreleased\]$" "$work1/CHANGELOG.md" && \
   grep -q "^## \[v0.3.0\] - ${today}$" "$work1/CHANGELOG.md"; then
  pass "CHANGELOG gains a fresh Unreleased and a dated v0.3.0 heading"
else
  fail "CHANGELOG gains a fresh Unreleased and a dated v0.3.0 heading"
fi

# The old Unreleased content must now live under the new version heading.
if awk '/^## \[v0.3.0\]/{f=1;next} /^## \[/{f=0} f' "$work1/CHANGELOG.md" | grep -q "Something worth shipping"; then
  pass "old Unreleased content moves under the new version heading"
else
  fail "old Unreleased content moves under the new version heading"
fi

if git -C "$work1" log -1 --format=%s | grep -qx "chore: release v0.3.0"; then
  pass "commits chore: release v0.3.0"
else
  fail "commits chore: release v0.3.0"
fi

if git -C "$work1" tag -l v0.3.0 | grep -qx v0.3.0; then
  pass "creates tag v0.3.0"
else
  fail "creates tag v0.3.0"
fi

if git -C "$work1" ls-remote --tags origin | grep -q "refs/tags/v0.3.0"; then
  pass "pushes the tag to origin"
else
  fail "pushes the tag to origin"
fi

if git -C "$work1" ls-remote origin main | grep -q "$(git -C "$work1" rev-parse main)"; then
  pass "pushes the release commit to origin/main"
else
  fail "pushes the release commit to origin/main"
fi

# --- Test 2: dirty tree refuses to run ---
work2="$TMPDIR/work2"
init_repo "$work2"
echo "uncommitted" >> "$work2/CHANGELOG.md"

if (cd "$work2" && bash "$RELEASE_SCRIPT" 0.3.0) >"$TMPDIR/out2.log" 2>&1; then
  fail "release.sh refuses a dirty tree"
else
  pass "release.sh refuses a dirty tree"
fi

if git -C "$work2" tag -l v0.3.0 | grep -qx v0.3.0; then
  fail "dirty tree run must not create a tag"
else
  pass "dirty tree run must not create a tag"
fi

# --- Test 3: off-main HEAD refuses to run ---
work3="$TMPDIR/work3"
init_repo "$work3"
git -C "$work3" checkout -q -b not-main

if (cd "$work3" && bash "$RELEASE_SCRIPT" 0.3.0) >"$TMPDIR/out3.log" 2>&1; then
  fail "release.sh refuses a non-main branch"
else
  pass "release.sh refuses a non-main branch"
fi

# --- Test 4: missing version argument refuses to run ---
work4="$TMPDIR/work4"
init_repo "$work4"

if (cd "$work4" && bash "$RELEASE_SCRIPT") >"$TMPDIR/out4.log" 2>&1; then
  fail "release.sh refuses a missing version argument"
else
  pass "release.sh refuses a missing version argument"
fi

# --- Test 5: malformed version argument refuses to run ---
work5="$TMPDIR/work5"
init_repo "$work5"

if (cd "$work5" && bash "$RELEASE_SCRIPT" not-a-version) >"$TMPDIR/out5.log" 2>&1; then
  fail "release.sh refuses a malformed version argument"
else
  pass "release.sh refuses a malformed version argument"
fi

# --- Test 6: changelog without an [Unreleased] section refuses to run ---
work6="$TMPDIR/work6"
init_repo "$work6"
sed -i.bak 's/^## \[Unreleased\]$/## [Something Else]/' "$work6/CHANGELOG.md"
rm -f "$work6/CHANGELOG.md.bak"
git -C "$work6" commit -q -am "drop Unreleased heading"
git -C "$work6" push -q origin main

if (cd "$work6" && bash "$RELEASE_SCRIPT" 0.3.0) >"$TMPDIR/out6.log" 2>&1; then
  fail "release.sh refuses a changelog with no [Unreleased] section"
else
  pass "release.sh refuses a changelog with no [Unreleased] section"
fi

if git -C "$work6" tag -l v0.3.0 | grep -qx v0.3.0; then
  fail "missing-Unreleased run must not create a tag"
else
  pass "missing-Unreleased run must not create a tag"
fi

echo ""
echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
