#!/bin/bash
# test-shell.sh — run every bats-free shell test in scripts/ (*_test.sh),
# one PASS/FAIL line per file. Wired into test-all.sh's "shell" layer so a
# regression in a script that gates the release pipeline (release.sh,
# changelog-section.sh, is-latest-tag.sh) fails CI instead of only a
# developer who remembers to run it by hand.
set -euo pipefail
cd "$(dirname "$0")"

failed=0
for f in *_test.sh; do
  echo "── ${f} ──"
  if bash "$f"; then
    echo "${f}: PASS"
  else
    echo "${f}: FAIL"
    failed=1
  fi
done

exit "$failed"
