#!/usr/bin/env bash
# preflight.sh — sanity-check the working copy before starting a review/fix
# loop or a spec implementation: fetch, confirm we're not stale, confirm the
# working copy is on a named bookmark (not an anonymous/detached commit).
#
# Usage: scripts/preflight.sh
set -euo pipefail

fail() { echo "PREFLIGHT: FAIL — $1" >&2; exit 1; }
pass() { echo "PREFLIGHT: PASS — $1"; }

command -v jj >/dev/null 2>&1 || fail "jj not found on PATH"

jj git fetch >/tmp/preflight-fetch.log 2>&1 || fail "jj git fetch failed: $(tail -n5 /tmp/preflight-fetch.log)"
pass "fetched origin"

# A commit reachable from @ (including @ itself) that carries a local
# bookmark means we're on named work, not an anonymous/detached tip.
bookmarked="$(jj log -r 'heads(::@ & bookmarks())' -T 'bookmarks' --no-graph --limit 1 2>/dev/null || true)"
if [[ -z "${bookmarked}" ]]; then
  fail "no bookmark reachable from the working copy — create one with 'jj bookmark create <name>' before continuing, or confirm with the user which branch this is"
fi
pass "on bookmark: ${bookmarked}"

# main must not be behind origin, or a rebase built on it will silently
# reintroduce already-merged conflicts.
local_main="$(jj log -r 'main' -T 'commit_id' --no-graph 2>/dev/null || true)"
remote_main="$(jj log -r 'main@origin' -T 'commit_id' --no-graph 2>/dev/null || true)"
if [[ -n "${local_main}" && -n "${remote_main}" && "${local_main}" != "${remote_main}" ]]; then
  fail "local main (${local_main:0:8}) is behind main@origin (${remote_main:0:8}) — fetch already ran above; something is holding main back, check for a stuck bookmark move"
fi
pass "main matches origin"

echo "PREFLIGHT: all checks passed"
