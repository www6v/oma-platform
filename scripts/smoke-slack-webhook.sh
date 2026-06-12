#!/usr/bin/env bash
# Smoke: Slack publication credentials + mock install + signed webhook → session.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "${ROOT}"

if [[ -f "${ROOT}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT}/.env"
  set +a
fi

PLATFORM_URL="${PLATFORM_URL:-http://127.0.0.1:8787}"
INTERNAL_SECRET="${OMA_INTERNAL_SECRET:-dev-internal-secret}"
API_KEY="${OMA_API_KEY:-dev-key}"
SIGNING_SECRET="sl_sign_smoke_$(date +%s)"

if ! curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
  echo "platform not reachable at ${PLATFORM_URL}" >&2
  exit 1
fi

AGENT_ID="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/agents" \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d '{"name":"smoke-slack-webhook","model":"claude-sonnet-4-20250514"}' \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
)"
ENV_ID="$(
  curl -sf "${PLATFORM_URL}/v1/environments?limit=5" \
    -H "x-api-key: ${API_KEY}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"][0]["id"])'
)"

PUB_JSON="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/integrations/slack/publications" \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "$(python3 -c 'import json,os,sys; print(json.dumps({
        "agentId": sys.argv[1],
        "environmentId": sys.argv[2],
        "personaName": "Smoke Bot",
        "returnUrl": "http://localhost/console",
    }))' "${AGENT_ID}" "${ENV_ID}")"
)"
PUB_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["publication_id"])' <<<"${PUB_JSON}")"
WEBHOOK_URL="$(python3 -c 'import json,sys; print(json.load(sys.stdin)["webhook_url"])' <<<"${PUB_JSON}")"
echo "publication=${PUB_ID}"
echo "webhook_url=${WEBHOOK_URL}"

curl -sf -X PATCH \
  "${PLATFORM_URL}/v1/integrations/slack/publications/${PUB_ID}/credentials" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${API_KEY}" \
  -d "$(python3 -c 'import json,os,sys; print(json.dumps({
      "signingSecret": sys.argv[1],
      "returnUrl": "http://localhost/console",
  }))' "${SIGNING_SECRET}")" >/dev/null

curl -sf -X POST \
  "${PLATFORM_URL}/v1/internal/slack/publications/${PUB_ID}/bind-mock-install" \
  -H "Content-Type: application/json" \
  -H "x-internal-secret: ${INTERNAL_SECRET}" \
  -d '{"workspace_id":"T_smoke","workspace_name":"Smoke","bot_user_id":"B_smoke"}' \
  >/dev/null
echo "mock install bound"

PAYLOAD="$(python3 -c 'import json; print(json.dumps({
    "type": "event_callback",
    "event_id": "Ev_slack_smoke",
    "team_id": "T_smoke",
    "event": {
        "type": "app_mention",
        "user": "U_smoke",
        "text": "<@B_smoke> hello",
        "channel": "C_smoke",
        "ts": "1710000000.000100"
    }
}))')"

TS="$(date +%s)"
SIG="$(python3 -c 'import hashlib,hmac,os,sys; secret=sys.argv[1].encode(); ts=sys.argv[2]; body=sys.argv[3].encode(); base=f"v0:{ts}:".encode()+body; print("v0="+hmac.new(secret, base, hashlib.sha256).hexdigest())' "${SIGNING_SECRET}" "${TS}" "${PAYLOAD}")"

RESP="$(curl -sf -X POST "${WEBHOOK_URL}" \
  -H "Content-Type: application/json" \
  -H "X-Slack-Request-Timestamp: ${TS}" \
  -H "X-Slack-Signature: ${SIG}" \
  -d "${PAYLOAD}")"
echo "${RESP}"

SESSION_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id") or "")' <<<"${RESP}")"
if [[ -z "${SESSION_ID}" ]]; then
  echo "webhook did not create session" >&2
  exit 1
fi
echo "session_id=${SESSION_ID}"
echo "slack webhook smoke OK"
