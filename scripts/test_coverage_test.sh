#!/bin/bash
set -euo pipefail

# test_coverage_test.sh — tests for test_coverage.sh
# Verifies the coverage script correctly passes when above thresholds
# and fails when below.

PASS_COUNT=0
FAIL_COUNT=0

TMPDIR=$(mktemp -d)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COVERAGE_SCRIPT="$SCRIPT_DIR/test_coverage.sh"

cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT

# Write a mock coverage profile in go cover format:
#   <file>:<start>,<end> <stmt_count> <exec_count>
# exec_count > 0 means covered, 0 means not covered.
write_mock_profile() {
  local dest="$1"
  shift
  printf 'mode: atomic\n' > "$dest"
  printf '%s\n' "$@" >> "$dest"
}

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

# --- Test 1: coverage above all thresholds should pass ---
# Every package has 90%+ coverage, total is 90%.
mock_file_1="$TMPDIR/coverage_high.out"
write_mock_profile "$mock_file_1" \
  "kestrix/internal/service/svc.go:5.10,10.2 10 100" \
  "kestrix/internal/service/svc.go:12.10,15.2 10 90" \
  "kestrix/internal/web/handler.go:3.10,8.2 10 100" \
  "kestrix/internal/web/handler.go:10.10,13.2 10 80" \
  "kestrix/internal/store/pg.go:4.10,9.2 10 100" \
  "kestrix/internal/store/pg.go:11.10,14.2 10 85" \
  "kestrix/internal/runner/run.go:6.10,11.2 10 100" \
  "kestrix/internal/runner/run.go:13.10,16.2 10 80"

run_test "pass when coverage above all thresholds" \
  bash "$COVERAGE_SCRIPT" "$mock_file_1"

# --- Test 2: total coverage below 80% should fail ---
# service: 10/20 covered = 50%, no other packages, total = 50%.
mock_file_2="$TMPDIR/coverage_low_total.out"
write_mock_profile "$mock_file_2" \
  "kestrix/internal/service/svc.go:5.10,10.2 10 100" \
  "kestrix/internal/service/svc.go:12.10,20.2 10 0"

if bash "$COVERAGE_SCRIPT" "$mock_file_2"; then
  echo "FAIL: should fail when total coverage below 80%"
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  echo "PASS: correctly fails when total coverage below 80%"
  PASS_COUNT=$((PASS_COUNT + 1))
fi

# --- Test 3: specific package below its threshold should fail ---
# store has 50% (below 80% threshold), total is high enough.
mock_file_3="$TMPDIR/coverage_low_pkg.out"
write_mock_profile "$mock_file_3" \
  "kestrix/internal/service/svc.go:5.10,10.2 10 100" \
  "kestrix/internal/web/handler.go:3.10,8.2 10 100" \
  "kestrix/internal/store/pg.go:4.10,9.2 10 100" \
  "kestrix/internal/store/pg.go:11.10,20.2 10 0" \
  "kestrix/internal/runner/run.go:6.10,11.2 10 100"

if bash "$COVERAGE_SCRIPT" "$mock_file_3"; then
  echo "FAIL: should fail when a package is below its threshold"
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  echo "PASS: correctly fails when a package is below its threshold"
  PASS_COUNT=$((PASS_COUNT + 1))
fi

# --- Test 4: unit mode requires exact statement equality ---
# A rounded 99.0% result must never pass the 100% unit gate.
mock_file_4="$TMPDIR/coverage_unit_not_exact.out"
write_mock_profile "$mock_file_4" \
  "kestrix/internal/service/svc.go:5.10,10.2 99 1" \
  "kestrix/internal/service/svc.go:12.10,20.2 1 0"
if UNIT_ONLY=1 WEB_COVERAGE_THRESHOLD=100 bash "$COVERAGE_SCRIPT" "$mock_file_4"; then
  echo "FAIL: unit mode must reject uncovered statements"
  FAIL_COUNT=$((FAIL_COUNT + 1))
else
  echo "PASS: unit mode rejects non-exact coverage"
  PASS_COUNT=$((PASS_COUNT + 1))
fi

# --- Summary ---
echo ""
echo "Results: ${PASS_COUNT} passed, ${FAIL_COUNT} failed"

if [ "$FAIL_COUNT" -gt 0 ]; then
  exit 1
fi
