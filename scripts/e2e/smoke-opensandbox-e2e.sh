#!/usr/bin/env bash
# E2E smoke: OpenSandbox sandbox — full round trip through oma-server.
#
# Steps:
#   1. preflight: lifecycle server + API key must be configured & reachable
#   2. record pre-existing sandbox ids so we only diff our own
#   3. ephemeral oma-server with SANDBOX_PROVIDER=opensandbox
#   4. POST /v1/agents → POST /v1/sessions → POST /v1/sessions/:id/exec
#   5. assert output carries the marker written inside the container
#   6. DELETE /v1/sessions/:id and confirm the container is gone from lifecycle
#   7. Go sandbox unit tests (OpenSandbox + registry + provider)
#
# Env:
#   OPENSANDBOX_DOMAIN / OPENSANDBOX_API_KEY must be set in .env
#   OMA_OPENSANDBOX_E2E_IMAGE (optional) override the container image
#
# Usage:
#   ./scripts/e2e/smoke-opensandbox-e2e.sh
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

SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"
PLATFORM_PORT="${OPENSANDBOX_SMOKE_PORT:-8797}"
PLATFORM_URL="http://127.0.0.1:${PLATFORM_PORT}"
API_KEY="${OMA_API_KEY:-dev-key}"
MARKER="${OPENSANDBOX_SMOKE_MARKER:-opensandbox-smoke-$(date +%s)}"
IMAGE="${OMA_OPENSANDBOX_E2E_IMAGE:-${OPENSANDBOX_IMAGE:-python:3.12-slim}}"

log() {
  echo "[opensandbox-smoke] $*"
}

# --- preflight --------------------------------------------------------------
if [[ "${SANDBOX_PROVIDER:-}" != "opensandbox" && -z "${OPENSANDBOX_DOMAIN:-}" ]]; then
  log "SKIP: OPENSANDBOX_DOMAIN not set (no live server configured)"
  exit 0
fi
if [[ -z "${OPENSANDBOX_DOMAIN:-}" ]]; then
  log "error: SANDBOX_PROVIDER=opensandbox but OPENSANDBOX_DOMAIN missing" >&2
  exit 1
fi

proto="${OPENSANDBOX_PROTOCOL:-http}"
lifecycle="${proto}://${OPENSANDBOX_DOMAIN}"
auth_header=()
if [[ -n "${OPENSANDBOX_API_KEY:-}" ]]; then
  auth_header=(-H "OPEN-SANDBOX-API-KEY: ${OPENSANDBOX_API_KEY}")
fi

log "preflight: GET ${lifecycle}/v1/sandboxes"
if ! curl -sf --max-time 10 "${auth_header[@]}" \
    "${lifecycle}/v1/sandboxes" >/dev/null; then
  log "error: lifecycle server unreachable at ${lifecycle}" >&2
  exit 1
fi

# Snapshot pre-existing sandbox ids so we can detect leaks that we created.
pre_ids="$(curl -sf --max-time 10 "${auth_header[@]}" \
  "${lifecycle}/v1/sandboxes?pageSize=200" \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print("\n".join(i["id"] for i in d.get("items",[])))')"

# --- server lifecycle -------------------------------------------------------
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

QA_DB="$(mktemp "${TMPDIR:-/tmp}/opensandbox-smoke.XXXXXX")"
rm -f "${QA_DB}"
QA_WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/opensandbox-wd.XXXXXX")"

log "ephemeral oma-server on :${PLATFORM_PORT} (SANDBOX_PROVIDER=opensandbox)"
(
  cd "${ROOT_DIR}"
  OMA_LISTEN_ADDR=":${PLATFORM_PORT}" \
  AUTH_DISABLED=1 \
  OMA_RATE_LIMIT_DISABLED=1 \
  OMA_FAKE_HARNESS=1 \
  SANDBOX_PROVIDER=opensandbox \
  OPENSANDBOX_DOMAIN="${OPENSANDBOX_DOMAIN}" \
  OPENSANDBOX_PROTOCOL="${OPENSANDBOX_PROTOCOL:-http}" \
  OPENSANDBOX_API_KEY="${OPENSANDBOX_API_KEY:-}" \
  OPENSANDBOX_USE_SERVER_PROXY="${OPENSANDBOX_USE_SERVER_PROXY:-true}" \
  OPENSANDBOX_EXECD_PORT="${OPENSANDBOX_EXECD_PORT:-44772}" \
  OPENSANDBOX_IMAGE="${IMAGE}" \
  OPENSANDBOX_TIMEOUT_SECONDS="${OPENSANDBOX_TIMEOUT_SECONDS:-3600}" \
  OPENSANDBOX_CPU="${OPENSANDBOX_CPU:-500m}" \
  OPENSANDBOX_MEMORY="${OPENSANDBOX_MEMORY:-512Mi}" \
  DATABASE_PATH="${QA_DB}" \
  OMA_DATABASE_PATH="${QA_DB}" \
  SANDBOX_WORKDIR="${QA_WORKDIR}" \
  MEMORY_DATA_DIR="${QA_WORKDIR}/memory" \
  SESSION_OUTPUTS_DIR="${QA_WORKDIR}/outputs" \
  exec "${GO_BIN}" run ./cmd/oma-server/
) &
SERVER_PID=$!

for _ in $(seq 1 80); do
  if curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -sf "${PLATFORM_URL}/health" >/dev/null

# --- exercise the sandbox ---------------------------------------------------
log "create agent + session"
agent_body='{"name":"opensandbox-smoke-agent","model":"claude-sonnet-4-20250514"}'
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
    -d "{\"agent\":\"${agent_id}\"}"
)"
session_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${sess_resp}")"
log "session_id=${session_id}"

log "POST /v1/sessions/${session_id}/exec (marker=${MARKER})"
exec_body="$(python3 -c "import json; print(json.dumps({'command':'echo ${MARKER} && uname -s && pwd','timeout_ms':120000}))")"
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
if [[ "${output}" != *"Linux"* ]]; then
  echo "error: exec output missing 'Linux' (uname -s): ${output}" >&2
  exit 1
fi
log "exec output OK: ${output}"

log "POST /v1/sessions/${session_id}/exec — read-file roundtrip"
write_body="$(python3 -c "import json; print(json.dumps({'command':'echo hello-from-sandbox > /workspace/smoke.txt && cat /workspace/smoke.txt','timeout_ms':30000}))")"
write_resp="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/sessions/${session_id}/exec" \
    -H "content-type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "${write_body}"
)"
write_out="$(python3 "${SMOKE_UTILS}" json-field output <<<"${write_resp}")"
if [[ "${write_out}" != *"hello-from-sandbox"* ]]; then
  echo "error: write-then-read output unexpected: ${write_out}" >&2
  exit 1
fi
log "file roundtrip OK: ${write_out}"

# --- teardown & leak check --------------------------------------------------
log "DELETE /v1/sessions/${session_id}"
curl -sf -X DELETE "${PLATFORM_URL}/v1/sessions/${session_id}" \
  -H "x-api-key: ${API_KEY}" >/dev/null || true

log "stop ephemeral server"
kill "${SERVER_PID}" 2>/dev/null || true
wait "${SERVER_PID}" 2>/dev/null || true
SERVER_PID=""

log "verify sandbox cleanup (wait up to 20s)"
for _ in $(seq 1 40); do
  post_ids="$(curl -sf --max-time 10 "${auth_header[@]}" \
    "${lifecycle}/v1/sandboxes?pageSize=200" \
    | python3 -c 'import json,sys; d=json.load(sys.stdin); print("\n".join(i["id"] for i in d.get("items",[])))')"
  # Diff only sandboxes tagged with our marker metadata — we can't rely on
  # id subtraction because lifecycle reuses paginated positions.
  ours="$(echo "${post_ids}" | grep -v "^$" | while read -r id; do
    meta="$(curl -sf --max-time 10 "${auth_header[@]}" \
      "${lifecycle}/v1/sandboxes/${id}" 2>/dev/null || true)"
    if echo "${meta}" | grep -q "\"oma_session_id\":\"${session_id}\""; then
      echo "${id}"
    fi
  done || true)"
  if [[ -z "${ours}" ]]; then
    break
  fi
  sleep 0.5
done
if [[ -n "${ours:-}" ]]; then
  echo "error: sandbox leak detected after session delete: ${ours}" >&2
  exit 1
fi
log "sandbox cleanup OK"

# --- Go unit tests ----------------------------------------------------------
log "Go sandbox unit tests"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/sandbox/... \
  -run 'OpenSandbox|Validate|IsRemote' -count=1 -v

log "PASS: OpenSandbox smoke completed"
