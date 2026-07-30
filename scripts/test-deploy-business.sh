#!/bin/bash
set -euo pipefail

echo "=== Business deployment checks ==="

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd)"
TEST_ROOT="$(mktemp -d)"
trap 'rm -rf "${TEST_ROOT}"' EXIT

SOURCE="${TEST_ROOT}/business"
BIN="${TEST_ROOT}/bin"
mkdir -p "${SOURCE}" "${BIN}"
printf '<!doctype html><title>business</title>\n' > "${SOURCE}/index.html"

cat > "${BIN}/docker" <<'EOF'
#!/bin/bash
set -euo pipefail
printf '%s\n' "$*" >> "${BUSINESS_TEST_LOG}"
EOF

cat > "${BIN}/rsync" <<'EOF'
#!/bin/bash
set -euo pipefail
printf 'rsync %s\n' "$*" >> "${BUSINESS_TEST_LOG}"
EOF

cat > "${BIN}/docker-should-not-run" <<'EOF'
#!/bin/bash
exit 1
EOF

chmod +x "${BIN}/docker" "${BIN}/rsync" "${BIN}/docker-should-not-run"

LOCAL_LOG="${TEST_ROOT}/local.log"
BUSINESS_TEST_LOG="${LOCAL_LOG}" \
  BUSINESS_SOURCE="${SOURCE}" \
  BUSINESS_COMPOSE_FILE="${REPO_ROOT}/docker/docker-compose.prod.yml" \
  BUSINESS_PORT=18082 \
  PATH="${BIN}:${PATH}" \
  bash "${REPO_ROOT}/scripts/deploy-business.sh" >/dev/null

grep -F -- "compose -f ${REPO_ROOT}/docker/docker-compose.prod.yml up -d --force-recreate business" "${LOCAL_LOG}" >/dev/null

REMOTE_LOG="${TEST_ROOT}/remote.log"
BUSINESS_TEST_LOG="${REMOTE_LOG}" \
  BUSINESS_SOURCE="${SOURCE}" \
  DEPLOY_HOST=example.test \
  BUSINESS_TARGET=/srv/business \
  PATH="${BIN}:${PATH}" \
  bash "${REPO_ROOT}/scripts/deploy-business.sh" >/dev/null

grep -F 'rsync -avz --delete' "${REMOTE_LOG}" >/dev/null
if grep -F 'docker' "${REMOTE_LOG}" >/dev/null; then
  echo "Remote deployment unexpectedly invoked Docker"
  exit 1
fi

if BUSINESS_SOURCE="${TEST_ROOT}/missing" PATH="${BIN}:${PATH}" \
  bash "${REPO_ROOT}/scripts/deploy-business.sh" >/dev/null 2>&1; then
  echo "Missing source unexpectedly succeeded"
  exit 1
fi

echo "=== Business deployment checks passed ==="
