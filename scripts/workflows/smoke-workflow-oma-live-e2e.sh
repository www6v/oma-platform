#!/usr/bin/env bash
# Live workflow → OMA session sub-thread smoke (requires running stack).
#
# Prerequisites:
#   - oma-server on PLATFORM_URL (default http://127.0.0.1:8787)
#   - harness on HARNESS_URL (default http://127.0.0.1:8090)
#   - OMA_INTERNAL_SECRET configured on both sides
#   - LLM credentials for piPy (model card or ~/.pi/agent/auth.json)
#
# Usage:
#   ./scripts/workflows/smoke-workflow-oma-live-e2e.sh
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
source "${ROOT_DIR}/scripts/e2e/common.sh"

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-120}"

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")

log() {
  echo "[workflow-oma-live] $*"
}

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

normalize_events() {
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

log "preflight"
api_get "/health" >/dev/null
curl -sf "${HARNESS_URL}/health" >/dev/null

WORKFLOW_BASE="${WORKFLOW_API_BASE:-${HARNESS_URL}}"
if curl -sf "${PLATFORM_URL}/api/workflows/health" >/dev/null 2>&1; then
  WORKFLOW_BASE="${PLATFORM_URL}"
fi

INTERNAL_CODE="$(
  curl -s -o /dev/null -w "%{http_code}" \
    -H "x-internal-secret: ${OMA_INTERNAL_SECRET:-dev-internal-secret}" \
    "${PLATFORM_URL}/v1/internal/model_cards/resolve?model_id=__workflow_smoke__"
)"
if [[ "${INTERNAL_CODE}" == "404" ]]; then
  echo "error: ${PLATFORM_URL} missing /v1/internal/* — use ./start-console.sh (go run), not stale oma-server binary" >&2
  exit 1
fi

log "workflow API base=${WORKFLOW_BASE}"

YAML='name: smoke-oma-live
description: workflow oma sub-thread smoke
steps:
  - name: step1
    action: llm_execute
    params:
      prompt: Reply with exactly WORKFLOW-SMOKE-OK and nothing else.'

log "create workflow"
WF_ID="$(
  curl -sf -X POST "${WORKFLOW_BASE}/api/workflows" \
    -H "content-type: application/json" \
    -H "x-api-key: ${OMA_API_KEY}" \
    -H "X-Active-Tenant: default" \
    -d "$(python3 -c 'import json,sys; print(json.dumps({
        "name": "smoke-oma-live",
        "description": "live",
        "yaml": sys.argv[1],
    }))' "${YAML}")" \
    | json_field id
)"

log "execute workflow"
EXEC_ID="$(
  curl -sf -X POST "${WORKFLOW_BASE}/api/workflows/${WF_ID}/execute" \
    -H "content-type: application/json" \
    -H "x-api-key: ${OMA_API_KEY}" \
    -H "X-Active-Tenant: default" \
    -d "{}" \
    | json_field execution_id
)"

SID=""
deadline=$((SECONDS + SMOKE_TOOL_TIMEOUT_SEC))
while (( SECONDS < deadline )); do
  ST="$(
    curl -sf "${WORKFLOW_BASE}/api/workflows/executions/${EXEC_ID}" \
      -H "x-api-key: ${OMA_API_KEY}" \
      -H "X-Active-Tenant: default"
  )"
  STATUS="$(python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["status"])' <<<"${ST}")"
  SID="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("oma_session_id") or "")' <<<"${ST}")"
  if [[ "${STATUS}" == "completed" || "${STATUS}" == "failed" ]]; then
    break
  fi
  sleep "${SMOKE_POLL_SEC}"
done

if [[ -z "${SID}" ]]; then
  echo "error: no oma_session_id on execution ${EXEC_ID}" >&2
  exit 1
fi

log "session=${SID}"

THREADS="$(api_get "/v1/sessions/${SID}/threads")"
python3 -c 'import json,sys
body=json.load(sys.stdin)
data=body.get("data") or []
if len(data) < 2:
    raise SystemExit(f"expected primary+sub threads, got {len(data)}")
print("threads ok count=%d" % len(data))' <<<"${THREADS}"

EVENTS="$(
  api_get "/v1/sessions/${SID}/events?order=asc" | normalize_events
)"

python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
types=[e.get("type") for e in events]
if "session.thread_created" not in types:
    raise SystemExit("missing session.thread_created")
sub_msgs=[
    e for e in events
    if e.get("type") == "agent.message"
    and e.get("session_thread_id")
    and e.get("session_thread_id") != "sthr_primary"
    and any(
        (b.get("text") or "").find("WORKFLOW-SMOKE-OK") >= 0
        for b in (e.get("content") or [])
        if b.get("type") == "text"
    )
]
if not sub_msgs:
    raise SystemExit(f"missing sub-thread agent.message; types={types}")
user_msgs=[
    e for e in events
    if e.get("type") == "user.message"
    and e.get("session_thread_id")
    and e.get("session_thread_id") != "sthr_primary"
]
if not user_msgs:
    raise SystemExit("missing sub-thread user.message")
print("WORKFLOW_OMA_LIVE_OK sub_thread=%s" % sub_msgs[0].get("session_thread_id"))' \
  <<<"${EVENTS}"

log "Console: ${PLATFORM_URL}/sessions/${SID}"
log "PASS"
