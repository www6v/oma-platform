#!/usr/bin/env bash
# Smoke: GitHub publication credentials + mock install + signed webhook → session.
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
WEBHOOK_SECRET="gh_wh_smoke_$(date +%s)"

if ! curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
  echo "platform not reachable at ${PLATFORM_URL}" >&2
  exit 1
fi

AGENT_ID="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/agents" \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d '{"name":"smoke-github-webhook","model":"claude-sonnet-4-20250514"}' \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
)"
ENV_ID="$(
  curl -sf "${PLATFORM_URL}/v1/environments?limit=5" \
    -H "x-api-key: ${API_KEY}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["data"][0]["id"])'
)"

PUB_JSON="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/integrations/github/publications" \
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
  "${PLATFORM_URL}/v1/integrations/github/publications/${PUB_ID}/credentials" \
  -H "Content-Type: application/json" \
  -H "x-api-key: ${API_KEY}" \
  -d "$(python3 -c 'import json,os,sys; print(json.dumps({
      "webhookSecret": sys.argv[1],
      "returnUrl": "http://localhost/console",
  }))' "${WEBHOOK_SECRET}")" >/dev/null

curl -sf -X POST \
  "${PLATFORM_URL}/v1/internal/github/publications/${PUB_ID}/bind-mock-install" \
  -H "Content-Type: application/json" \
  -H "x-internal-secret: ${INTERNAL_SECRET}" \
  -d '{"workspace_id":"org_smoke","workspace_name":"Smoke","bot_user_id":"bot_smoke"}' \
  >/dev/null
echo "mock install bound"

DELIVERY_ID="del_gh_smoke_$(date +%s)"
PAYLOAD="$(python3 -c 'import json,sys; print(json.dumps({
    "action": "labeled",
    "repository": {"full_name": "acme/demo"},
    "issue": {
        "number": 42,
        "title": "Smoke issue",
        "body": "Please help",
        "html_url": "https://github.com/acme/demo/issues/42",
        "labels": [{"name": "smoke bot"}]
    },
    "label": {"name": "smoke bot"},
    "sender": {"login": "alice"}
}))')"

SIG="$(python3 -c 'import hashlib,hmac,os,sys; secret=sys.argv[1].encode(); body=sys.argv[2].encode(); print("sha256="+hmac.new(secret, body, hashlib.sha256).hexdigest())' "${WEBHOOK_SECRET}" "${PAYLOAD}")"

RESP="$(curl -sf -X POST "${WEBHOOK_URL}" \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: issues" \
  -H "X-GitHub-Delivery: ${DELIVERY_ID}" \
  -H "X-Hub-Signature-256: ${SIG}" \
  -d "${PAYLOAD}")"
echo "${RESP}"

SESSION_ID="$(python3 -c 'import json,sys; print(json.load(sys.stdin).get("session_id") or "")' <<<"${RESP}")"
if [[ -z "${SESSION_ID}" ]]; then
  echo "webhook did not create session" >&2
  exit 1
fi
echo "session_id=${SESSION_ID}"
echo "github webhook smoke OK"
