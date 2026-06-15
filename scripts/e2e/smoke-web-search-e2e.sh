#!/usr/bin/env bash
# E2E: agent must call web_search (DuckDuckGo) and return a result.
#
# Prerequisites:
#   - oma-server on OMA_LISTEN_ADDR (default :8787) with OMA_FAKE_HARNESS=0
#   - LLM credentials for piPy
#   - Outbound network (DuckDuckGo)
#
# Usage:
#   ./scripts/e2e/smoke-web-search-e2e.sh
#   ./scripts/e2e/smoke-web-search-e2e.sh http://localhost:8787
set -uo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/e2e/common.sh"

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"

BASE="${1:-$PLATFORM_URL}"
KEY="${2:-${OMA_API_KEY}}"
MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
PASS=0
FAIL=0

check() {
  local name="$1" expected="$2" actual="$3"
  if [[ "$actual" == *"$expected"* ]]; then
    echo "  ✓ $name"
    ((++PASS))
  else
    echo "  ✗ $name — expected '$expected'"
    ((++FAIL))
  fi
}

api() {
  curl -sS "$BASE$1" -H "x-api-key: $KEY" -H "content-type: application/json" "${@:2}"
}

collect_sse() {
  local sess_id="$1" timeout="${2:-120}"
  local sse_file
  sse_file=$(mktemp)
  curl -sS -N "$BASE/v1/sessions/$sess_id/events/stream" \
    -H "x-api-key: $KEY" -H "Accept: text/event-stream" \
    --max-time "$timeout" > "$sse_file" 2>/dev/null &
  local pid=$!
  sleep 1
  echo "$sse_file:$pid"
}

wait_for_idle() {
  local sse_file="$1" pid="$2" timeout="${3:-120}"
  local elapsed=0
  while [ "$elapsed" -lt "$timeout" ]; do
    if grep -q "session.status_idle\|session.error" "$sse_file" 2>/dev/null; then
      break
    fi
    sleep 2
    ((elapsed+=2))
    printf "."
  done
  echo ""
  kill "$pid" 2>/dev/null || true
  wait "$pid" 2>/dev/null || true
}

send_msg() {
  local sess_id="$1" text="$2"
  api "/v1/sessions/$sess_id/events" -X POST \
    -d "{\"events\":[{\"type\":\"user.message\",\"content\":[{\"type\":\"text\",\"text\":\"$text\"}]}]}" \
    -o /dev/null -w "%{http_code}"
}

echo "=== Web Search E2E (T17) ==="
echo "Base: $BASE"
echo "Model: $MODEL"

echo ""
echo "=== Setup ==="
AGENT=$(api /v1/agents -X POST -d "{
  \"name\":\"Web Search E2E Agent\",
  \"model\":\"$MODEL\",
  \"system\":\"You are a research assistant. For lookup questions you MUST call the web_search tool with a clear query. Do not guess from memory. After search results return, answer briefly.\",
  \"tools\":[{
    \"type\":\"agent_toolset_20260401\",
    \"default_config\":{\"enabled\":false},
    \"configs\":[
      {\"name\":\"web_search\",\"enabled\":true},
      {\"name\":\"read\",\"enabled\":true}
    ]
  }]
}")
AGENT_ID=$(echo "$AGENT" | jq -r .id)
echo "Agent: $AGENT_ID"

ENV=$(api /v1/environments -X POST -d '{
  "name":"web-search-e2e-env",
  "config":{"type":"cloud","networking":{"type":"unrestricted"}}
}')
ENV_ID=$(echo "$ENV" | jq -r .id)
echo "Env: $ENV_ID"

echo ""
echo "========================================"
echo "TEST: web_search tool invocation"
echo "========================================"

SESS=$(api /v1/sessions -X POST -d "{
  \"agent\":\"$AGENT_ID\",
  \"environment_id\":\"$ENV_ID\",
  \"title\":\"Web Search E2E Demo\"
}")
SESS_ID=$(echo "$SESS" | jq -r .id)
echo "Session: $SESS_ID"
echo "Console: ${CONSOLE_URL:-http://localhost:5173}/sessions/$SESS_ID"

IFS=: read -r SSE_FILE SSE_PID <<< "$(collect_sse "$SESS_ID" 180)"
send_msg "$SESS_ID" "Use web_search to search for Python programming language. Reply with only the domain name."
echo "Waiting for agent..."
wait_for_idle "$SSE_FILE" "$SSE_PID" 180

echo "--- Events (tail) ---"
tail -n 40 "$SSE_FILE"
echo ""

SSE=$(cat "$SSE_FILE")
check "agent used web_search" '"name":"web_search"' "$SSE"
check "web_search input has query" '"query"' "$SSE"
check "tool result received" "agent.tool_result" "$SSE"
check "session went idle" "session.status_idle" "$SSE"
check "got agent message" "agent.message" "$SSE"

if echo "$SSE" | grep -qE '"url"|\\"url\\"'; then
  echo "  ✓ search results contain url field"
  ((++PASS))
else
  echo "  ✗ search results contain url field"
  ((++FAIL))
fi

rm -f "$SSE_FILE"

echo ""
echo "=== Cleanup ==="
api "/v1/agents/$AGENT_ID" -X DELETE > /dev/null
api "/v1/environments/$ENV_ID" -X DELETE > /dev/null
echo "  ✓ Cleaned up (session kept for console inspection: $SESS_ID)"

echo ""
echo "========================================"
echo "Results: $PASS passed, $FAIL failed"
echo "========================================"

[ "$FAIL" -eq 0 ] && exit 0 || exit 1
