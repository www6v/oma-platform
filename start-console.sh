#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WORKSPACE_ROOT="$(cd "${ROOT_DIR}/.." && pwd)"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

CONSOLE_DIST="${CONSOLE_DIR:-${WORKSPACE_ROOT}/open-managed-agents/apps/console/dist}"

if [[ ! -f "${CONSOLE_DIST}/index.html" ]]; then
  echo "Console dist missing at ${CONSOLE_DIST}; building..."
  "${ROOT_DIR}/scripts/build-console.sh"
fi

CONSOLE_DIST="$(cd "$(dirname "${CONSOLE_DIST}")" && pwd)/$(basename "${CONSOLE_DIST}")"

export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-1}"
export HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}"
export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export DATABASE_PATH="${DATABASE_PATH:-${ROOT_DIR}/data/oma.db}"
export SANDBOX_WORKDIR="${SANDBOX_WORKDIR:-${ROOT_DIR}/data/sandboxes}"
export OMA_LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
export CONSOLE_DIR="${CONSOLE_DIST}"
export AUTH_DISABLED="${AUTH_DISABLED:-0}"
export AUTH_UPSTREAM_URL="${AUTH_UPSTREAM_URL:-http://127.0.0.1:8788}"
export PUBLIC_BASE_URL="${PUBLIC_BASE_URL:-http://127.0.0.1:8787}"
export AUTH_DATABASE_PATH="${AUTH_DATABASE_PATH:-${ROOT_DIR}/data/auth.db}"
export OMA_DATABASE_PATH="${OMA_DATABASE_PATH:-${DATABASE_PATH}}"
export OMA_INTERNAL_SECRET="${OMA_INTERNAL_SECRET:-}"

mkdir -p "$(dirname "${DATABASE_PATH}")" "${SANDBOX_WORKDIR}"

AUTH_PID=""
cleanup() {
  if [[ -n "${AUTH_PID}" ]] && kill -0 "${AUTH_PID}" 2>/dev/null; then
    kill "${AUTH_PID}" 2>/dev/null || true
    wait "${AUTH_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

if [[ "${AUTH_DISABLED}" != "1" ]]; then
  echo "Starting auth sidecar on ${AUTH_UPSTREAM_URL}..."
  "${ROOT_DIR}/scripts/start-auth-sidecar.sh" &
  AUTH_PID=$!
  sleep 1
fi

echo "Console UI: http://127.0.0.1${OMA_LISTEN_ADDR#:}/"
echo "API + static mount via ${GO_BIN} run ./cmd/oma-server/"

cd "${ROOT_DIR}"
exec "${GO_BIN}" run ./cmd/oma-server/
