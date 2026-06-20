#!/usr/bin/env bash
# End-to-end smoke: mock MCP upstream -> /v1/mcp-proxy -> harness mcp__* tools -> LLM turn
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"
SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/e2e/common.sh"

if ! _e2e_is_local_target; then
  echo "SKIP: mock MCP upstream binds 127.0.0.1; remote ${PLATFORM_URL} cannot reach it"
  exit 0
fi

_e2e_ensure_model_card

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-180}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export MCP_MOCK_PORT="${MCP_MOCK_PORT:-9876}"
export MCP_MOCK_URL="http://127.0.0.1:${MCP_MOCK_PORT}/mcp"

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")

json_field() {
  local field="$1"
  python3 "${SMOKE_UTILS}" json-field "$field"
}

api_get() {
  curl -sf "${PLATFORM_URL}$1" "${API_HEADERS[@]}"
}

api_post_json() {
  local path="$1"
  local body="$2"
  curl -sf -X POST "${PLATFORM_URL}${path}" \
    -H "content-type: application/json" \
    "${API_HEADERS[@]}" \
    -d "${body}"
}

normalize_events_response() {
  python3 "${SMOKE_UTILS}" normalize-events
}

wait_for_mcp_ping_chain() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TOOL_TIMEOUT_SEC))
  local events=""
  local status=0
  local polls=0

  while (( SECONDS < deadline )); do
    events="$(
      api_get "/v1/sessions/${sid}/events?order=asc" | normalize_events_response
    )"
    status=0
    CHAIN_ERR="$(
      python3 "${SMOKE_UTILS}" check-events-mcp-ping <<<"${events}"
    )" || status=$?

    if [[ "${status}" -eq 0 ]]; then
      echo "${events}"
      return 0
    fi
    if [[ "${status}" -eq 3 ]]; then
      echo "error: harness turn failed: ${CHAIN_ERR}" >&2
      echo "${events}" >&2
      return 1
    fi
    polls=$((polls + 1))
    if (( polls % 5 == 0 )); then
      echo "   ... waiting for mcp__smoke__ping ($((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out waiting for MCP tool chain" >&2
  echo "${events}" >&2
  return 1
}

cleanup() {
  if [[ -n "${MOCK_PID:-}" ]] && kill -0 "${MOCK_PID}" 2>/dev/null; then
    kill "${MOCK_PID}" 2>/dev/null || true
    wait "${MOCK_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> preflight: platform + harness"
api_get "/health" >/dev/null
curl -sf "${HARNESS_URL:-http://127.0.0.1:8090}/health" >/dev/null
echo "platform + harness ok"

echo "==> start mock MCP upstream on ${MCP_MOCK_URL}"
python3 "${ROOT_DIR}/scripts/e2e/mock-mcp-server.py" "${MCP_MOCK_PORT}" &
MOCK_PID=$!
sleep 0.5
curl -sf -X POST "${MCP_MOCK_URL}" \
  -H "content-type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | python3 "${SMOKE_UTILS}" check-mock-tools

echo "==> create MCP agent"
AID="$(
  api_post_json "/v1/agents" \
    "$(python3 "${SMOKE_UTILS}" make-mcp-agent-body "${SMOKE_MODEL}")" \
    | json_field id
)"
echo "AID=${AID}"

echo "==> create session"
SID="$(
  api_post_json "/v1/sessions" \
    "$(python3 "${SMOKE_UTILS}" make-session-body "${AID}" "env-local-default" "mcp-smoke")" \
    | json_field id
)"
echo "SID=${SID}"

echo "==> proxy smoke: initialize via /v1/mcp-proxy"
PROXY_HTTP="$(
  curl -s -o /tmp/oma-mcp-proxy-smoke.json -w "%{http_code}" -X POST \
    "${PLATFORM_URL}/v1/mcp-proxy/${SID}/smoke" \
    -H "x-api-key: ${OMA_API_KEY}" \
    -H "content-type: application/json" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"1"}}}'
)"
if [[ "${PROXY_HTTP}" != "200" ]]; then
  echo "error: mcp-proxy status=${PROXY_HTTP}" >&2
  cat /tmp/oma-mcp-proxy-smoke.json >&2 || true
  exit 1
fi
python3 "${SMOKE_UTILS}" check-mcp-proxy

echo "==> send turn: call mcp__smoke__ping"
api_post_json "/v1/sessions/${SID}/events" \
  '{"events":[{"type":"user.message","content":[{"type":"text","text":"Call the MCP tool named mcp__smoke__ping with empty arguments. After you get the tool result, reply with exactly the tool result text and nothing else."}]}]}' \
  >/dev/null

echo "==> wait for mcp tool_use + pong result (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
EVENTS="$(wait_for_mcp_ping_chain "${SID}")"

python3 "${SMOKE_UTILS}" check-mcp-events-result <<<"${EVENTS}"

echo "MCP end-to-end smoke passed"
