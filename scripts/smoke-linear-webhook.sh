#!/usr/bin/env bash
# Smoke: Linear publication credentials + mock install + signed webhook → session.
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
WEBHOOK_SECRET="lin_wh_smoke_$(date +%s)"

if ! curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
  echo "platform not reachable at ${PLATFORM_URL}" >&2
  exit 1
fi

AGENT_ID="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/agents" \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d '{"name":"smoke-linear-webhook","model":"claude-sonnet-4-20250514"}' \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
)"
ENV_ID="$(
  curl -sf "${PLATFORM_URL}/v1/environments?limit=5" \
    -H "x-api-key: ${API_KEY}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"][0]["id"])'
)"

PUB_JSON="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/integrations/linear/publications" \
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
  "${PLATFORM_URL}/v1/integrations/linear/publications/${PUB_ID}/credentials" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${API_KEY}" \
  -d "$(python3 -c 'import json,os,sys; print(json.dumps({
      "clientId": "smoke-client",
      "clientSecret": "smoke-secret",
      "webhookSecret": sys.argv[1],
      "returnUrl": "http://localhost/console",
  }))' "${WEBHOOK_SECRET}")" >/dev/null

curl -sf -X POST \
  "${PLATFORM_URL}/v1/internal/linear/publications/${PUB_ID}/bind-mock-install" \
  -H "Content-Type: application/json" \
  -H "x-internal-secret: ${INTERNAL_SECRET}" \
  -d '{"workspace_id":"org_smoke","workspace_name":"Smoke","bot_user_id":"bot_smoke"}' \
  >/dev/null
echo "mock install bound"

DELIVERY_ID="del_smoke_$(date +%s)"
PAYLOAD="$(python3 -c 'import json,sys; print(json.dumps({
    "type": "AppUserNotification",
    "action": "issueAssignedToYou",
    "webhookId": sys.argv[1],
    "organizationId": "org_smoke",
    "notification": {
        "type": "issueAssignedToYou",
        "issue": {
            "id": "iss_smoke",
            "identifier": "SMOKE-1",
            "title": "Smoke test issue",
        },
        "actor": {"id": "usr_smoke", "name": "Smoke User"},
    },
}))' "${DELIVERY_ID}")"

SIG="$(
  PAYLOAD="${PAYLOAD}" WEBHOOK_SECRET="${WEBHOOK_SECRET}" python3 -c '
import hashlib, hmac, json, os
body = os.environ["PAYLOAD"]
secret = os.environ["WEBHOOK_SECRET"]
print(hmac.new(secret.encode(), body.encode(), hashlib.sha256).hexdigest())
'
)"

WEBHOOK_RESP="$(
  curl -sS -w $'\n%{http_code}' -X POST "${WEBHOOK_URL}" \
    -H "Content-Type: application/json" \
    -H "linear-signature: ${SIG}" \
    -H "linear-delivery: ${DELIVERY_ID}" \
    -d "${PAYLOAD}"
)"
WEBHOOK_BODY="${WEBHOOK_RESP%$'\n'*}"
WEBHOOK_CODE="${WEBHOOK_RESP##*$'\n'}"
if [[ "${WEBHOOK_CODE}" != "200" ]]; then
  echo "webhook failed code=${WEBHOOK_CODE} body=${WEBHOOK_BODY}" >&2
  exit 1
fi

SESSION_ID="$(
  python3 -c 'import json,sys
data=json.loads(sys.argv[1])
assert data.get("ok") is True, data
sid=data.get("session_id")
assert sid, data
print(sid)' "${WEBHOOK_BODY}"
)"
echo "session=${SESSION_ID}"

EVENTS="$(
  curl -sf "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events" \
    -H "x-api-key: ${API_KEY}"
)"
python3 -c 'import json,sys
events=json.loads(sys.argv[1])["data"]
assert events, "no events"
assert events[0]["type"]=="user.message", events[0]
print("user.message event ok")' "${EVENTS}"

curl -sf -X DELETE \
  "${PLATFORM_URL}/v1/integrations/linear/publications/${PUB_ID}" \
  -H "x-api-key: ${API_KEY}" >/dev/null
curl -sf -X DELETE "${PLATFORM_URL}/v1/agents/${AGENT_ID}" \
  -H "x-api-key: ${API_KEY}" >/dev/null

echo "smoke-linear-webhook passed"
