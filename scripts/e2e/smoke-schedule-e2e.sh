#!/usr/bin/env bash
# E2E: session wakeup schedules (T18) — store, harness client, internal API, worker fire.
#
# Phase 1 (always): Go wakeup repo test + Python harness schedule tests.
# Phase 2 (when OMA_INTERNAL_SECRET set and oma-server is up):
#   internal schedule → span.wakeup_scheduled → list → cancel → cron list/cancel
#   → short delay fire → worker injects user.message (harness=schedule) → one-shot row removed.
#
# Prerequisites for live phase:
#   - oma-server on OMA_LISTEN_ADDR (default :8787)
#   - OMA_INTERNAL_SECRET in .env (same value server uses)
#   - Wakeup worker enabled (do not set OMA_WAKEUP_WORKER_DISABLED=1)
#
# Usage:
#   ./scripts/e2e/smoke-schedule-e2e.sh
#   ./scripts/e2e/smoke-schedule-e2e.sh http://127.0.0.1:8787
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"

LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
if [[ "${LISTEN_ADDR}" == :* ]]; then
  DEFAULT_BASE="http://127.0.0.1${LISTEN_ADDR}"
else
  DEFAULT_BASE="http://${LISTEN_ADDR}"
fi

BASE="${1:-${OMA_BASE_URL:-${OMA_API_URL:-$DEFAULT_BASE}}}"
INTERNAL_SECRET="${OMA_INTERNAL_SECRET:-}"
FIRE_TIMEOUT_SEC="${SMOKE_SCHEDULE_FIRE_TIMEOUT_SEC:-30}"
POLL_SEC="${SMOKE_POLL_SEC:-2}"

log() {
  echo "[schedule-e2e] $*"
}

echo "=== Schedule wakeup E2E (T18) ==="
echo "Base: ${BASE}"

echo ""
echo "== Phase 1: unit tests =="

if command -v go >/dev/null 2>&1; then
  GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
    go test ./internal/store/... -run TestWakeupRepoCreateListDelete -count=1 -v
else
  log "skip: go not installed — wakeup store test not run"
fi

(
  cd "${ROOT_DIR}/harness"
  python3 -m pytest tests/test_schedule.py tests/test_tools.py -q
)

live_ok=0
if [[ -n "${INTERNAL_SECRET}" ]] && curl -sf "${BASE}/health" >/dev/null 2>&1; then
  live_ok=1
fi

if [[ "${live_ok}" -ne 1 ]]; then
  if [[ -z "${INTERNAL_SECRET}" ]]; then
    log "skip live phase: OMA_INTERNAL_SECRET not set"
  else
    log "skip live phase: ${BASE}/health not reachable"
  fi
  echo ""
  echo "Schedule E2E passed (unit tests only)"
  exit 0
fi

if [[ "${OMA_WAKEUP_WORKER_DISABLED:-}" == "1" ]]; then
  echo "error: OMA_WAKEUP_WORKER_DISABLED=1 — worker fire test cannot run" >&2
  exit 1
fi

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")
INTERNAL_HEADERS=(-H "x-internal-secret: ${INTERNAL_SECRET}")

json_field() {
  local field="$1"
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$field"
}

api_get() {
  curl -sf "${BASE}$1" "${API_HEADERS[@]}"
}

api_post_json() {
  local path="$1"
  local body="$2"
  curl -sf -X POST "${BASE}${path}" \
    -H "content-type: application/json" \
    "${API_HEADERS[@]}" \
    -d "${body}"
}

internal_get() {
  curl -sf "${BASE}$1" "${INTERNAL_HEADERS[@]}"
}

internal_post_json() {
  local path="$1"
  local body="$2"
  local expect="${3:-201}"
  local resp code
  resp="$(
    curl -sS -X POST "${BASE}${path}" \
      -H "content-type: application/json" \
      "${INTERNAL_HEADERS[@]}" \
      -d "${body}" \
      -w $'\n%{http_code}'
  )"
  code="${resp##*$'\n'}"
  resp="${resp%$'\n'*}"
  if [[ "${code}" != "${expect}" ]]; then
    echo "error: POST ${path} → HTTP ${code}: ${resp}" >&2
    exit 1
  fi
  echo "${resp}"
}

internal_delete() {
  curl -sf -X DELETE "${BASE}$1" "${INTERNAL_HEADERS[@]}"
}

normalize_events_response() {
  python3 -c 'import json,sys
raw=json.load(sys.stdin)
out=[]
for item in raw.get("data", []):
    typ=item.get("type")
    inner=item.get("data")
    if isinstance(inner, dict):
        evt=dict(inner)
        if typ and "type" not in evt:
            evt["type"]=typ
        out.append(evt)
    elif isinstance(inner, str):
        evt=json.loads(inner)
        if typ and "type" not in evt:
            evt["type"]=typ
        out.append(evt)
    else:
        out.append(item)
print(json.dumps({"data": out}))'
}

wait_for_span_scheduled() {
  local sid="$1"
  local schedule_id="$2"
  local deadline=$((SECONDS + 15))
  while (( SECONDS < deadline )); do
    local events
    events="$(api_get "/v1/sessions/${sid}/events?limit=200" | normalize_events_response)"
    if python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
want=sys.argv[1]
for evt in events:
    if evt.get("type") != "span.wakeup_scheduled":
        continue
    if evt.get("schedule_id") == want:
        sys.exit(0)
sys.exit(2)' "${schedule_id}" <<<"${events}"; then
      return 0
    fi
    sleep "${POLL_SEC}"
  done
  echo "error: span.wakeup_scheduled not found for ${schedule_id}" >&2
  return 1
}

wait_for_wakeup_message() {
  local sid="$1"
  local prompt="$2"
  local deadline=$((SECONDS + FIRE_TIMEOUT_SEC))
  while (( SECONDS < deadline )); do
    local events
    events="$(api_get "/v1/sessions/${sid}/events?limit=200" | normalize_events_response)"
    if python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
want=sys.argv[1]
for evt in events:
    if evt.get("type") != "user.message":
        continue
    meta=evt.get("metadata") or {}
    if meta.get("harness") != "schedule" or meta.get("kind") != "wakeup":
        continue
    for block in evt.get("content") or []:
        if (block.get("text") or "") == want:
            sys.exit(0)
sys.exit(2)' "${prompt}" <<<"${events}"; then
      echo "${events}"
      return 0
    fi
  sleep "${POLL_SEC}"
  done
  echo "error: wakeup user.message not found within ${FIRE_TIMEOUT_SEC}s" >&2
  return 1
}

echo ""
echo "== Phase 2: live internal API + worker =="

log "preflight internal routes"
internal_code="$(
  curl -sS -o /dev/null -w '%{http_code}' \
    -H "x-internal-secret: ${INTERNAL_SECRET}" \
    "${BASE}/v1/internal/sessions" \
    -H "Content-Type: application/json" \
    -d '{"action":"create"}' || true
)"
if [[ "${internal_code}" == "404" ]]; then
  echo "error: /v1/internal/* not mounted — restart oma-server with OMA_INTERNAL_SECRET" >&2
  exit 1
fi

log "create agent + session"
AGENT_ID="$(
  api_post_json "/v1/agents" \
    '{"name":"schedule-e2e-agent","model":"claude-sonnet-4-6","system":"schedule e2e","tools":[{"type":"agent_toolset_20260401"}]}' \
    | json_field id
)"
SID="$(
  api_post_json "/v1/sessions" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "agent": sys.argv[1],
        "environment_id": "env-local-default",
        "title": "schedule-e2e",
    }))' "${AGENT_ID}")" \
    | json_field id
)"
log "AGENT_ID=${AGENT_ID} SID=${SID}"

wakeup_probe_code="$(
  curl -sS -o /dev/null -w '%{http_code}' \
    -X POST "${BASE}/v1/internal/sessions/${SID}/wakeups" \
    -H "content-type: application/json" \
    "${INTERNAL_HEADERS[@]}" \
    -d '{}' || true
)"
if [[ "${wakeup_probe_code}" == "404" ]]; then
  echo "error: POST /v1/internal/sessions/*/wakeups returned 404 (stale oma-server)" >&2
  echo "  rebuild and restart: source scripts/go-env.sh && go run ./cmd/oma-server/" >&2
  exit 1
fi
if [[ "${wakeup_probe_code}" != "400" ]]; then
  echo "error: wakeups probe expected 400, got ${wakeup_probe_code}" >&2
  exit 1
fi

log "schedule one_shot (long delay) via internal API"
SCHEDULE_JSON="$(
  internal_post_json "/v1/internal/sessions/${SID}/wakeups" \
    '{"delay_seconds":300,"prompt":"e2e schedule list probe"}'
)"
SCHEDULE_ID="$(echo "${SCHEDULE_JSON}" | json_field id)"
SCHEDULE_KIND="$(echo "${SCHEDULE_JSON}" | json_field kind)"
if [[ "${SCHEDULE_KIND}" != "one_shot" ]]; then
  echo "error: expected kind one_shot, got ${SCHEDULE_KIND}" >&2
  exit 1
fi
log "SCHEDULE_ID=${SCHEDULE_ID}"

wait_for_span_scheduled "${SID}" "${SCHEDULE_ID}"

LIST_JSON="$(internal_get "/v1/internal/sessions/${SID}/wakeups")"
python3 -c 'import json,sys
body=json.load(sys.stdin)
schedules=body.get("schedules") or []
want=sys.argv[1]
found=any(s.get("id")==want for s in schedules)
if not found:
    print("error: schedule missing from list", schedules)
    sys.exit(1)' "${SCHEDULE_ID}" <<<"${LIST_JSON}"

log "cancel scheduled wakeup"
CANCEL_JSON="$(internal_delete "/v1/internal/sessions/${SID}/wakeups/${SCHEDULE_ID}")"
python3 -c 'import json,sys
body=json.load(sys.stdin)
if body.get("cancelled") is not True:
    print("error: cancel expected true", body)
    sys.exit(1)' <<<"${CANCEL_JSON}"

LIST_AFTER_CANCEL="$(internal_get "/v1/internal/sessions/${SID}/wakeups")"
python3 -c 'import json,sys
schedules=json.load(sys.stdin).get("schedules") or []
want=sys.argv[1]
if any(s.get("id")==want for s in schedules):
    print("error: schedule still listed after cancel", schedules)
    sys.exit(1)' "${SCHEDULE_ID}" <<<"${LIST_AFTER_CANCEL}"

log "schedule cron + list + cancel"
CRON_JSON="$(
  internal_post_json "/v1/internal/sessions/${SID}/wakeups" \
    '{"cron":"0 9 * * *","prompt":"morning standup e2e"}'
)"
CRON_ID="$(echo "${CRON_JSON}" | json_field id)"
if [[ "$(echo "${CRON_JSON}" | json_field kind)" != "cron" ]]; then
  echo "error: cron schedule kind mismatch" >&2
  exit 1
fi
CRON_LIST="$(internal_get "/v1/internal/sessions/${SID}/wakeups")"
python3 -c 'import json,sys
schedules=json.load(sys.stdin).get("schedules") or []
want=sys.argv[1]
row=next((s for s in schedules if s.get("id")==want), None)
if not row or row.get("cron") != "0 9 * * *":
    print("error: cron row missing", schedules)
    sys.exit(1)' "${CRON_ID}" <<<"${CRON_LIST}"
internal_delete "/v1/internal/sessions/${SID}/wakeups/${CRON_ID}" >/dev/null

WAKE_PROMPT="SCHEDULE_E2E_WAKEUP_PAYLOAD"
log "schedule short delay for worker fire (delay_seconds=6)"
FIRE_JSON="$(
  internal_post_json "/v1/internal/sessions/${SID}/wakeups" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "delay_seconds": 6,
        "prompt": sys.argv[1],
    }))' "${WAKE_PROMPT}")"
)"
FIRE_ID="$(echo "${FIRE_JSON}" | json_field id)"

wait_for_wakeup_message "${SID}" "${WAKE_PROMPT}"

LIST_AFTER_FIRE="$(internal_get "/v1/internal/sessions/${SID}/wakeups")"
python3 -c 'import json,sys
schedules=json.load(sys.stdin).get("schedules") or []
want=sys.argv[1]
if any(s.get("id")==want for s in schedules):
    print("error: one_shot schedule still present after fire", schedules)
    sys.exit(1)' "${FIRE_ID}" <<<"${LIST_AFTER_FIRE}"

echo ""
echo "Schedule E2E passed (unit + live)"
