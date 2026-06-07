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
export SMOKE_TIMEOUT_SEC="${SMOKE_TIMEOUT_SEC:-120}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"

if [[ "${OMA_FAKE_HARNESS}" == "1" ]]; then
  echo "error: OMA_FAKE_HARNESS=1 uses fake responses, not a real LLM" >&2
  echo "set OMA_FAKE_HARNESS=0 in .env and restart start-platform.sh + start-harness.sh" >&2
  exit 1
fi

LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
if [[ "${LISTEN_ADDR}" == :* ]]; then
  PLATFORM_URL="http://127.0.0.1${LISTEN_ADDR}"
else
  PLATFORM_URL="http://${LISTEN_ADDR}"
fi

json_field() {
  local field="$1"
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$field"
}

wait_for_agent_reply() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TIMEOUT_SEC))
  local events=""
  local status=0

  while (( SECONDS < deadline )); do
    events="$(
      curl -sf "${PLATFORM_URL}/v1/sessions/${sid}/events?order=asc" \
        -H "x-api-key: ${OMA_API_KEY}"
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

echo "==> health ${PLATFORM_URL}/health"
curl -sf "${PLATFORM_URL}/health"
echo ""

echo "==> create agent (model=${SMOKE_MODEL})"
AID="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/agents" \
    -H "content-type: application/json" \
    -H "x-api-key: ${OMA_API_KEY}" \
    -d "{\"name\":\"hello\",\"model\":\"${SMOKE_MODEL}\",\"system_prompt\":\"You are helpful.\"}" \
    | json_field id
)"
echo "AID=${AID}"

echo "==> create session"
SID="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/sessions" \
    -H "content-type: application/json" \
    -H "x-api-key: ${OMA_API_KEY}" \
    -d "{\"agent\":\"${AID}\"}" \
    | json_field id
)"
echo "SID=${SID}"

echo "==> send message"
EVENT_RESP="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/sessions/${SID}/events" \
    -H "content-type: application/json" \
    -H "x-api-key: ${OMA_API_KEY}" \
    -d '{"events":[{"type":"user.message","content":[{"type":"text","text":"Reply with one short sentence only."}]}]}'
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
echo "smoke test passed (real LLM)"
