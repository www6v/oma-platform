#!/usr/bin/env bash
# E2E smoke: per-environment sandbox binding — prove the environment's
# config overrides the deployment-wide global config when a session runs.
#
# What this exercises (end-to-end):
#   POST /v1/environments   — create env with image Y (env-specific)
#   POST /v1/agents         — create agent
#   POST /v1/sessions       — bind session to the env above
#   POST /v1/sessions/:id/exec — trigger sandbox acquisition
#   Mock lifecycle          — records the image the platform requested
#
# Then we assert:
#   1. The sandbox create for session #1 carried image Y (env override),
#      NOT the global default image X.
#   2. A second session, created WITHOUT an environment_id, uses the
#      default environment ({"type":"local"}) — no sandbox create at all.
#      This locks in the backward-compat invariant.
#   3. A third session bound to an env that inherits from global
#      ({"type":"sandbox","sandbox":{"provider":"opensandbox"}}) gets the
#      global default image X.
#
# The lifecycle server is a mock (scripts/e2e/mock-opensandbox-lifecycle.py),
# so this test runs without a real OpenSandbox deployment.
#
# Usage:
#   ./scripts/e2e/smoke-environment-sandbox-binding-e2e.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"
MOCK_SERVER="${ROOT_DIR}/scripts/e2e/mock-opensandbox-lifecycle.py"

PLATFORM_PORT="${ENV_BINDING_SMOKE_PORT:-8812}"
PLATFORM_URL="http://127.0.0.1:${PLATFORM_PORT}"
API_KEY="${OMA_API_KEY:-dev-key}"

# Two free ports for the mock lifecycle + execd. Using fixed defaults is
# fine for local dev; override via env for CI where ports may collide.
LIFECYCLE_MOCK_PORT="${LIFECYCLE_MOCK_PORT:-18095}"
EXECD_MOCK_PORT="${EXECD_MOCK_PORT:-18096}"
MOCK_RECORD_PATH="$(mktemp "${TMPDIR:-/tmp}/env-binding-smoke.XXXXXX.jsonl")"

# Distinct images so we can tell which config was honoured.
GLOBAL_IMAGE="python:3.12-slim"         # deployment-wide default (image X)
ENV_IMAGE="env-specific-image:v1"        # per-env override     (image Y)
INHERIT_ENV_IMAGE="global-inherit-image:v2" # third env name, but no image
                                            # override → must inherit X

log() { echo "[env-binding-smoke] $*"; }

# --- port sanity ------------------------------------------------------------
if command -v lsof >/dev/null 2>&1; then
  for p in "${PLATFORM_PORT}" "${LIFECYCLE_MOCK_PORT}" "${EXECD_MOCK_PORT}"; do
    stale="$(lsof -ti ":${p}" 2>/dev/null || true)"
    if [[ -n "${stale}" ]]; then
      log "killing stale process on :${p}"
      kill ${stale} 2>/dev/null || true
      sleep 0.5
    fi
  done
fi

# --- mock lifecycle ---------------------------------------------------------
MOCK_PID=""
SERVER_PID=""
cleanup() {
  if [[ -n "${SERVER_PID}" ]] && kill -0 "${SERVER_PID}" 2>/dev/null; then
    kill "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  if [[ -n "${MOCK_PID}" ]] && kill -0 "${MOCK_PID}" 2>/dev/null; then
    kill "${MOCK_PID}" 2>/dev/null || true
    wait "${MOCK_PID}" 2>/dev/null || true
  fi
  rm -f "${MOCK_RECORD_PATH}"
}
trap cleanup EXIT INT TERM

log "start mock lifecycle on :${LIFECYCLE_MOCK_PORT} (execd :${EXECD_MOCK_PORT})"
LIFECYCLE_MOCK_PORT="${LIFECYCLE_MOCK_PORT}" \
EXECD_MOCK_PORT="${EXECD_MOCK_PORT}" \
MOCK_RECORD_PATH="${MOCK_RECORD_PATH}" \
  python3 "${MOCK_SERVER}" &
MOCK_PID=$!

for _ in $(seq 1 40); do
  if curl -sf --max-time 2 "http://127.0.0.1:${LIFECYCLE_MOCK_PORT}/__smoke__/creates" \
      >/dev/null 2>&1; then
    break
  fi
  sleep 0.25
done
if ! curl -sf --max-time 2 "http://127.0.0.1:${LIFECYCLE_MOCK_PORT}/__smoke__/creates" \
    >/dev/null 2>&1; then
  log "error: mock lifecycle never came up" >&2
  exit 1
fi

# --- oma-server -------------------------------------------------------------
QA_DB="$(mktemp "${TMPDIR:-/tmp}/env-binding-smoke-db.XXXXXX")"
rm -f "${QA_DB}"
QA_WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/env-binding-wd.XXXXXX")"

log "start ephemeral oma-server on :${PLATFORM_PORT} (global image=${GLOBAL_IMAGE})"
(
  cd "${ROOT_DIR}"
  OMA_LISTEN_ADDR=":${PLATFORM_PORT}" \
  AUTH_DISABLED=1 \
  OMA_RATE_LIMIT_DISABLED=1 \
  OMA_FAKE_HARNESS=1 \
  SANDBOX_PROVIDER=opensandbox \
  OPENSANDBOX_DOMAIN="127.0.0.1:${LIFECYCLE_MOCK_PORT}" \
  OPENSANDBOX_PROTOCOL="http" \
  OPENSANDBOX_API_KEY="" \
  OPENSANDBOX_USE_SERVER_PROXY="true" \
  OPENSANDBOX_EXECD_PORT="${EXECD_MOCK_PORT}" \
  OPENSANDBOX_IMAGE="${GLOBAL_IMAGE}" \
  OPENSANDBOX_TIMEOUT_SECONDS="3600" \
  OPENSANDBOX_CPU="500m" \
  OPENSANDBOX_MEMORY="512Mi" \
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
if ! curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
  log "error: oma-server never came up" >&2
  exit 1
fi

# --- helpers ----------------------------------------------------------------
api_post() {
  local path="$1" body="$2"
  curl -sf -X POST "${PLATFORM_URL}${path}" \
    -H "content-type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "${body}"
}

create_count() {
  curl -sf "http://127.0.0.1:${LIFECYCLE_MOCK_PORT}/__smoke__/creates" \
    | python3 -c "import json,sys; print(len(json.load(sys.stdin)['creates']))"
}

image_at() {
  local idx="$1"
  curl -sf "http://127.0.0.1:${LIFECYCLE_MOCK_PORT}/__smoke__/creates" \
    | python3 -c "
import json, sys
creates = json.load(sys.stdin)['creates']
print(creates[${idx}]['body'].get('image', {}).get('uri', ''))
"
}

# --- case 1: env with image override ---------------------------------------
log "case 1: env override → sandbox create must carry image=${ENV_IMAGE}"
env_resp="$(api_post /v1/environments "$(cat <<EOF
{
  "name":"smoke-env-override",
  "config":{
    "type":"sandbox",
    "sandbox":{
      "provider":"opensandbox",
      "opensandbox":{"image":"${ENV_IMAGE}"}
    }
  }
}
EOF
)")"
env_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${env_resp}")"
log "env_id=${env_id}"

agent_resp="$(api_post /v1/agents '{"name":"smoke-agent","model":"claude-sonnet-4-20250514"}')"
agent_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${agent_resp}")"

sess_resp="$(api_post /v1/sessions "{\"agent\":\"${agent_id}\",\"environment_id\":\"${env_id}\"}")"
session_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${sess_resp}")"
log "session_id=${session_id}"

exec_resp="$(api_post "/v1/sessions/${session_id}/exec" \
  '{"command":"echo hello","timeout_ms":30000}')"
output="$(python3 "${SMOKE_UTILS}" json-field output <<<"${exec_resp}")"
log "exec output: ${output}"

if [[ "$(create_count)" -lt 1 ]]; then
  log "error: expected at least 1 sandbox create, got $(create_count)" >&2
  exit 1
fi
got_image="$(image_at 0)"
if [[ "${got_image}" != "${ENV_IMAGE}" ]]; then
  log "error: case 1 image mismatch: got ${got_image}, want ${ENV_IMAGE}" >&2
  exit 1
fi
log "case 1 OK: image=${got_image}"

# --- case 2: default env (local) — must NOT create a sandbox -------------
log "case 2: default env → no sandbox create (backward-compat invariant)"
agent2_resp="$(api_post /v1/agents '{"name":"smoke-agent-2","model":"claude-sonnet-4-20250514"}')"
agent2_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${agent2_resp}")"

# No environment_id → store uses the default env ({"type":"local"}).
sess2_resp="$(api_post /v1/sessions "{\"agent\":\"${agent2_id}\"}")"
session2_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${sess2_resp}")"

before="$(create_count)"
exec2_resp="$(api_post "/v1/sessions/${session2_id}/exec" \
  '{"command":"echo default","timeout_ms":30000}')" || true
after="$(create_count)"

if [[ "${after}" -ne "${before}" ]]; then
  log "error: case 2 default env must NOT trigger sandbox create; creates before=${before} after=${after}" >&2
  exit 1
fi
log "case 2 OK: no sandbox create (before=${before} after=${after})"

# --- case 3: env with no image override → inherits global -----------------
log "case 3: env inherits global image → sandbox create must carry image=${GLOBAL_IMAGE}"
env3_resp="$(api_post /v1/environments "$(cat <<EOF
{
  "name":"smoke-env-inherit",
  "config":{
    "type":"sandbox",
    "sandbox":{"provider":"opensandbox"}
  }
}
EOF
)")"
env3_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${env3_resp}")"

agent3_resp="$(api_post /v1/agents '{"name":"smoke-agent-3","model":"claude-sonnet-4-20250514"}')"
agent3_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${agent3_resp}")"

sess3_resp="$(api_post /v1/sessions "{\"agent\":\"${agent3_id}\",\"environment_id\":\"${env3_id}\"}")"
session3_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${sess3_resp}")"

before3="$(create_count)"
exec3_resp="$(api_post "/v1/sessions/${session3_id}/exec" \
  '{"command":"echo inherit","timeout_ms":30000}')"
after3="$(create_count)"
if [[ "${after3}" -le "${before3}" ]]; then
  log "error: case 3 expected a new sandbox create, before=${before3} after=${after3}" >&2
  exit 1
fi
got_image3="$(image_at $((after3 - 1)))"
if [[ "${got_image3}" != "${GLOBAL_IMAGE}" ]]; then
  log "error: case 3 image mismatch: got ${got_image3}, want ${GLOBAL_IMAGE} (global inheritance)" >&2
  exit 1
fi
log "case 3 OK: image=${got_image3} (inherited from global)"

# --- case 4: full Machine.RunTurn path (not just /exec) -------------------
# Case 1-3 went through POST /v1/sessions/:id/exec, which is the direct
# command-execution path. This case proves the same per-env binding
# applies through the async worker: POST /messages → enqueue → worker
# calls Machine.RunTurn → acquires sandbox. This is the path real users
# exercise when the LLM decides to use a tool.
log "case 4: full turn via /messages → sandbox create must carry image=${ENV_IMAGE}"
agent4_resp="$(api_post /v1/agents '{"name":"smoke-agent-4","model":"claude-sonnet-4-20250514"}')"
agent4_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${agent4_resp}")"

sess4_resp="$(api_post /v1/sessions "{\"agent\":\"${agent4_id}\",\"environment_id\":\"${env_id}\"}")"
session4_id="$(python3 "${SMOKE_UTILS}" json-field id <<<"${sess4_resp}")"

before4="$(create_count)"
# POST /v1/sessions/:id/messages wraps content as user.message and
# enqueues it; the worker picks it up and runs Machine.RunTurn.
msg_resp="$(curl -sf -o /dev/null -w "%{http_code}" -X POST \
  "${PLATFORM_URL}/v1/sessions/${session4_id}/messages" \
  -H "content-type: application/json" \
  -H "x-api-key: ${API_KEY}" \
  -d '{"role":"user","content":[{"type":"text","text":"hi"}]}')"
if [[ "${msg_resp}" != "202" ]]; then
  log "error: case 4 POST /messages status=${msg_resp} (want 202)" >&2
  exit 1
fi

# Poll events until session.status_idle appears AFTER the last user.message.
deadline=$((SECONDS + 30))
while [[ ${SECONDS} -lt ${deadline} ]]; do
  events="$(curl -sf "${PLATFORM_URL}/v1/sessions/${session4_id}/events?order=asc" \
    -H "x-api-key: ${API_KEY}" || true)"
  idle_seen="$(echo "${events}" | python3 -c "
import sys, json
try:
    raw = json.load(sys.stdin)['data']
except Exception:
    print('no'); sys.exit()
last_user = -1
for i, e in enumerate(raw):
    if e.get('type') == 'user.message':
        last_user = i
tail = raw[last_user + 1:] if last_user >= 0 else raw
print('yes' if any(e.get('type') == 'session.status_idle' for e in tail) else 'no')
" 2>/dev/null || echo "no")"
  if [[ "${idle_seen}" == "yes" ]]; then
    break
  fi
  sleep 0.5
done
if [[ "${idle_seen}" != "yes" ]]; then
  log "error: case 4 turn did not complete within 30s" >&2
  exit 1
fi

after4="$(create_count)"
if [[ "${after4}" -le "${before4}" ]]; then
  log "error: case 4 expected a new sandbox create; before=${before4} after=${after4}" >&2
  exit 1
fi
got_image4="$(image_at $((after4 - 1)))"
if [[ "${got_image4}" != "${ENV_IMAGE}" ]]; then
  log "error: case 4 image mismatch: got ${got_image4}, want ${ENV_IMAGE}" >&2
  exit 1
fi
log "case 4 OK: full turn via /messages → image=${got_image4}"

# --- teardown & summary ----------------------------------------------------
log "stop server"
kill "${SERVER_PID}" 2>/dev/null || true
wait "${SERVER_PID}" 2>/dev/null || true
SERVER_PID=""
kill "${MOCK_PID}" 2>/dev/null || true
wait "${MOCK_PID}" 2>/dev/null || true
MOCK_PID=""

# --- Go unit tests for the same feature ------------------------------------
log "Go unit tests for sandbox resolver + registry + api environments"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/sandbox/... ./internal/api/... ./internal/session/... \
    -run 'Resolver|Validate|PerEnvironment|DefaultEnvironment|CreateEnvironment|UpdateEnvironment' \
    -count=1

log "PASS: per-environment sandbox binding smoke"
