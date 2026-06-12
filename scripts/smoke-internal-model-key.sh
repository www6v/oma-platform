#!/usr/bin/env bash
# Smoke: internal model card key + resolve endpoints (no LLM).
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

if ! curl -sf "${PLATFORM_URL}/health" >/dev/null 2>&1; then
  echo "platform not reachable at ${PLATFORM_URL}" >&2
  exit 1
fi

preflight="$(
  curl -sS -w $'\n%{http_code}' \
    "${PLATFORM_URL}/v1/internal/model_cards/resolve?tenant_id=default&model_id=__smoke__" \
    -H "x-internal-secret: ${INTERNAL_SECRET}" 2>/dev/null || true
)"
preflight_body="${preflight%$'\n'*}"
preflight_code="${preflight##*$'\n'}"
if [[ "${preflight_code}" == "200" ]] && [[ "${preflight_body}" == "<!DOCTYPE html>"* ]]; then
  echo "error: /v1/internal/* returned console HTML — oma-server likely needs restart" >&2
  echo "  set OMA_INTERNAL_SECRET in .env and restart ./start-console.sh" >&2
  exit 1
fi
if [[ "${preflight_code}" == "503" ]]; then
  echo "error: internal endpoints not configured on platform (OMA_INTERNAL_SECRET)" >&2
  echo "  add OMA_INTERNAL_SECRET to .env and restart ./start-console.sh" >&2
  exit 1
fi
if [[ "${preflight_code}" == "401" ]]; then
  echo "error: x-internal-secret rejected (got 401)" >&2
  echo "  ensure OMA_INTERNAL_SECRET matches between .env and this script" >&2
  exit 1
fi

CARD_ID="smoke-internal-$(date +%s)"
CREATE_BODY="$(python3 -c 'import json,os,sys; print(json.dumps({
    "model_id": sys.argv[1],
    "model": "claude-sonnet-4-20250514",
    "provider": "ant",
    "api_key": "sk-smoke-internal",
    "base_url": "https://api.smoke.test",
}))' "${CARD_ID}")"

ROW_ID="$(
  curl -sf -X POST "${PLATFORM_URL}/v1/model_cards" \
    -H "Content-Type: application/json" \
    -H "x-api-key: ${API_KEY}" \
    -d "${CREATE_BODY}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
)"
echo "model_card=${ROW_ID}"

KEY="$(
  curl -sf "${PLATFORM_URL}/v1/internal/model_cards/${ROW_ID}/key?tenant_id=default" \
    -H "x-internal-secret: ${INTERNAL_SECRET}" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["api_key"])'
)"
if [[ "${KEY}" != "sk-smoke-internal" ]]; then
  echo "unexpected api_key=${KEY}" >&2
  exit 1
fi
echo "internal key ok"

RESOLVED="$(
  curl -sf \
    "${PLATFORM_URL}/v1/internal/model_cards/resolve?tenant_id=default&model_id=${CARD_ID}" \
    -H "x-internal-secret: ${INTERNAL_SECRET}"
)"
python3 -c 'import json,sys
data=json.loads(sys.argv[1])
assert data["api_key"]=="sk-smoke-internal", data
assert data["model"]=="claude-sonnet-4-20250514", data
assert data["base_url"]=="https://api.smoke.test", data
print("internal resolve ok")' "${RESOLVED}"

curl -sf -X DELETE "${PLATFORM_URL}/v1/model_cards/${ROW_ID}" \
  -H "x-api-key: ${API_KEY}" >/dev/null
echo "smoke-internal-model-key passed"
