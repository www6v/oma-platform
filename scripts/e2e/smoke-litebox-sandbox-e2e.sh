#!/usr/bin/env bash
# E2E smoke: LiteBox sandbox — bridge + ephemeral oma-server exec API.
#
# Steps:
#   1. install @boxlite-ai/boxlite
#   2. platform precheck (skip on unsupported hosts)
#   3. bridge exec smoke
#   4. ephemeral oma-server + POST /v1/sessions/:id/exec
#   5. Go sandbox unit tests (boxrun/registry)
#
# Usage:
#   ./scripts/e2e/smoke-litebox-sandbox-e2e.sh
#   LITEBOX_SMOKE_SKIP_UNSUPPORTED=1  # exit 0 when Intel Mac / no KVM
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/sandbox/litebox-env.sh"

SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"
PLATFORM_PORT="${LITEBOX_SMOKE_PORT:-8795}"
PLATFORM_URL="http://127.0.0.1:${PLATFORM_PORT}"
API_KEY="${OMA_API_KEY:-dev-key}"
MARKER="${LITEBOX_SMOKE_MARKER:-litebox-smoke-ok}"

log() {
  echo "[litebox-smoke] $*"
}

skip_unsupported() {
  log "SKIP: ${1}"
  if [[ "${LITEBOX_SMOKE_SKIP_UNSUPPORTED:-1}" == "1" ]]; then
    log "PASS (skipped on unsupported host; set LITEBOX_SMOKE_SKIP_UNSUPPORTED=0 to fail)"
    exit 0
  fi
  exit 1
}

log "install LiteBox npm bridge"
"${ROOT_DIR}/scripts/sandbox/install-litebox.sh"

precheck_rc=0
"${ROOT_DIR}/scripts/sandbox/litebox-precheck.sh" || precheck_rc=$?
if [[ "${precheck_rc}" == "2" ]]; then
  skip_unsupported "LiteBox not supported on this CPU/OS"
fi
if [[ "${precheck_rc}" != "0" ]]; then
  exit "${precheck_rc}"
fi

log "bridge exec smoke"
LITEBOX_SMOKE_MARKER="${MARKER}" \
  "${ROOT_DIR}/scripts/sandbox/smoke-litebox-bridge.sh"

SERVER_PID=""
cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

if command -v lsof >/dev/null 2>&1; then
  stale="$(lsof -ti ":${PLATFORM_PORT}" 2>/dev/null || true)"
  if [[ -n "${stale}" ]]; then
    kill ${stale} 2>/dev/null || true
    sleep 0.5
  fi
fi

QA_DB="$(mktemp "${TMPDIR:-/tmp}/litebox-smoke.XXXXXX")"
rm -f "${QA_DB}"
QA_WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/litebox-wd.XXXXXX")"

log "ephemeral oma-server on :${PLATFORM_PORT} (SANDBOX_PROVIDER=litebox)"
(
  cd "${ROOT_DIR}"
  OMA_LISTEN_ADDR=":${PLATFORM_PORT}" \
  AUTH_DISABLED=1 \
  OMA_RATE_LIMIT_DISABLED=1 \
  OMA_FAKE_HARNESS=1 \
  SANDBOX_PROVIDER=litebox \
  LITEBOX_BRIDGE_PATH="${LITEBOX_BRIDGE_PATH}" \
  SANDBOX_IMAGE="${SANDBOX_IMAGE}" \
  LITEBOX_MEMORY_MIB="${LITEBOX_MEMORY_MIB}" \
  LITEBOX_CPUS="${LITEBOX_CPUS}" \
  DATABASE_PATH="${QA_DB}" \
  OMA_DATABASE_PATH="${QA_DB}" \
  SANDBOX_WORKDIR="${QA_WORKDIR}" \
  MEMORY_DATA_DIR="${QA_WORKDIR}/memory" \
  SESSION_OUTPUTS_DIR="${QA_WORKDIR}/outputs" \
  exec "${GO_BIN}" run ./cmd/oma-server/
) &
SERVER_PID=$!

for _ in $(seq 1 60); do
  if curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -sf "${PLATFORM_URL}/health" >/dev/null

log "create agent + session"
agent_body='{"name":"litebox-smoke-agent","model":"claude-sonnet-4-20250514"}'
agent_resp="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/agents" \
    -H "content-type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "${agent_body}"
)"
agent_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${agent_resp}")"

sess_resp="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/sessions" \
    -H "content-type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "{\"agent_id\":\"${agent_id}\"}"
)"
session_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${sess_resp}")"

log "POST /v1/sessions/${session_id}/exec"
exec_body='{"command":"echo '"${MARKER}"'","timeout_ms":120000}'
exec_resp="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/sessions/${session_id}/exec" \
    -H "content-type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "${exec_body}"
)"
output="$(python3 "${SMOKE_UTILS}" json-field output <<<"${exec_resp}")"

if [[ "${output}" != *"${MARKER}"* ]]; then
  echo "error: exec output missing marker: ${output}" >&2
  exit 1
fi
log "exec output OK: ${output}"

log "Go sandbox unit tests"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/sandbox/... \
  -run 'BoxRun|Validate|Litebox|LocalExecutor' -count=1 -v

log "PASS: LiteBox sandbox smoke completed"
