#!/usr/bin/env bash
# Ephemeral auth-enabled stack on :8789 for console-auth-e2e.mjs (does not touch :8787).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKSPACE_ROOT="$(cd "${ROOT_DIR}/.." && pwd)"
QA_PORT="${QA_AUTH_PORT:-8789}"
AUTH_PORT="${QA_AUTH_UPSTREAM_PORT:-8788}"
QA_DB="${ROOT_DIR}/data/qa-auth-e2e.db"
QA_AUTH_DB="${ROOT_DIR}/data/qa-auth-sidecar.db"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

CONSOLE_DIST="${CONSOLE_DIR:-${WORKSPACE_ROOT}/open-managed-agents/apps/console/dist}"
if [[ ! -f "${CONSOLE_DIST}/index.html" ]]; then
  echo "Building console dist..."
  "${ROOT_DIR}/scripts/build-console.sh"
fi

export BETTER_AUTH_SECRET="${BETTER_AUTH_SECRET:-$(openssl rand -hex 32)}"
export AUTH_DATABASE_PATH="${QA_AUTH_DB}"
export OMA_DATABASE_PATH="${QA_DB}"
export DATABASE_PATH="${QA_DB}"
export PUBLIC_BASE_URL="http://127.0.0.1:${QA_PORT}"
export AUTH_LISTEN_ADDR="127.0.0.1:${AUTH_PORT}"
export AUTH_UPSTREAM_URL="http://127.0.0.1:${AUTH_PORT}"

AUTH_PID=""
SERVER_PID=""
cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  if [[ -n "${AUTH_PID}" ]] && kill -0 "${AUTH_PID}" 2>/dev/null; then
    kill "${AUTH_PID}" 2>/dev/null || true
    wait "${AUTH_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

echo "[qa-auth] starting auth sidecar on ${AUTH_UPSTREAM_URL}"
(
  cd "${ROOT_DIR}/auth-sidecar"
  if [[ ! -d node_modules ]]; then
    npm install --no-fund --no-audit
  fi
  exec node server.mjs
) &
AUTH_PID=$!
sleep 2

echo "[qa-auth] starting oma-server on :${QA_PORT} (AUTH_DISABLED=0)"
(
  cd "${ROOT_DIR}"
  OMA_LISTEN_ADDR=":${QA_PORT}" \
  AUTH_DISABLED=0 \
  CONSOLE_DIR="${CONSOLE_DIST}" \
  HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}" \
  OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-1}" \
  OMA_API_KEY="${OMA_API_KEY:-dev-key}" \
  SANDBOX_WORKDIR="${ROOT_DIR}/data/sandboxes" \
  exec "${GO_BIN}" run ./cmd/oma-server/
) &
SERVER_PID=$!

for _ in $(seq 1 30); do
  if curl -sf "http://127.0.0.1:${QA_PORT}/auth-info" >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

echo "[qa-auth] running Playwright auth E2E"
cd "${ROOT_DIR}/scripts"
CONSOLE_URL="http://127.0.0.1:${QA_PORT}" node console-auth-e2e.mjs
