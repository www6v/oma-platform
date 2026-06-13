#!/usr/bin/env bash
# End-to-end smoke for Local ACP Runtime against oma-platform.
#
# Phase 1 (always): connect-runtime → exchange → list runtimes
# Phase 2 (always): daemon WebSocket attach + hello/ping
# Phase 3 (RUNTIME_ACP=1): real bridge daemon + claude-acp session probe
#
# Prerequisites for Phase 3:
#   - claude CLI on PATH (~/.local/bin/claude)
#   - npm install -g @agentclientprotocol/claude-agent-acp
#   - oma CLI built: pnpm --dir ../open-managed-agents/packages/cli build
#
# Usage:
#   ./scripts/smoke-runtime-e2e.sh
#   RUNTIME_ACP=0 ./scripts/smoke-runtime-e2e.sh   # skip Claude Code turn
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MONOREPO_DIR="$(cd "${ROOT_DIR}/../open-managed-agents" && pwd)"
CLI_ENTRY="${OMA_CLI_ENTRY:-${MONOREPO_DIR}/packages/cli/dist/index.js}"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  _saved_anthropic_key="${ANTHROPIC_API_KEY:-}"
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
  # .env often ships ANTHROPIC_API_KEY= empty — do not wipe a shell login key.
  if [[ -z "${ANTHROPIC_API_KEY}" ]]; then
    if [[ -n "${_saved_anthropic_key}" ]]; then
      export ANTHROPIC_API_KEY="${_saved_anthropic_key}"
    else
      unset ANTHROPIC_API_KEY
    fi
  fi
  unset _saved_anthropic_key
fi

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_INTERNAL_SECRET="${OMA_INTERNAL_SECRET:-dev-internal-secret}"
export RUNTIME_ACP="${RUNTIME_ACP:-1}"
export OMA_PROFILE="${OMA_PROFILE:-local}"
export ACP_AGENT_ID="${ACP_AGENT_ID:-claude-acp}"
export ACP_TIMEOUT_MS="${ACP_TIMEOUT_MS:-180000}"

LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
if [[ "${LISTEN_ADDR}" == :* ]]; then
  PLATFORM_URL="http://127.0.0.1${LISTEN_ADDR}"
else
  PLATFORM_URL="http://${LISTEN_ADDR}"
fi

USER_HEADERS=(
  -H "x-api-key: ${OMA_API_KEY}"
  -H "x-user-id: smoke-user"
  -H "x-tenant-id: default"
)

json_field() {
  local field="$1"
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$field"
}

log() {
  echo "[runtime-smoke] $*"
}

fail() {
  echo "[runtime-smoke] FAIL: $*" >&2
  exit 1
}

require_platform() {
  if ! curl -sf "${PLATFORM_URL}/health" >/dev/null; then
    fail "oma-server not reachable at ${PLATFORM_URL} — start platform first"
  fi
  log "platform healthy at ${PLATFORM_URL}"
}

# ── Phase 1: registration ────────────────────────────────────────────────

phase_registration() {
  local state="smoke-runtime-$(date +%s)"
  log "Phase 1: connect-runtime + exchange"

  local connect_resp
  connect_resp="$(curl -sf "${PLATFORM_URL}/v1/runtimes/connect-runtime" \
    "${USER_HEADERS[@]}" \
    -H "Content-Type: application/json" \
    -d "{\"state\":\"${state}\"}")"

  local code expires
  code="$(printf '%s' "${connect_resp}" | json_field code)"
  expires="$(printf '%s' "${connect_resp}" | json_field expires_at)"
  [[ -n "${code}" ]] || fail "missing connect code"
  log "connect code ok (expires ${expires})"

  local exchange_resp
  exchange_resp="$(curl -sf "${PLATFORM_URL}/agents/runtime/exchange" \
    -H "Content-Type: application/json" \
    -d "{
      \"code\": \"${code}\",
      \"state\": \"${state}\",
      \"machine_id\": \"smoke-machine-$(hostname -s 2>/dev/null || echo local)\",
      \"hostname\": \"$(hostname -s 2>/dev/null || echo localhost)\",
      \"os\": \"$(uname -s | tr '[:upper:]' '[:lower:]')\",
      \"version\": \"smoke-0.1.0\"
    }")"

  RUNTIME_ID="$(printf '%s' "${exchange_resp}" | json_field runtime_id)"
  RUNTIME_TOKEN="$(printf '%s' "${exchange_resp}" | json_field token)"
  AGENT_API_KEY="$(printf '%s' "${exchange_resp}" | json_field agent_api_key)"

  [[ "${RUNTIME_TOKEN}" == sk_machine_* ]] || fail "bad runtime token"
  [[ -n "${RUNTIME_ID}" ]] || fail "missing runtime_id"
  log "exchange ok runtime_id=${RUNTIME_ID}"

  local list_resp
  list_resp="$(curl -sf "${PLATFORM_URL}/v1/runtimes" "${USER_HEADERS[@]}")"
  python3 -c '
import json, sys
data = json.load(sys.stdin)
ids = [r["id"] for r in data.get("runtimes", [])]
rid = sys.argv[1]
assert rid in ids, f"runtime {rid} not in list"
print("runtime listed ok")
' "${RUNTIME_ID}" <<< "${list_resp}"
}

# ── Phase 2: daemon WS hello/ping (inline node) ───────────────────────────

phase_daemon_ws() {
  log "Phase 2: daemon WebSocket hello/ping"
  (
    cd "${ROOT_DIR}/scripts"
    RUNTIME_ID="${RUNTIME_ID}" RUNTIME_TOKEN="${RUNTIME_TOKEN}" PLATFORM_URL="${PLATFORM_URL}" \
      node --input-type=module - <<'EOF'
import WebSocket from 'ws'

const platformUrl = process.env.PLATFORM_URL
const token = process.env.RUNTIME_TOKEN
const runtimeId = process.env.RUNTIME_ID
const wsBase = platformUrl.replace(/^http(s?):\/\//, 'ws$1://').replace(/\/$/, '')
const url = `${wsBase}/agents/runtime/_attach`

const ws = new WebSocket(url, { headers: { Authorization: `Bearer ${token}` } })

function once(type, ms = 15000) {
  return new Promise((resolve, reject) => {
    const t = setTimeout(() => reject(new Error(`timeout ${type}`)), ms)
    ws.on('message', (raw) => {
      const msg = JSON.parse(String(raw))
      if (msg.type === type) {
        clearTimeout(t)
        resolve(msg)
      }
    })
  })
}

ws.on('error', (e) => {
  console.error('ws error', e.message)
  process.exit(1)
})

ws.on('open', async () => {
  ws.send(JSON.stringify({
    type: 'hello',
    version: 'smoke-probe',
    agents: [{ id: 'claude-acp' }],
    local_skills: {},
    hostname: 'smoke-host',
    os: 'darwin'
  }))
  const welcome = await once('welcome')
  if (welcome.runtime_id !== runtimeId) {
    console.error('welcome runtime_id mismatch', welcome)
    process.exit(1)
  }
  ws.send(JSON.stringify({ type: 'ping' }))
  await once('pong')
  console.log('daemon hello/ping ok')
  ws.close()
  process.exit(0)
})
EOF
  )
}

write_bridge_creds() {
  local creds_dir
  if [[ -n "${OMA_PROFILE}" ]]; then
    creds_dir="${HOME}/.oma/bridge-${OMA_PROFILE}"
  else
    creds_dir="${HOME}/.oma/bridge"
  fi
  mkdir -p "${creds_dir}"
  chmod 700 "${creds_dir}"
  local machine_id="${creds_dir}/machine-id"
  if [[ ! -f "${machine_id}" ]]; then
    python3 -c 'import uuid; print(uuid.uuid4())' > "${machine_id}"
  fi
  local mid
  mid="$(tr -d '\n' < "${machine_id}")"
  local creds_file="${creds_dir}/credentials.json"
  PLATFORM_URL="${PLATFORM_URL}" \
  RUNTIME_ID="${RUNTIME_ID}" \
  RUNTIME_TOKEN="${RUNTIME_TOKEN}" \
  AGENT_API_KEY="${AGENT_API_KEY}" \
  MID="${mid}" \
  CREDS_FILE="${creds_file}" \
  python3 - <<'PY'
import json, time, os
creds = {
  "v": 2,
  "serverUrl": os.environ["PLATFORM_URL"],
  "runtimeId": os.environ["RUNTIME_ID"],
  "token": os.environ["RUNTIME_TOKEN"],
  "tenants": [{
    "id": "default",
    "name": "Default",
    "agentApiKey": os.environ["AGENT_API_KEY"],
  }],
  "machineId": os.environ["MID"],
  "createdAt": int(time.time()),
}
path = os.environ["CREDS_FILE"]
with open(path, "w") as f:
    json.dump(creds, f, indent=2)
os.chmod(path, 0o600)
print(path)
PY
  log "wrote credentials ${creds_file} (OMA_PROFILE=${OMA_PROFILE})"
}

# ── Phase 3: real daemon + ACP turn ───────────────────────────────────────

phase_acp_turn() {
  if [[ "${RUNTIME_ACP}" != "1" ]]; then
    log "Phase 3 skipped (RUNTIME_ACP=${RUNTIME_ACP})"
    return 0
  fi

  if ! command -v claude >/dev/null 2>&1; then
    fail "claude not on PATH — install Claude Code first"
  fi
  if ! claude auth status 2>/dev/null | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d.get("loggedIn") else 1)' 2>/dev/null; then
    fail "Claude Code not logged in — run: claude auth login"
  fi
  if ! command -v claude-agent-acp >/dev/null 2>&1; then
    log "installing @agentclientprotocol/claude-agent-acp globally..."
    npm install -g @agentclientprotocol/claude-agent-acp
  fi
  if [[ ! -f "${CLI_ENTRY}" ]]; then
    log "building oma CLI at ${CLI_ENTRY}..."
    (cd "${MONOREPO_DIR}" && pnpm install --filter @openma/cli... && pnpm --filter @openma/cli build)
  fi
  [[ -f "${CLI_ENTRY}" ]] || fail "oma CLI missing at ${CLI_ENTRY}"

  if [[ ! -d "${ROOT_DIR}/scripts/node_modules/ws" ]]; then
    log "installing ws in scripts/ for probe..."
    (cd "${ROOT_DIR}/scripts" && npm install ws@8 --no-save 2>/dev/null || npm install ws@8)
  fi

  write_bridge_creds

  log "Phase 3: starting bridge daemon (profile=${OMA_PROFILE})..."
  local daemon_log="${ROOT_DIR}/data/runtime-daemon-smoke.log"
  mkdir -p "${ROOT_DIR}/data"
  : > "${daemon_log}"

  OMA_PROFILE="${OMA_PROFILE}" node "${CLI_ENTRY}" bridge daemon >>"${daemon_log}" 2>&1 &
  DAEMON_PID=$!
  cleanup_daemon() {
    if kill -0 "${DAEMON_PID}" 2>/dev/null; then
      kill "${DAEMON_PID}" 2>/dev/null || true
      wait "${DAEMON_PID}" 2>/dev/null || true
    fi
  }
  trap cleanup_daemon EXIT

  log "waiting for daemon attach (see ${daemon_log})..."
  local i
  for i in $(seq 1 30); do
    if grep -q "connected" "${daemon_log}" 2>/dev/null || \
       grep -q "welcome" "${daemon_log}" 2>/dev/null; then
      break
    fi
    sleep 1
  done
  sleep 2

  log "Phase 3: harness probe → claude-acp turn"
  (
    cd "${ROOT_DIR}/scripts"
    PLATFORM_URL="${PLATFORM_URL}" \
    OMA_INTERNAL_SECRET="${OMA_INTERNAL_SECRET}" \
    RUNTIME_ID="${RUNTIME_ID}" \
    ACP_AGENT_ID="${ACP_AGENT_ID}" \
    ACP_TIMEOUT_MS="${ACP_TIMEOUT_MS}" \
      node runtime-acp-probe.mjs
  )

  cleanup_daemon
  trap - EXIT
  log "Phase 3 OK"
}

# ── main ───────────────────────────────────────────────────────────────────

if [[ ! -d "${ROOT_DIR}/scripts/node_modules/ws" ]]; then
  (cd "${ROOT_DIR}/scripts" && npm install ws@8 --no-save 2>/dev/null || npm install ws@8)
fi
export NODE_PATH="${ROOT_DIR}/scripts/node_modules${NODE_PATH:+:${NODE_PATH}}"

require_platform
phase_registration
phase_daemon_ws
phase_acp_turn

log "ALL PASSED"
log ""
log "Note: Session POST /events still uses piPy harness — acp-proxy harness"
log "is not wired in oma-server yet. This smoke validates runtime registration,"
log "daemon attach, and local Claude Code via harness relay probe."
