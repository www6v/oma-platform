#!/usr/bin/env bash
# Live end-to-end: real harness (OMA_FAKE_HARNESS=0) + real LLM calls call_agent_*.
#
# Prerequisites:
#   - oma-server on OMA_LISTEN_ADDR (default :8787) with OMA_FAKE_HARNESS=0
#   - harness sidecar on HARNESS_URL (default :8090)
#   - LLM credentials configured for piPy (see README / ~/.pi/agent/auth.json)
#
# Usage:
#   ./scripts/smoke-subagent-live-e2e.sh
#   SMOKE_MODEL=claude-sonnet-4-6 ./scripts/smoke-subagent-live-e2e.sh
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
export HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}"

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

wait_for_call_agent_chain() {
  local sid="$1"
  local worker_id="$2"
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
      WORKER_ID="${worker_id}" python3 -c 'import json,os,sys
events=json.load(sys.stdin)["data"]
worker_id=os.environ["WORKER_ID"]
tool_prefix=f"call_agent_{worker_id.replace(chr(45), chr(95))}"
saw_tool=False
saw_thread=False
saw_worker_msg=False
saw_primary=False
for evt in events:
    if evt.get("type") == "session.error":
        print(evt.get("message") or evt.get("error") or "session.error")
        sys.exit(3)
    name=evt.get("name") or ""
    if evt.get("type") == "agent.tool_use" and name.startswith("call_agent_"):
        saw_tool=True
    if evt.get("type") == "session.thread_created":
        saw_thread=True
    if evt.get("type") == "agent.message" and evt.get("session_thread_id"):
        for block in evt.get("content") or []:
            text=(block.get("text") or "")
            if "WORKER-LIVE-OK" in text:
                saw_worker_msg=True
    if evt.get("type") == "agent.message" and not evt.get("session_thread_id"):
        for block in evt.get("content") or []:
            text=(block.get("text") or "")
            if text.strip():
                saw_primary=True
if saw_tool and saw_thread and saw_worker_msg and saw_primary:
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
      echo "   ... waiting for call_agent_* chain ($((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out waiting for call_agent delegation chain" >&2
  echo "${events}" >&2
  return 1
}

log() {
  echo "[subagent-live] $*"
}

if [[ "${OMA_FAKE_HARNESS}" == "1" ]]; then
  echo "error: OMA_FAKE_HARNESS=1 skips real harness — set OMA_FAKE_HARNESS=0" >&2
  exit 1
fi

log "preflight: platform + harness"
api_get "/health" >/dev/null
curl -sf "${HARNESS_URL}/health" >/dev/null
log "platform + harness ok"

log "create worker agent"
WORKER_ID="$(
  api_post_json "/v1/agents" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "name": "subagent-live-worker",
        "model": sys.argv[1],
        "system_prompt": (
            "You are a worker sub-agent. For every task, respond with exactly "
            "WORKER-LIVE-OK and nothing else."
        ),
        "tools": [{
            "type": "agent_toolset_20260401",
            "default_config": {"enabled": False},
            "configs": [],
        }],
    }))' "${SMOKE_MODEL}")" \
    | json_field id
)"
log "WORKER_ID=${WORKER_ID}"

log "create coordinator agent"
COORD_ID="$(
  api_post_json "/v1/agents" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "name": "subagent-live-coordinator",
        "model": sys.argv[1],
        "system_prompt": (
            "You are a coordinator smoke test agent. You MUST delegate every user "
            "request to the worker using the call_agent tool (call_agent_*). "
            "Never answer the user directly without delegating first. After you "
            "receive the worker tool result, reply with a one-line summary that "
            "includes the worker text."
        ),
        "tools": [{"type": "agent_toolset_20260401"}],
        "callable_agents": [
            {"type": "agent", "id": sys.argv[2], "version": 1},
        ],
    }))' "${SMOKE_MODEL}" "${WORKER_ID}")" \
    | json_field id
)"
log "COORD_ID=${COORD_ID}"

log "create session"
SID="$(
  api_post_json "/v1/sessions" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "agent": sys.argv[1],
        "environment_id": "env-local-default",
        "title": "subagent-live-smoke",
    }))' "${COORD_ID}")" \
    | json_field id
)"
log "SID=${SID}"

log "send turn (must trigger call_agent_*)"
api_post_json "/v1/sessions/${SID}/events" \
  '{"events":[{"type":"user.message","content":[{"type":"text","text":"Delegate to the worker with message: perform smoke task. After delegation, summarize the worker result in one line."}]}]}' \
  >/dev/null

log "wait for call_agent tool_use + sub thread + worker reply (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
EVENTS="$(wait_for_call_agent_chain "${SID}" "${WORKER_ID}")"

python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
tool_name=""
thread_id=""
for evt in events:
    if evt.get("type") == "agent.tool_use" and str(evt.get("name","")).startswith("call_agent_"):
        tool_name=evt.get("name")
    if evt.get("type") == "session.thread_created":
        thread_id=evt.get("session_thread_id")
if not tool_name:
    raise SystemExit("missing call_agent tool_use")
if not thread_id:
    raise SystemExit("missing session.thread_created")
print(f"SUBAGENT_LIVE_OK tool_use={tool_name!r} thread={thread_id!r}")' <<<"${EVENTS}"

log "verify GET /threads"
THREADS="$(
  api_get "/v1/sessions/${SID}/threads"
)"
python3 -c 'import json,sys
body=json.load(sys.stdin)
data=body.get("data") or []
if len(data) < 2:
    raise SystemExit(f"expected primary+sub threads, got {len(data)}")
if data[0].get("id") != "sthr_primary":
    raise SystemExit("primary id=%r" % data[0].get("id"))
print("threads ok count=%d" % len(data))' <<<"${THREADS}"

log "PASS: live sub-agent harness smoke completed"
