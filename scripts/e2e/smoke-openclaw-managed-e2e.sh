#!/usr/bin/env bash
# End-to-end test: OpenClaw agent via the full stack (OpenResponses API).
#
# Verifies that a managed agent with runtime_binding.agent=openclaw:
#   1. Accepts a user message through the API
#   2. Drives the OpenClaw OpenResponses API via OpenClawClient
#   3. Emits the session event vocabulary (agent.message +
#      span.model_request_end; and agent.tool_use / agent.tool_result
#      when function_call items surface)
#   4. Feeds /v1/cost_report via the span
#
# Expects a running oma-server at ${PLATFORM_URL:-http://127.0.0.1:8787}
# with OMA_OPENCLAW_GATEWAY_URL + OMA_OPENCLAW_TOKEN configured, and the
# gateway must support the /v1/responses endpoint.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"

PLATFORM_URL="${PLATFORM_URL:-http://127.0.0.1:8787}"
OMA_API_KEY="${OMA_API_KEY:-dev-key}"
AGENT_NAME="${AGENT_NAME:-openclaw-e2e-$(date +%s)}"
SESSION_TIMEOUT_SEC="${SESSION_TIMEOUT_SEC:-180}"

H_COMMON=(
  -H "x-api-key: ${OMA_API_KEY}"
  -H "x-user-id: openclaw-e2e"
  -H "x-tenant-id: default"
  -H "Content-Type: application/json"
)

log() { echo "[openclaw-e2e] $*"; }
fail() { echo "[openclaw-e2e] FAIL: $*" >&2; exit 1; }

require_platform() {
  curl -sf "${PLATFORM_URL}/health" >/dev/null ||
    fail "oma-server not reachable at ${PLATFORM_URL}"
  log "platform healthy at ${PLATFORM_URL}"
}

# Verify the OpenClaw gateway exposes /v1/responses before burning
# time on agent creation.
require_openclaw_responses_api() {
  local gw_url="${OMA_OPENCLAW_GATEWAY_URL:-}"
  local gw_token="${OMA_OPENCLAW_TOKEN:-}"
  if [[ -z "${gw_url}" ]]; then
    fail "OMA_OPENCLAW_GATEWAY_URL not set — cannot verify OpenResponses API"
  fi
  # Use a tiny HEAD-style probe: POST /v1/responses with stream=false
  # and expect 200 (or any non-404 response). We send empty body so the
  # call fails fast with a validation error rather than running a real
  # inference.
  local status
  status=$(curl -s -o /dev/null -w "%{http_code}" -m 10 \
    -H "Authorization: Bearer ${gw_token}" \
    -H "Content-Type: application/json" \
    -X POST \
    -d '{"model":"openclaw/default","input":"ping","stream":false}' \
    "${gw_url}/v1/responses" || echo "000")
  if [[ "${status}" == "404" || "${status}" == "000" ]]; then
    fail "OpenClaw gateway at ${gw_url} does not expose /v1/responses (status=${status}). Upgrade the gateway or check OMA_OPENCLAW_GATEWAY_URL."
  fi
  log "OpenClaw gateway exposes /v1/responses (status=${status})"
}

cleanup() {
  set +e
  if [[ -n "${AGENT_ID:-}" ]]; then
    log "cleanup: archive agent ${AGENT_ID}"
    curl -sf -X POST "${H_COMMON[@]}" \
      "${PLATFORM_URL}/v1/agents/${AGENT_ID}/archive" >/dev/null || true
  fi
}
trap cleanup EXIT

# ── 1. Create a managed agent pointing at OpenClaw ────────────────────

create_agent() {
  log "create managed agent (harness=managed, agent=openclaw) name=${AGENT_NAME}"
  local resp http_code
  resp=$(curl -s -o /tmp/openclaw-agent-body -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/agents" \
    -d @- <<JSON
{
  "name": "${AGENT_NAME}",
  "model": {"id": "openclaw/default", "speed": "standard"},
  "description": "E2E-managed OpenClaw agent — exercise OpenResponses session events",
  "system": "You are a helpful assistant. Reply concisely.",
  "_oma": {
    "harness": "managed",
    "runtime_binding": {"agent": "openclaw"}
  }
}
JSON
  )
  http_code="${resp}"
  if [[ "${http_code}" != "201" ]]; then
    fail "create agent status=${http_code}: $(cat /tmp/openclaw-agent-body)"
  fi
  AGENT_ID=$(python3 "${SMOKE_UTILS}" json-field "id" </tmp/openclaw-agent-body)
  log "agent created — id=${AGENT_ID}"
}

# ── 2. Pick an environment + create a session ─────────────────────────

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
  resp=$(curl -s -o /tmp/openclaw-session-body -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions" \
    -d "{\"agent\":\"${AGENT_ID}\",\"environment_id\":\"${env_id}\"}")
  http_code="${resp}"
  if [[ "${http_code}" != "201" ]]; then
    fail "create session status=${http_code}: $(cat /tmp/openclaw-session-body)"
  fi
  SESSION_ID=$(python3 "${SMOKE_UTILS}" json-field "id" </tmp/openclaw-session-body)
  log "session created — id=${SESSION_ID}"
}

# ── 3. Send a user message ─────────────────────────────────────────────

send_message() {
  local msg="$1"
  log "POST /v1/sessions/${SESSION_ID}/events — ${msg}"
  local resp http_code
  resp=$(curl -s -o /tmp/openclaw-post-events -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events" \
    -d "{\"events\":[{\"type\":\"user.message\",\"content\":[{\"type\":\"text\",\"text\":\"${msg}\"}]}]}")
  http_code="${resp}"
  if [[ "${http_code}" != "202" && "${http_code}" != "200" && "${http_code}" != "201" ]]; then
    fail "post events status=${http_code}: $(cat /tmp/openclaw-post-events)"
  fi
  log "message accepted"
}

# ── 4. Wait for turn to complete ───────────────────────────────────────

wait_for_agent_reply() {
  local deadline=$((SECONDS + SESSION_TIMEOUT_SEC))
  local events terminal_seen last_type
  while [[ ${SECONDS} -lt ${deadline} ]]; do
    events=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc")
    terminal_seen=$(echo "${events}" | python3 -c "
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
else:
    print(types[-1] if types else 'empty')
")
    if [[ "${terminal_seen}" == "idle" || "${terminal_seen}" == "span" ]]; then
      log "turn completed after $((SECONDS))s (signal=${terminal_seen})"
      return 0
    fi
    last_type="${terminal_seen}"
    sleep 2
  done
  fail "timed out waiting for turn completion (last type: ${last_type:-?})"
}

# ── 5. Inspect the event stream ────────────────────────────────────────

inspect_events() {
  log "fetching full event stream"
  local events
  events=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc")
  echo "${events}" > /tmp/openclaw-events.json
  echo "${events}" | python3 -c '
import json, sys
data = json.load(sys.stdin)["data"]
print(f"-- event stream ({len(data)} events) --")
types = {}
for e in data:
    t = e.get("type")
    types[t] = types.get(t, 0) + 1
print("type histogram:")
for t, c in sorted(types.items()):
    print(f"  {t:40s} {c}")

# Detail for interesting events
print("\n-- event detail --")
for e in data:
    inner = e.get("data", e)
    t = e.get("type")
    if t == "user.message":
        content = inner.get("content", [])
        text = content[0].get("text", "") if content else ""
        print(f"  USER        text={text[:80]!r}")
    elif t == "agent.message":
        content = inner.get("content", [])
        text = content[0].get("text", "") if content else ""
        print(f"  AGENT_MSG   text={text[:80]!r}")
    elif t == "agent.tool_use":
        name = inner.get("name", "")
        preview = inner.get("input", {}).get("preview", "")[:60]
        print(f"  TOOL_USE    name={name} preview={preview}")
    elif t == "agent.tool_result":
        name = inner.get("name", "")
        content = inner.get("content", "")[:60]
        print(f"  TOOL_RESULT name={name} content={content}")
    elif t == "span.model_request_end":
        model = inner.get("model", "")
        provider = inner.get("provider", "")
        dur = inner.get("duration_ms", 0)
        u = inner.get("model_usage", {})
        in_tok = u.get("input_tokens", 0)
        out_tok = u.get("output_tokens", 0)
        print(f"  SPAN        model={model} provider={provider} duration_ms={dur} in={in_tok} out={out_tok}")

# Vocabulary check — the events the UI cares about
print("\n-- vocabulary check --")
type_set = set(types.keys())
required = ["agent.message", "span.model_request_end"]
optional = ["agent.tool_use", "agent.tool_result"]
for r in required:
    status = "OK" if r in type_set else "MISSING"
    print(f"  {r:40s} {status}")
for o in optional:
    status = "OK" if o in type_set else "(not present — server-side tool execution)"
    print(f"  {o:40s} {status}")
'
}

# ── 6. Verify /v1/cost_report saw the span ─────────────────────────────

inspect_cost_report() {
  log "GET /v1/cost_report"
  local report
  report=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/cost_report?limit=50")
  echo "${report}" > /tmp/openclaw-cost-report.json
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
    m_in = match.get("input_tokens")
    m_out = match.get("output_tokens")
    m_spans = match.get("span_count")
    print(f"  OUR AGENT {agent_id}:")
    print(f"    input_tokens={m_in} output_tokens={m_out} span_count={m_spans}")
    if (m_in or 0) > 0 or (m_out or 0) > 0:
        print("    -> usage captured (non-zero)")
    else:
        print("    -> WARNING: usage all zeros (span pipeline may not have decoded usage)")
else:
    print(f"  (agent {agent_id} not in by_agent — span pipeline may be lagging)")
' "${AGENT_ID}"
}

# ── 7. Verify span has non-zero usage ──────────────────────────────────

verify_usage_captured() {
  log "verifying span usage is non-zero"
  local events
  events=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc")
  local result
  result=$(echo "${events}" | python3 -c '
import sys, json
data = json.load(sys.stdin)["data"]
for e in data:
    if e.get("type") == "span.model_request_end":
        u = e.get("data", e).get("model_usage", {})
        in_tok = u.get("input_tokens", 0)
        out_tok = u.get("output_tokens", 0)
        print(f"{in_tok} {out_tok}")
        sys.exit(0)
print("no_span")
')
  if [[ "${result}" == "no_span" ]]; then
    fail "no span.model_request_end found in events"
  fi
  local in_tok out_tok
  in_tok=$(echo "${result}" | awk '{print $1}')
  out_tok=$(echo "${result}" | awk '{print $2}')
  log "span usage: input_tokens=${in_tok} output_tokens=${out_tok}"
  if [[ "${in_tok}" == "0" && "${out_tok}" == "0" ]]; then
    fail "span usage all zeros — OpenResponses usage not captured"
  fi
  log "usage capture OK"
}

# ── main ───────────────────────────────────────────────────────────────

main() {
  require_platform
  require_openclaw_responses_api
  create_agent
  create_session

  # Simple prompt — OpenClaw's server-side tools handle execution and
  # fold results into the response message. We verify the full pipeline:
  # user.message → agent.message + span.model_request_end (with real
  # usage) → /v1/cost_report.
  local PROMPT="Say 'hello from openclaw e2e' in exactly those words, then tell me what 2+2 equals."

  send_message "${PROMPT}"
  wait_for_agent_reply

  inspect_events
  echo
  verify_usage_captured
  echo
  inspect_cost_report
  log "DONE"
}

main "$@"
