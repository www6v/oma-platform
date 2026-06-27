#!/usr/bin/env bash
# Workflow Quickstart console E2E — ephemeral harness + oma-server + Playwright.
#
# Flow: /workflows → NL generate → Run → trace viewer
#
# Usage:
#   ./scripts/workflows/smoke-workflow-console-e2e.sh
#   WORKFLOW_E2E_HEADED=1 ./scripts/workflows/smoke-workflow-console-e2e.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
MA_DIR="${ROOT_DIR}/scripts/multi-agent"
OMA_DIR="$(cd "${ROOT_DIR}/.." && pwd)"
PIPY_DIR="${OMA_DIR}/piPy-dynamic-workflows"

PLATFORM_PORT="${WORKFLOW_E2E_PORT:-8796}"
HARNESS_PORT="${WORKFLOW_E2E_HARNESS_PORT:-8797}"
PLATFORM_URL="http://127.0.0.1:${PLATFORM_PORT}"
HARNESS_URL="http://127.0.0.1:${HARNESS_PORT}"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

if command -v lsof >/dev/null 2>&1; then
  for port in "${PLATFORM_PORT}" "${HARNESS_PORT}"; do
    stale_pids="$(lsof -ti ":${port}" 2>/dev/null || true)"
    if [[ -n "${stale_pids}" ]]; then
      echo "[workflow-e2e] stopping stale listener(s) on :${port}"
      # shellcheck disable=SC2086
      kill ${stale_pids} 2>/dev/null || true
      sleep 1
    fi
  done
fi

CONSOLE_DIST="${CONSOLE_DIR:-${ROOT_DIR}/console/dist}"
if [[ ! -f "${CONSOLE_DIST}/index.html" ]] || [[ "${WORKFLOW_E2E_FORCE_BUILD:-0}" == "1" ]]; then
  echo "[workflow-e2e] building console dist…"
  "${ROOT_DIR}/scripts/build-console.sh"
fi

if [[ ! -d "${MA_DIR}/node_modules/playwright" ]]; then
  echo "[workflow-e2e] installing Playwright deps in scripts/multi-agent…"
  (cd "${MA_DIR}" && npm install --no-fund --no-audit)
fi

if ! (cd "${MA_DIR}" && npx playwright install chromium --with-deps 2>/dev/null); then
  echo "[workflow-e2e] installing Playwright chromium browser…"
  (cd "${MA_DIR}" && npx playwright install chromium)
fi

if [[ ! -x "${ROOT_DIR}/harness/.venv/bin/uvicorn" ]]; then
  echo "[workflow-e2e] syncing harness venv…"
  (cd "${ROOT_DIR}/harness" && uv sync)
fi

HARNESS_PID=""
SERVER_PID=""
cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  if [[ -n "${HARNESS_PID}" ]] && kill -0 "${HARNESS_PID}" 2>/dev/null; then
    kill "${HARNESS_PID}" 2>/dev/null || true
    wait "${HARNESS_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

QA_DB="$(mktemp "${TMPDIR:-/tmp}/workflow-console-e2e.XXXXXX")"
rm -f "${QA_DB}"

echo "[workflow-e2e] starting harness on :${HARNESS_PORT}"
(
  cd "${ROOT_DIR}/harness"
  WORKFLOW_AUTH_DISABLED=1 \
  WORKFLOW_GEN_DISABLE_AGENT=1 \
  exec .venv/bin/uvicorn oma_adapter.main:app \
    --host 127.0.0.1 \
    --port "${HARNESS_PORT}"
) &
HARNESS_PID=$!

for _ in $(seq 1 40); do
  if curl -sf "${HARNESS_URL}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -sf "${HARNESS_URL}/health" >/dev/null

echo "[workflow-e2e] starting oma-server on :${PLATFORM_PORT} (proxy → harness)"
(
  cd "${ROOT_DIR}"
  OMA_LISTEN_ADDR=":${PLATFORM_PORT}" \
  AUTH_DISABLED=1 \
  OMA_RATE_LIMIT_DISABLED=1 \
  HARNESS_URL="${HARNESS_URL}" \
  DATABASE_PATH="${QA_DB}" \
  OMA_DATABASE_PATH="${QA_DB}" \
  SANDBOX_WORKDIR="${ROOT_DIR}/data/sandboxes-workflow-e2e" \
  CONSOLE_DIR="${CONSOLE_DIST}" \
  exec "${GO_BIN}" run ./cmd/oma-server/
) &
SERVER_PID=$!

for _ in $(seq 1 40); do
  if curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -sf "${PLATFORM_URL}/health" >/dev/null
curl -sf "${PLATFORM_URL}/api/workflows/health" >/dev/null

echo "[workflow-e2e] running Playwright flow"
(
  cd "${MA_DIR}"
  CONSOLE_URL="${PLATFORM_URL}" \
  PLATFORM_URL="${PLATFORM_URL}" \
  node console-workflow-e2e.mjs
)

echo "[workflow-e2e] passed"
