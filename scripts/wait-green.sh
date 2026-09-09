#!/usr/bin/env bash
# wait-green.sh — poll the current branch's GitLab pipeline until it
# finishes, then print PASS/FAIL plus which jobs failed. Read-only: it
# never merges or pushes anything, so it's safe to run unattended.
#
# Usage: scripts/wait-green.sh [branch] [--interval SECONDS] [--max-cycles N]
#   branch defaults to the current jj/git branch. interval defaults to 30s,
#   max-cycles to 40 (~20 minutes) — after that it reports TIMEOUT and stops
#   instead of looping forever.
set -euo pipefail

interval=30
max_cycles=40
branch=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --interval) interval="$2"; shift 2 ;;
    --max-cycles) max_cycles="$2"; shift 2 ;;
    *) branch="$1"; shift ;;
  esac
done

if [[ -z "${branch}" ]]; then
  branch="$(git branch --show-current)"
fi
if [[ -z "${branch}" ]]; then
  echo "wait-green: FAIL — no branch given and none checked out (detached HEAD?)" >&2
  exit 2
fi

echo "wait-green: watching pipeline for '${branch}' (poll ${interval}s, max ${max_cycles} cycles)"

cycle=0
while (( cycle < max_cycles )); do
  cycle=$((cycle + 1))
  pipeline_json="$(glab api "projects/:id/pipelines?ref=${branch}&per_page=1" 2>/dev/null || true)"
  status="$(echo "${pipeline_json}" | jq -r '.[0].status // empty' 2>/dev/null || true)"
  pipeline_id="$(echo "${pipeline_json}" | jq -r '.[0].id // empty' 2>/dev/null || true)"
  web_url="$(echo "${pipeline_json}" | jq -r '.[0].web_url // empty' 2>/dev/null || true)"

  if [[ -z "${status}" ]]; then
    echo "wait-green: no pipeline found yet for '${branch}' (cycle ${cycle}/${max_cycles})"
    sleep "${interval}"
    continue
  fi

  case "${status}" in
    success)
      echo "wait-green: PASS — pipeline ${pipeline_id} green (${web_url})"
      exit 0
      ;;
    failed|canceled)
      echo "wait-green: FAIL — pipeline ${pipeline_id} ${status} (${web_url})"
      echo "wait-green: failed jobs:"
      glab api "projects/:id/pipelines/${pipeline_id}/jobs?scope=failed" 2>/dev/null \
        | jq -r '.[] | "  - " + .name + "  " + .web_url' || true
      exit 1
      ;;
    running|pending|created|waiting_for_resource|preparing)
      echo "wait-green: ${status} — pipeline ${pipeline_id} (cycle ${cycle}/${max_cycles})"
      sleep "${interval}"
      ;;
    *)
      echo "wait-green: unknown status '${status}' for pipeline ${pipeline_id} (cycle ${cycle}/${max_cycles})"
      sleep "${interval}"
      ;;
  esac
done

echo "wait-green: TIMEOUT — still not finished after ${max_cycles} cycles" >&2
exit 3
