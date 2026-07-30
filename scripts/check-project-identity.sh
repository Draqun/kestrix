#!/bin/bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
OLD_PROVIDER="gitlab.com/lunaris-bot"
OLD_PROJECT="metaci"
OLD_MODULE="${OLD_PROVIDER}/${OLD_PROJECT}"
OLD_IMAGE="registry.${OLD_MODULE}"

if [ "$(cd "$ROOT" && go list -m)" != "kestrix" ]; then
  echo "Project identity check failed: go list -m is not kestrix"
  exit 1
fi

if ! grep -Eq '^name: Kestrix$' "$ROOT/project.yaml" || \
  ! grep -Eq '^module: kestrix$' "$ROOT/project.yaml"; then
  echo "Project identity check failed: project.yaml has unexpected identity"
  exit 1
fi

if rg -n --hidden --fixed-strings "$OLD_MODULE" "$ROOT" \
  --glob '!openspec/**' \
  --glob '!ui/node_modules/**' \
  --glob '!ui/business/node_modules/**' \
  --glob '!node_modules/**' \
  --glob '!.git/**'; then
  echo "Project identity check failed: obsolete module path is still active"
  exit 1
fi

if rg -n --hidden --fixed-strings "$OLD_IMAGE" "$ROOT" \
  --glob '!openspec/**' \
  --glob '!ui/node_modules/**' \
  --glob '!ui/business/node_modules/**' \
  --glob '!node_modules/**' \
  --glob '!.git/**'; then
  echo "Project identity check failed: obsolete image path is still active"
  exit 1
fi

echo "Project identity check passed: Kestrix / kestrix"
