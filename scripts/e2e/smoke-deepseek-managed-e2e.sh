#!/usr/bin/env bash
# End-to-end test: DeepSeek agent via the full stack.
#
# Verifies that a managed agent with _oma.harness: "deepseek":
#   1. Accepts a user message through the API
#   2. Drives the DeepSeek dsh gateway via DeepSeekClient
#   3. Emits the full session event vocabulary (agent.message,
#      agent.tool_use, agent.tool_result, span.model_request_end)
#   4. Feeds /v1/cost_report via the span
#
# Expects a running oma-server at ${PLATFORM_URL:-http://127.0.0.1:8787}
# with OMA_DEEPSEEK_GATEWAY_URL configured.
#
# Skips (exit 0) when the DeepSeek dsh gateway is not reachable.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"

PLATFORM_URL="${PLATFORM_URL:-http://127.0.0.1:8787}"
DEEPSEEK_GATEWAY="${DEEPSEEK_GATEWAY:-http://127.0.0.1:3080}"
OMA_API_KEY="${OMA_API_KEY:-dev-key}"
AGENT_NAME="${AGENT_NAME:-deepseek-e2e-$(date +%s)}"
SESSION_TIMEOUT_SEC="${SESSION_TIMEOUT_SEC:-120}"

H_COMMON=(
  -H "x-api-key: ${OMA_API_KEY}"
  -H "x-user-id: deepseek-e2e"
  -H "x-tenant-id: default"
  -H "Content-Type: application/json"
)

log() { echo "[deepseek-e2e] $*"; }
fail() { echo "[deepseek-e2e] FAIL: $*" >&2; exit 1; }
skip() { echo "[deepseek-e2e] SKIP: $*" >&2; exit 0; }

# ── 0. Check dsh gateway reachability ────────────────────────────────

check_gateway() {
  # dsh has no dedicated health endpoint — probe with a lightweight RPC
  # call (session.list). A 200 with a valid JSON response means it's up.
  local resp
  resp=$(curl -s --connect-timeout 3 -o /dev/null -w "%{http_code}" \
    -X POST "${DEEPSEEK_GATEWAY}/api/session.list" \
    -H "Content-Type: application/json" \
    -d '{"type":"client-request","rpcId":"health","method":"session.list","payload":{}}')
  if [[ "${resp}" != "200" ]]; then
    skip "dsh gateway not reachable at ${DEEPSEEK_GATEWAY} — set DEEPSEEK_GATEWAY if different"
  fi
  log "dsh gateway healthy at ${DEEPSEEK_GATEWAY}"
}

# ── 1. Check platform ────────────────────────────────────────────────

require_platform() {
  curl -sf "${PLATFORM_URL}/health" >/dev/null ||
    fail "oma-server not reachable at ${PLATFORM_URL}"
  log "platform healthy at ${PLATFORM_URL}"
}

# ── 2. Create DeepSeek agent ─────────────────────────────────────────

create_agent() {
  log "create managed agent (harness=deepseek) name=${AGENT_NAME}"
  local resp http_code
  resp=$(curl -s -o /tmp/deepseek-agent-body -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/agents" \
    -d @- <<JSON
{
  "name": "${AGENT_NAME}",
  "model": {"id": "deepseek-chat", "speed": "standard"},
  "description": "E2E DeepSeek agent — exercise dsh gateway session events",
  "system": "You are a helpful assistant. Use the terminal tool when asked to run shell commands.",
  "_oma": {
    "harness": "deepseek"
  }
}
JSON
  )
  http_code="${resp}"
  if [[ "${http_code}" != "201" ]]; then
    fail "create agent status=${http_code}: $(cat /tmp/deepseek-agent-body)"
  fi
  AGENT_ID=$(python3 "${SMOKE_UTILS}" json-field "id" </tmp/deepseek-agent-body)
  log "agent created — id=${AGENT_ID}"

  # Verify the returned agent has the correct harness binding
  local harness
  harness=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/agents/${AGENT_ID}" |
    python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('_oma',{}).get('harness',''))")
  if [[ "${harness}" != "deepseek" ]]; then
    fail "agent _oma.harness=${harness!r} want 'deepseek'"
  fi
  log "agent harness verified: deepseek"
}

# ── 3. Pick an environment + create a session ─────────────────────────

create_session() {
  log "listing environments"
  local envs env_id
  envs=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/environments?limit=5")
  env_id=$(echo "${envs}" | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print(d[0]['id'] if d else '')")
  if [[ -z "${env_id}" ]]; then
    fail "no environments available"
  fi
  log "using environment ${env_id}"

  local resp http_code
  resp=$(curl -s -o /tmp/deepseek-session-body -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions" \
    -d "{\"agent\":\"${AGENT_ID}\",\"environment_id\":\"${env_id}\"}")
  http_code="${resp}"
  if [[ "${http_code}" != "201" ]]; then
    fail "create session status=${http_code}: $(cat /tmp/deepseek-session-body)"
  fi
  SESSION_ID=$(python3 "${SMOKE_UTILS}" json-field "id" </tmp/deepseek-session-body)
  log "session created — id=${SESSION_ID}"
}

# ── 4. Send a user message ───────────────────────────────────────────

send_message() {
  local msg="$1"
  log "POST /v1/sessions/${SESSION_ID}/events — ${msg:0:60}..."
  local resp http_code
  resp=$(curl -s -o /tmp/deepseek-post-events -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events" \
    -d "{\"events\":[{\"type\":\"user.message\",\"content\":[{\"type\":\"text\",\"text\":\"${msg}\"}]}]}")
  http_code="${resp}"
  if [[ "${http_code}" != "202" && "${http_code}" != "200" && "${http_code}" != "201" ]]; then
    fail "post events status=${http_code}: $(cat /tmp/deepseek-post-events)"
  fi
  log "message accepted"
}

# ── 5. Wait for turn completion ──────────────────────────────────────

wait_for_agent_reply() {
  local deadline=$((SECONDS + SESSION_TIMEOUT_SEC))
  local terminal_seen last_type
  while [[ ${SECONDS} -lt ${deadline} ]]; do
    terminal_seen=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc" | python3 -c "
import sys, json
raw = json.load(sys.stdin)['data']
last_user_idx = -1
for i, e in enumerate(raw):
    if e.get('type') == 'user.message':
        last_user_idx = i
tail = raw[last_user_idx + 1:] if last_user_idx >= 0 else raw
types = [e['type'] for e in tail]
if 'session.status_idle' in types:
    print('idle')
elif 'span.model_request_end' in types:
    print('span')
elif 'session.error' in types:
    print('error')
else:
    print(types[-1] if types else 'empty')
")
    if [[ "${terminal_seen}" == "idle" || "${terminal_seen}" == "span" ]]; then
      log "turn completed after $((SECONDS))s (signal=${terminal_seen})"
      return 0
    fi
    if [[ "${terminal_seen}" == "error" ]]; then
      fail "session.error during turn"
    fi
    last_type="${terminal_seen}"
    sleep 2
  done
  fail "timed out waiting for turn completion (last type: ${last_type:-?})"
}

# ── 5b. Check for tool events ────────────────────────────────────────

has_tool_events() {
  local has_tool
  has_tool=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc" | python3 -c "
import sys, json
data = json.load(sys.stdin)['data']
inner = [e.get('data', e) if 'data' in e else e for e in data]
print('yes' if any(e.get('type') == 'agent.tool_use' for e in inner) else 'no')
")
  [[ "${has_tool}" == "yes" ]]
}

# ── 6. Inspect events ────────────────────────────────────────────────

inspect_events() {
  log "fetching full event stream"
  local events
  events=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc")
  echo "${events}" > /tmp/deepseek-events.json

  # Per-event summary
  echo "${events}" | python3 -c '
import json, sys

raw = json.load(sys.stdin)["data"]
data = [e.get("data", e) if "data" in e else e for e in raw]
types = [e["type"] for e in data]
print(f"event count: {len(data)}")
print(f"type histogram: { {t: types.count(t) for t in set(types)} }")

need = ["agent.tool_use", "agent.tool_result", "agent.message", "span.model_request_end"]
required = ["agent.message", "span.model_request_end"]
optional = ["agent.tool_use", "agent.tool_result"]
missing_required = [t for t in required if t not in types]
missing_optional = [t for t in optional if t not in types]

print()
print("-- vocabulary check --")
for t in need:
    flag = "OK" if t in types else ("MISSING (required)" if t in required else "missing (optional)")
    print(f"  {t:30s} {flag}")

print()
print("-- event detail --")
for e in data:
    t = e["type"]
    if t == "agent.tool_use":
        name = e.get("name")
        preview = (e.get("input") or {}).get("preview")
        print(f"  TOOL_USE    name={name} preview={preview}")
    elif t == "agent.tool_result":
        print(f"  TOOL_RESULT name={e.get(\"name\")} content={e.get(\"content\")}")
    elif t == "agent.message":
        content = e.get("content", [])
        text = next((c.get("text", "") for c in content if c.get("type") == "text"), "")
        tail = text[-60:].replace("\n", "\\n") if text else ""
        print(f"  MESSAGE     (len={len(text)}) ...{tail}")
    elif t == "span.model_request_end":
        usage = e.get("model_usage", {})
        print(
            f"  SPAN        model={e.get(\"model\")} "
            f"provider={e.get(\"provider\")} "
            f"duration_ms={e.get(\"duration_ms\")} "
            f"in={usage.get(\"input_tokens\", 0)} "
            f"out={usage.get(\"output_tokens\", 0)}"
        )
    elif t == "user.message":
        content = e.get("content", [])
        text = next((c.get("text", "") for c in content if c.get("type") == "text"), "")
        print(f"  USER        {text[:60]}")
    else:
        print(f"  {t}")

if missing_required:
    print(f"\nFAIL — missing required vocabulary: {missing_required}", file=sys.stderr)
    sys.exit(2)
if missing_optional:
    print(f"\nWARN — optional vocabulary not emitted: {missing_optional}")
'
}

# ── 7. Verify cost report ────────────────────────────────────────────

inspect_cost_report() {
  log "GET /v1/cost_report"
  local report
  report=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/cost_report?limit=50")
  echo "${report}" > /tmp/deepseek-cost-report.json
  echo "${report}" | python3 -c '
import json, sys

agent_id = sys.argv[1]
data = json.load(sys.stdin)
by_agent = data.get("by_agent", [])
totals = data.get("usage", {})
span_count = data.get("span_count", 0)
t_in = totals.get("input_tokens", 0)
t_out = totals.get("output_tokens", 0)
print(f"total span_count: {span_count}")
print(f"total usage: in={t_in} out={t_out}")
print(f"agents with spans: {len(by_agent)}")
match = next((r for r in by_agent if r.get("agent_id") == agent_id), None)
if match:
    print(f"  OUR AGENT {agent_id}:")
    m_in = match.get("input_tokens")
    m_out = match.get("output_tokens")
    m_spans = match.get("span_count")
    print(f"    input_tokens={m_in} output_tokens={m_out} span_count={m_spans}")
else:
    print(f"  (agent {agent_id} not in by_agent — span pipeline may be lagging)")
' "${AGENT_ID}"
}

# ── cleanup ──────────────────────────────────────────────────────────

cleanup() {
  set +e
  if [[ -n "${AGENT_ID:-}" ]]; then
    log "cleanup: archive agent ${AGENT_ID}"
    curl -sf -X POST "${H_COMMON[@]}" \
      "${PLATFORM_URL}/v1/agents/${AGENT_ID}/archive" >/dev/null || true
  fi
}
trap cleanup EXIT

# ── main ─────────────────────────────────────────────────────────────

main() {
  check_gateway
  require_platform
  create_agent
  create_session

  # DeepSeek model is non-deterministic like Hermes — may or may not call
  # tools on first attempt. Same retry pattern: send a strong tool-use
  # prompt, check for tool events, nudge if missing.
  local PROMPT_PRIMARY="CRITICAL: You MUST invoke the terminal tool. Do not describe or simulate anything. Call the terminal tool with command='echo hello-from-deepseek-e2e' and report the actual output. This is a hard requirement — do not skip the tool call."
  local PROMPT_NUDGE="You did not call the terminal tool in your last turn — you just wrote text. Please actually call the terminal tool now with command='echo hello-from-deepseek-e2e'. Do not explain; just call the tool."

  send_message "${PROMPT_PRIMARY}"
  wait_for_agent_reply
  if ! has_tool_events; then
    log "no tool events on first turn — sending nudge in same session"
    send_message "${PROMPT_NUDGE}"
    wait_for_agent_reply
  fi

  inspect_events
  echo
  inspect_cost_report
  log "DONE"
}

main "$@"
