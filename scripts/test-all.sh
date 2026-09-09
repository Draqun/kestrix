#!/usr/bin/env bash
# test-all.sh — run every regression layer this repo has, in order, and
# print one PASS/FAIL line per layer plus a final summary. Never pipes a
# test command through tail/head/grep, so a failing exit code can never be
# swallowed.
#
# Usage: scripts/test-all.sh [layer ...]
#   With no arguments, runs all layers. Otherwise runs only the named ones,
#   e.g. scripts/test-all.sh unit coverage
set -euo pipefail
cd "$(dirname "$0")/.."

declare -A LAYERS=(
  [unit]="make test"
  [coverage]="make test-coverage"
  [integration]="make test-integration"
  [e2e]="make test-e2e"
  [runner-coverage]="make runner-unit-coverage"
  [ui]="make ui-test"
)
ORDER=(unit coverage integration e2e runner-coverage ui)

selected=("$@")
if [[ ${#selected[@]} -eq 0 ]]; then
  selected=("${ORDER[@]}")
fi

failed=()
for layer in "${selected[@]}"; do
  cmd="${LAYERS[$layer]:-}"
  if [[ -z "${cmd}" ]]; then
    echo "test-all: unknown layer '${layer}' (known: ${ORDER[*]})" >&2
    exit 2
  fi
  echo "── ${layer}: ${cmd} ──"
  if ${cmd}; then
    echo "${layer}: PASS"
  else
    echo "${layer}: FAIL"
    failed+=("${layer}")
  fi
done

echo
if [[ ${#failed[@]} -eq 0 ]]; then
  echo "test-all: PASS — all layers green (${selected[*]})"
  exit 0
else
  echo "test-all: FAIL — failing layers: ${failed[*]}"
  exit 1
fi
