#!/usr/bin/env bash
# Console team E2E — ephemeral stack + Playwright Team tab verification.
#
# Starts oma-server with OMA_FAKE_HARNESS=1 (no real LLM/harness),
# seeds team fixture in SQLite, opens Session → Team tab, asserts members,
# mailbox, and shutdown_request flow.
#
# Usage:
#   ./scripts/e2e/smoke-team-console-e2e.sh
#   TEAM_E2E_HEADED=1 ./scripts/e2e/smoke-team-console-e2e.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
E2E_DIR="${ROOT_DIR}/scripts/e2e"
QA_PORT="${TEAM_E2E_PORT:-8794}"
QA_DB="${ROOT_DIR}/data/team-console-e2e.db"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

if command -v lsof >/dev/null 2>&1; then
  stale_pids="$(lsof -ti ":${QA_PORT}" 2>/dev/null || true)"
  if [[ -n "${stale_pids}" ]]; then
    echo "[team-console] stopping stale listener(s) on :${QA_PORT}"
    # shellcheck disable=SC2086
    kill ${stale_pids} 2>/dev/null || true
    sleep 1
  fi
fi

CONSOLE_DIST="${CONSOLE_DIR:-${ROOT_DIR}/console/dist}"
if [[ ! -f "${CONSOLE_DIST}/index.html" ]]; then
  echo "[team-console] building console dist…"
  "${ROOT_DIR}/scripts/build-console.sh"
fi

if [[ ! -d "${E2E_DIR}/node_modules/playwright" ]]; then
  echo "[team-console] installing Playwright deps in scripts/e2e…"
  (cd "${E2E_DIR}" && npm install --no-fund --no-audit)
fi

if ! (cd "${E2E_DIR}" && node -e "import('playwright')") >/dev/null 2>&1; then
  echo "error: playwright not available — run: (cd scripts/e2e && npm install)" >&2
  exit 1
fi

SERVER_PID=""
cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

rm -f "${QA_DB}"

echo "[team-console] starting oma-server on :${QA_PORT} (OMA_FAKE_HARNESS=1)"
(
  cd "${ROOT_DIR}"
  OMA_LISTEN_ADDR=":${QA_PORT}" \
  AUTH_DISABLED=1 \
  OMA_RATE_LIMIT_DISABLED=1 \
  OMA_FAKE_HARNESS=1 \
  OMA_API_KEY="${OMA_API_KEY:-dev-key}" \
  DATABASE_PATH="${QA_DB}" \
  OMA_DATABASE_PATH="${QA_DB}" \
  SANDBOX_WORKDIR="${ROOT_DIR}/data/sandboxes-team-e2e" \
  CONSOLE_DIR="${CONSOLE_DIST}" \
  exec "${GO_BIN}" run ./cmd/oma-server/
) &
SERVER_PID=$!

for _ in $(seq 1 40); do
  if curl -sf "http://127.0.0.1:${QA_PORT}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

if ! curl -sf "http://127.0.0.1:${QA_PORT}/health" >/dev/null 2>&1; then
  echo "error: oma-server failed to start on :${QA_PORT}" >&2
  exit 1
fi

echo "[team-console] running Playwright console-team-e2e.mjs"
cd "${E2E_DIR}"
CONSOLE_URL="http://127.0.0.1:${QA_PORT}" \
PLATFORM_URL="http://127.0.0.1:${QA_PORT}" \
OMA_API_KEY="${OMA_API_KEY:-dev-key}" \
DATABASE_PATH="${QA_DB}" \
OMA_DATABASE_PATH="${QA_DB}" \
node console-team-e2e.mjs

echo "[team-console] PASS"
