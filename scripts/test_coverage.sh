#!/bin/bash
set -euo pipefail

# test_coverage.sh — parse coverage.out and enforce thresholds.
# Unit-only mode requires 100%; integration mode keeps an 80% aggregate floor.
#
# The coverage profile format (mode: atomic) is:
#   <file>:<line>.<col>,<line>.<col> <stmt_count> <covered>
# We aggregate covered_stmts / total_stmts per package for accuracy.

COVERAGE_FILE="${1:-coverage.out}"

if [ ! -f "$COVERAGE_FILE" ]; then
  echo "ERROR: coverage file $COVERAGE_FILE not found"
  exit 1
fi

export LC_ALL=C

docker_available() {
  command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1
}

# Compute coverage percentage for a given package path prefix from the
# coverage profile. Returns "0" if no data found.
#
# Coverage profile format (mode: atomic):
#   <file>:<start>,<end> <stmt_count> <exec_count>
# The last field is the execution count; 0 means not covered, >0 means covered.
#
# Usage: pkg_coverage <profile> <path_prefix>
# Outputs: "covered_stmts total_stmts percentage"
pkg_coverage() {
  local profile=$1
  local prefix=$2
  awk -v pfx="$prefix" '
    /^mode:/ { next }
    index($1, pfx) == 1 {
      n = $2 + 0          # statement count for this block
      if (n == 0) next
      covered = $3 + 0   # execution count; 0 = not covered
      total += n
      if (covered > 0) cov += n
    }
    END {
      if (total == 0) print "0 0 0"
      else printf "%d %d %.1f\n", cov, total, (cov / total) * 100
    }
  ' "$profile"
}

# ── Total coverage ─────────────────────────────────────────────────
TOTAL_RESULT=$(awk '
  /^mode:/ { next }
  {
    n = $2 + 0
    if (n == 0) next
    covered = $3 + 0
    total += n
    if (covered > 0) cov += n
  }
  END {
    if (total == 0) print "0 0 0"
    else printf "%d %d %.1f\n", cov, total, (cov / total) * 100
  }
' "$COVERAGE_FILE")

TOTAL_COVERED=$(echo "$TOTAL_RESULT" | awk '{print $1}')
TOTAL_STATEMENTS=$(echo "$TOTAL_RESULT" | awk '{print $2}')
TOTAL_COV=$(echo "$TOTAL_RESULT" | awk '{print $3}')

if [ -z "$TOTAL_COV" ]; then
  echo "ERROR: could not parse total coverage from $COVERAGE_FILE"
  exit 1
fi

echo "Total coverage: ${TOTAL_COV}%"

PASS=true

# Unit-only coverage is a hard 100% gate. Integration profiles retain the
# historical 80% aggregate floor because external adapters may be unavailable.
TOTAL_THRESHOLD=80
if [ "${UNIT_ONLY:-0}" = "1" ]; then TOTAL_THRESHOLD=100; fi
if [ "$TOTAL_STATEMENTS" -eq 0 ]; then
  echo "FAIL: coverage profile contains no statements"
  PASS=false
elif [ "${UNIT_ONLY:-0}" = "1" ] && [ "$TOTAL_COVERED" -ne "$TOTAL_STATEMENTS" ]; then
  echo "FAIL: exact unit coverage is ${TOTAL_COVERED}/${TOTAL_STATEMENTS} statements"
  PASS=false
elif [ "${UNIT_ONLY:-0}" != "1" ] && awk "BEGIN {exit !($TOTAL_COV < $TOTAL_THRESHOLD)}"; then
  echo "FAIL: total coverage ${TOTAL_COV}% is below ${TOTAL_THRESHOLD}% threshold"
  PASS=false
fi

# ── Per-package thresholds ─────────────────────────────────────────
# Each entry: "<path_prefix> <threshold>"
# We strip the module prefix for matching.
MODULE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && go list -m)"

check_pkg() {
  local pkg=$1
  local threshold=$2
  local full="$MODULE/$pkg"
  local result
  result=$(pkg_coverage "$COVERAGE_FILE" "$full")

  local total_stmts
  total_stmts=$(echo "$result" | awk '{print $2}')
  if [ "$total_stmts" = "0" ] || [ -z "$total_stmts" ]; then
    if [ "$pkg" = "internal/domain" ]; then
      echo "  ${pkg}: no executable statements"
      return
    fi
    if [ "$pkg" = "internal/runner" ] && { [ "${UNIT_ONLY:-0}" = "1" ] || ! docker_available; }; then
      echo "  ${pkg}: skipped (Docker unavailable)"
      return
    fi
    echo "  FAIL: ${pkg}: no coverage data"
    PASS=false
    return
  fi

  local pct
  pct=$(echo "$result" | awk '{print $3}')
  local covered_stmts
  covered_stmts=$(echo "$result" | awk '{print $1}')
  echo "  ${pkg}: ${pct}% (threshold: ${threshold}%)"
  if [ "${UNIT_ONLY:-0}" = "1" ] && [ "$covered_stmts" -ne "$total_stmts" ]; then
    echo "  FAIL: ${pkg} exact coverage is ${covered_stmts}/${total_stmts} statements"
    PASS=false
  elif [ "${UNIT_ONLY:-0}" != "1" ] && awk "BEGIN {exit !($pct < $threshold)}"; then
    echo "  FAIL: ${pkg} coverage ${pct}% is below ${threshold}% threshold"
    PASS=false
  fi
}

check_pkg "internal/domain" 90
if [ -z "${UNIT_ONLY:-}" ]; then
  SERVICE_THRESHOLD=90
else
  SERVICE_THRESHOLD=100
fi
check_pkg "internal/service" "$SERVICE_THRESHOLD"
check_pkg "internal/web" "${WEB_COVERAGE_THRESHOLD:-85}"
if [ "${UNIT_ONLY:-0}" = "1" ]; then
  check_pkg "internal/store" 100
else
  check_pkg "internal/store" 80
fi
check_pkg "internal/runner" 80

if [ "$PASS" = false ]; then
  echo ""
  echo "Coverage thresholds not met."
  exit 1
fi

echo ""
echo "All coverage thresholds met."
