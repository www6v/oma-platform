#!/usr/bin/env bash
# End-to-end smoke: mock MCP upstream -> /v1/mcp-proxy -> harness mcp__* tools -> LLM turn
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-180}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export MCP_MOCK_PORT="${MCP_MOCK_PORT:-9876}"
export MCP_MOCK_URL="http://127.0.0.1:${MCP_MOCK_PORT}/mcp"

LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
if [[ "${LISTEN_ADDR}" == :* ]]; then
  PLATFORM_URL="http://127.0.0.1${LISTEN_ADDR}"
else
  PLATFORM_URL="http://${LISTEN_ADDR}"
fi

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")

json_field() {
  local field="$1"
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$field"
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
  python3 -c 'import json,sys
raw=json.load(sys.stdin)
out=[]
for item in raw.get("data", []):
    inner=item.get("data")
    if isinstance(inner, dict):
        out.append(inner)
    elif isinstance(inner, str):
        out.append(json.loads(inner))
    else:
        out.append(item)
print(json.dumps({"data": out}))'
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
      python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
saw_tool_use=False
saw_result=False
for evt in events:
    if evt.get("type") == "session.error":
        print(evt.get("message") or evt.get("error") or "session.error")
        sys.exit(3)
    if evt.get("type") == "agent.tool_use" and evt.get("name") == "mcp__smoke__ping":
        saw_tool_use=True
    if evt.get("type") != "agent.tool_result":
        continue
    for block in evt.get("content") or []:
        text=(block.get("text") or "").strip()
        if "pong-from-mcp-smoke" in text:
            saw_result=True
            break
if saw_tool_use and saw_result:
    sys.exit(0)
sys.exit(2)' <<<"${events}"
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
python3 "${ROOT_DIR}/scripts/mock-mcp-server.py" "${MCP_MOCK_PORT}" &
MOCK_PID=$!
sleep 0.5
curl -sf -X POST "${MCP_MOCK_URL}" \
  -H "content-type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}' \
  | python3 -c 'import json,sys
r=json.load(sys.stdin)
tools=(r.get("result") or {}).get("tools") or []
assert any(t.get("name")=="ping" for t in tools), r
print("mock tools/list ok")'

echo "==> create MCP agent"
AID="$(
  api_post_json "/v1/agents" \
    "$(python3 -c 'import json,os,sys
print(json.dumps({
    "name": "smoke-mcp-agent",
    "model": sys.argv[1],
    "system_prompt": "You are a smoke test agent. When asked, call MCP tools exactly as instructed.",
    "description": "MCP e2e smoke",
    "tools": [{"type": "agent_toolset_20260401", "default_config": {"enabled": False}}],
    "mcp_servers": [{
        "name": "smoke",
        "type": "url",
        "url": os.environ["MCP_MOCK_URL"],
        "authorization_token": "smoke-local-token",
    }],
}))' "${SMOKE_MODEL}")" \
    | json_field id
)"
echo "AID=${AID}"

echo "==> create session"
SID="$(
  api_post_json "/v1/sessions" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "agent": sys.argv[1],
        "environment_id": "env-local-default",
        "title": "mcp-smoke",
    }))' "${AID}")" \
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
python3 -c 'import json,sys
r=json.load(open("/tmp/oma-mcp-proxy-smoke.json"))
assert "result" in r, r
print("mcp-proxy initialize ok")'

echo "==> send turn: call mcp__smoke__ping"
api_post_json "/v1/sessions/${SID}/events" \
  '{"events":[{"type":"user.message","content":[{"type":"text","text":"Call the MCP tool named mcp__smoke__ping with empty arguments. After you get the tool result, reply with exactly the tool result text and nothing else."}]}]}' \
  >/dev/null

echo "==> wait for mcp tool_use + pong result (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
EVENTS="$(wait_for_mcp_ping_chain "${SID}")"

python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
tool_use=""
tool_result=""
for evt in events:
    if evt.get("type") == "agent.tool_use" and evt.get("name") == "mcp__smoke__ping":
        tool_use=evt.get("name")
    if evt.get("type") == "agent.tool_result":
        for block in evt.get("content") or []:
            if block.get("type") == "text" and "pong-from-mcp-smoke" in block.get("text", ""):
                tool_result=block.get("text", "").strip()
if not tool_use:
    raise SystemExit("missing agent.tool_use mcp__smoke__ping")
if not tool_result:
    raise SystemExit("missing tool_result with pong-from-mcp-smoke")
print(f"MCP_E2E_OK tool_use={tool_use!r} tool_result={tool_result!r}")' <<<"${EVENTS}"

echo "MCP end-to-end smoke passed"
