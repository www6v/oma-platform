#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_MODEL_CARD_ID="${SMOKE_MODEL_CARD_ID:-smoke-claude}"
export SMOKE_TIMEOUT_SEC="${SMOKE_TIMEOUT_SEC:-120}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export SMOKE_SKIP_LLM="${SMOKE_SKIP_LLM:-0}"

DEFAULT_ENV_ID="env-local-default"

if [[ "${SMOKE_SKIP_LLM}" != "1" && "${OMA_FAKE_HARNESS}" == "1" ]]; then
  echo "error: OMA_FAKE_HARNESS=1 uses fake responses, not a real LLM" >&2
  echo "set OMA_FAKE_HARNESS=0 in .env and restart start-platform.sh + start-harness.sh" >&2
  echo "or set SMOKE_SKIP_LLM=1 to exercise P1 APIs only" >&2
  exit 1
fi

LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
if [[ "${LISTEN_ADDR}" == :* ]]; then
  PLATFORM_URL="http://127.0.0.1${LISTEN_ADDR}"
else
  PLATFORM_URL="http://${LISTEN_ADDR}"
fi

API_HEADERS=(
  -H "x-api-key: ${OMA_API_KEY}"
)

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

wait_for_agent_reply() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TIMEOUT_SEC))
  local events=""
  local status=0

  while (( SECONDS < deadline )); do
    events="$(
      api_get "/v1/sessions/${sid}/events?order=asc"
    )"
    status=0
    python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
for evt in events:
    if evt.get("type") != "agent.message":
        continue
    if evt.get("id") == "evt_fake":
        sys.exit(1)
    content=evt.get("content") or []
    for block in content:
        if block.get("type") == "text" and block.get("text", "").strip():
            sys.exit(0)
sys.exit(2)' <<<"${events}" || status=$?

    if [[ "${status}" -eq 0 ]]; then
      echo "${events}"
      return 0
    fi
    if [[ "${status}" -eq 1 ]]; then
      echo "error: got evt_fake — platform is still in fake mode" >&2
      echo "restart start-platform.sh with OMA_FAKE_HARNESS=0" >&2
      echo "${events}" >&2
      return 1
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out after ${SMOKE_TIMEOUT_SEC}s waiting for real agent.message" >&2
  echo "${events}" >&2
  return 1
}

SMOKE_ENV_ID="${DEFAULT_ENV_ID}"
SMOKE_ENV_CREATED=0
AGENT_MODEL="${SMOKE_MODEL}"
MODEL_CARD_ROW_ID=""

cleanup() {
  if [[ "${SMOKE_ENV_CREATED}" == "1" && -n "${SMOKE_ENV_ID}" ]]; then
    echo "==> archive smoke environment ${SMOKE_ENV_ID}"
    api_post_json "/v1/environments/${SMOKE_ENV_ID}/archive" "{}" >/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> health ${PLATFORM_URL}/health"
api_get "/health" >/dev/null
echo "ok"

echo "==> list environments (expect ${DEFAULT_ENV_ID})"
ENV_LIST="$(api_get "/v1/environments")"
python3 -c 'import json,sys
data=json.load(sys.stdin)["data"]
ids={row["id"] for row in data}
if sys.argv[1] not in ids:
    raise SystemExit(f"missing default env {sys.argv[1]!r}, got {sorted(ids)}")
print(f"environments={len(data)} default_ok")' "${DEFAULT_ENV_ID}" <<<"${ENV_LIST}"

echo "==> get default environment"
DEFAULT_ENV="$(api_get "/v1/environments/${DEFAULT_ENV_ID}")"
python3 -c 'import json,sys
env=json.load(sys.stdin)
assert env["id"]==sys.argv[1], env
cfg=env.get("config") or {}
typ=cfg.get("type") if isinstance(cfg, dict) else None
print("name=%s type=%s" % (env.get("name"), typ))' \
  "${DEFAULT_ENV_ID}" <<<"${DEFAULT_ENV}"

echo "==> create smoke environment"
SMOKE_ENV_ID="$(
  api_post_json "/v1/environments" \
    '{"name":"smoke-test","description":"smoke-test.sh","config":{"type":"local"}}' \
    | json_field id
)"
SMOKE_ENV_CREATED=1
echo "SMOKE_ENV_ID=${SMOKE_ENV_ID}"

echo "==> list model cards"
CARD_LIST="$(api_get "/v1/model_cards")"
CARD_COUNT="$(
  python3 -c 'import json,sys; print(len(json.load(sys.stdin)["data"]))' <<<"${CARD_LIST}"
)"
echo "model_cards=${CARD_COUNT}"

if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "==> ensure model card (model_id=${SMOKE_MODEL_CARD_ID}, model=${SMOKE_MODEL})"
  CARD_BODY="$(
    python3 -c 'import json,os,sys
print(json.dumps({
    "model_id": sys.argv[1],
    "model": sys.argv[2],
    "provider": "ant",
    "api_key": os.environ["ANTHROPIC_API_KEY"],
    "is_default": True,
}))' "${SMOKE_MODEL_CARD_ID}" "${SMOKE_MODEL}"
  )"
  CARD_HTTP="$(
    curl -s -o /tmp/oma-smoke-card.json -w "%{http_code}" -X POST \
      "${PLATFORM_URL}/v1/model_cards" \
      -H "content-type: application/json" \
      "${API_HEADERS[@]}" \
      -d "${CARD_BODY}"
  )"
  if [[ "${CARD_HTTP}" == "201" ]]; then
    MODEL_CARD_ROW_ID="$(json_field id </tmp/oma-smoke-card.json)"
    echo "MODEL_CARD_ROW_ID=${MODEL_CARD_ROW_ID} (created)"
  elif [[ "${CARD_HTTP}" == "409" ]]; then
    CARD_LIST="$(api_get "/v1/model_cards")"
    MODEL_CARD_ROW_ID="$(
      python3 -c 'import json,sys
target=sys.argv[1]
for row in json.load(sys.stdin)["data"]:
    if row.get("model_id")==target:
        print(row["id"])
        raise SystemExit(0)
raise SystemExit(f"model_id {target!r} conflict but not in list")' \
        "${SMOKE_MODEL_CARD_ID}" <<<"${CARD_LIST}"
    )"
    echo "MODEL_CARD_ROW_ID=${MODEL_CARD_ROW_ID} (existing)"
  else
    echo "error: model card POST status=${CARD_HTTP}" >&2
    cat /tmp/oma-smoke-card.json >&2 || true
    exit 1
  fi

  echo "==> get model card key (redacted)"
  KEY_PREVIEW="$(
    api_get "/v1/model_cards/${MODEL_CARD_ROW_ID}/key" \
      | python3 -c 'import json,sys
k=json.load(sys.stdin).get("api_key","")
print("set" if k else "empty", f"len={len(k)}")'
  )"
  echo "api_key ${KEY_PREVIEW}"

  AGENT_MODEL="${SMOKE_MODEL_CARD_ID}"
else
  echo "==> skip model card (ANTHROPIC_API_KEY unset; agent.model=${SMOKE_MODEL})"
fi

echo "==> create agent (model=${AGENT_MODEL})"
AID="$(
  api_post_json "/v1/agents" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "name": "hello",
        "model": sys.argv[1],
        "system_prompt": "You are helpful.",
        "description": "smoke test agent",
        "tools": "agent_toolset_20260401",
    }))' "${AGENT_MODEL}")" \
    | json_field id
)"
echo "AID=${AID}"

echo "==> list agent versions (historical only)"
VERSIONS="$(api_get "/v1/agents/${AID}/versions")"
VERSION_COUNT="$(
  python3 -c 'import json,sys; print(len(json.load(sys.stdin).get("data",[])))' \
    <<<"${VERSIONS}"
)"
echo "agent_versions=${VERSION_COUNT}"

echo "==> create session (environment_id=${SMOKE_ENV_ID})"
SESSION_JSON="$(
  api_post_json "/v1/sessions" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "agent": sys.argv[1],
        "environment_id": sys.argv[2],
        "title": "smoke",
    }))' "${AID}" "${SMOKE_ENV_ID}")"
)"
SID="$(echo "${SESSION_JSON}" | json_field id)"
SESSION_ENV_ID="$(
  python3 -c 'import json,sys
sess=json.load(sys.stdin)
expected=sys.argv[1]
actual=sess.get("environment_id")
if actual!=expected:
    raise SystemExit(f"environment_id mismatch: {actual!r} != {expected!r}")
print(actual)' "${SMOKE_ENV_ID}" <<<"${SESSION_JSON}"
)"
echo "SID=${SID} environment_id=${SESSION_ENV_ID}"

if [[ "${SMOKE_SKIP_LLM}" == "1" ]]; then
  echo "smoke test passed (P1 APIs only, SMOKE_SKIP_LLM=1)"
  exit 0
fi

echo "==> send message"
EVENT_RESP="$(
  api_post_json "/v1/sessions/${SID}/events" \
    '{"events":[{"type":"user.message","content":[{"type":"text","text":"Reply with one short sentence only."}]}]}'
)"
echo "${EVENT_RESP}"

echo "==> wait for real LLM response (timeout=${SMOKE_TIMEOUT_SEC}s)"
EVENTS="$(wait_for_agent_reply "${SID}")"
echo "${EVENTS}"
echo ""

REPLY_TEXT="$(
  python3 -c 'import json,sys
for evt in json.load(sys.stdin)["data"]:
    if evt.get("type") != "agent.message" or evt.get("id") == "evt_fake":
        continue
    for block in evt.get("content") or []:
        if block.get("type") == "text":
            print(block.get("text", ""))
            raise SystemExit(0)
raise SystemExit(1)' <<<"${EVENTS}"
)"

echo "AGENT_REPLY=${REPLY_TEXT}"
echo "smoke test passed (P1 APIs + real LLM)"
