#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

case "$(uname -s 2>/dev/null || true)" in
  MINGW*|MSYS*|CYGWIN*) _OMA_IS_WINDOWS=1 ;;
  *) _OMA_IS_WINDOWS=0 ;;
esac

# List PIDs listening on a TCP port (empty output when the port is free).
_oma_port_listeners() {
  local port="$1"
  if [[ "${_OMA_IS_WINDOWS}" == "1" ]]; then
    netstat -ano 2>/dev/null |
      awk -v p=":${port}$" \
        '$1 == "TCP" && $2 ~ p && $4 == "LISTENING" { print $5 }' |
      sort -u
  elif command -v lsof >/dev/null 2>&1; then
    lsof -t -iTCP:"${port}" -sTCP:LISTEN 2>/dev/null || true
  elif command -v ss >/dev/null 2>&1; then
    ss -ltnp 2>/dev/null |
      awk -v p=":${port}$" \
        '$4 ~ p && match($0, /pid=[0-9]+/) { print substr($0, RSTART + 4, RLENGTH - 4) }' |
      sort -u
  fi
}

# Kill leftover processes occupying a port so the service can (re)start
# cleanly (orphaned oma-server / auth-sidecar survive script aborts).
_oma_free_port() {
  local port="$1" pid pids
  [[ "${port}" =~ ^[0-9]+$ ]] || return 0
  pids="$(_oma_port_listeners "${port}")"
  [[ -n "${pids}" ]] || return 0
  for pid in ${pids}; do
    [[ "${pid}" == "$$" ]] && continue
    echo "Killing leftover process on port ${port} (pid ${pid})..."
    if [[ "${_OMA_IS_WINDOWS}" == "1" ]]; then
      taskkill //F //PID "${pid}" >/dev/null 2>&1 || true
    else
      kill "${pid}" 2>/dev/null || true
    fi
  done
  sleep 1
}

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

CONSOLE_DIST="${CONSOLE_DIR:-${ROOT_DIR}/console/dist}"

if [[ ! -f "${CONSOLE_DIST}/index.html" ]]; then
  echo "Console dist missing at ${CONSOLE_DIST}; building..."
  "${ROOT_DIR}/scripts/build-console.sh"
fi

CONSOLE_DIST="$(cd "$(dirname "${CONSOLE_DIST}")" && pwd)/$(basename "${CONSOLE_DIST}")"

export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-1}"
export HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}"
export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
# DATABASE_PATH is legacy SQLite; prefer DATABASE_URL (MySQL) when set.
export DATABASE_PATH="${DATABASE_PATH:-${ROOT_DIR}/data/oma.db}"
export SANDBOX_WORKDIR="${SANDBOX_WORKDIR:-${ROOT_DIR}/data/sandboxes}"
export OMA_LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
export OMA_OUTBOUND_PROXY_ADDR="${OMA_OUTBOUND_PROXY_ADDR:-:8790}"
export CONSOLE_DIR="${CONSOLE_DIST}"
export AUTH_DISABLED="${AUTH_DISABLED:-0}"
export AUTH_UPSTREAM_URL="${AUTH_UPSTREAM_URL:-http://127.0.0.1:8788}"
export PUBLIC_BASE_URL="${PUBLIC_BASE_URL:-http://127.0.0.1:8787}"
export AUTH_DATABASE_PATH="${AUTH_DATABASE_PATH:-${ROOT_DIR}/data/auth.db}"
export OMA_DATABASE_PATH="${OMA_DATABASE_PATH:-${DATABASE_PATH}}"
export OMA_INTERNAL_SECRET="${OMA_INTERNAL_SECRET:-}"

# Free the service ports before starting (see _oma_free_port).
_oma_free_port "${OMA_LISTEN_ADDR##*:}"
_oma_free_port "${AUTH_UPSTREAM_URL##*:}"
_oma_free_port "${OMA_OUTBOUND_PROXY_ADDR##*:}"

if [[ -z "${DATABASE_URL:-}" ]]; then
  mkdir -p "$(dirname "${DATABASE_PATH}")"
fi
mkdir -p "${SANDBOX_WORKDIR}"

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

echo "Console UI: http://127.0.0.1:${OMA_LISTEN_ADDR##*:}/"
echo "API + static mount via ${GO_BIN} run ./cmd/oma-server/"

cd "${ROOT_DIR}"
exec "${GO_BIN}" run ./cmd/oma-server/
