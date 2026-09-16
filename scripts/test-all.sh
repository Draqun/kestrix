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

# bash 3.2 (macOS default) has no associative arrays, so use a case lookup.
layer_cmd() {
  case "$1" in
    unit) echo "make test" ;;
    coverage) echo "make test-coverage" ;;
    integration) echo "make test-integration" ;;
    e2e) echo "make test-e2e" ;;
    runner-coverage) echo "make runner-unit-coverage" ;;
    ui) echo "make ui-test" ;;
    shell) echo "bash scripts/test-shell.sh" ;;
    *) echo "" ;;
  esac
}
ORDER=(unit coverage integration e2e runner-coverage ui shell)

selected=("$@")
if [[ ${#selected[@]} -eq 0 ]]; then
  selected=("${ORDER[@]}")
fi

failed=()
for layer in "${selected[@]}"; do
  cmd="$(layer_cmd "$layer")"
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
