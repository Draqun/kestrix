#!/bin/bash
set -euo pipefail

# test_coverage.sh — parse coverage.out and enforce thresholds.
# Exit 1 if total coverage < 80% or any package is below its target.
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
    $1 ~ "^" pfx {
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

TOTAL_COV=$(echo "$TOTAL_RESULT" | awk '{print $3}')

if [ -z "$TOTAL_COV" ]; then
  echo "ERROR: could not parse total coverage from $COVERAGE_FILE"
  exit 1
fi

echo "Total coverage: ${TOTAL_COV}%"

PASS=true

# Check total threshold (80%).
if awk "BEGIN {exit !($TOTAL_COV < 80)}"; then
  echo "FAIL: total coverage ${TOTAL_COV}% is below 80% threshold"
  PASS=false
fi

# ── Per-package thresholds ─────────────────────────────────────────
# Each entry: "<path_prefix> <threshold>"
# We strip the module prefix for matching.
MODULE="gitlab.com/lunaris-bot/metaci"

check_pkg() {
  local pkg=$1
  local threshold=$2
  local full="$MODULE/$pkg"
  local result
  result=$(pkg_coverage "$COVERAGE_FILE" "$full")

  local total_stmts
  total_stmts=$(echo "$result" | awk '{print $2}')
  if [ "$total_stmts" = "0" ] || [ -z "$total_stmts" ]; then
    echo "  ${pkg}: (no coverage data, skipping)"
    return
  fi

  local pct
  pct=$(echo "$result" | awk '{print $3}')
  echo "  ${pkg}: ${pct}% (threshold: ${threshold}%)"
  if awk "BEGIN {exit !($pct < $threshold)}"; then
    echo "  FAIL: ${pkg} coverage ${pct}% is below ${threshold}% threshold"
    PASS=false
  fi
}

check_pkg "internal/domain" 0
check_pkg "internal/service" 90
check_pkg "internal/web" 85
check_pkg "internal/store" 80
check_pkg "internal/runner" 75

if [ "$PASS" = false ]; then
  echo ""
  echo "Coverage thresholds not met."
  exit 1
fi

echo ""
echo "All coverage thresholds met."