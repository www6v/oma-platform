#!/usr/bin/env bash
# Fast smoke: vault credential -> outbound proxy bearer injection (no LLM).
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
export OUTBOUND_MOCK_PORT="${OUTBOUND_MOCK_PORT:-9888}"
export MOCK_OUTBOUND_TOKEN="${MOCK_OUTBOUND_TOKEN:-outbound-smoke-token}"
export OUTBOUND_MOCK_URL="http://127.0.0.1:${OUTBOUND_MOCK_PORT}"

LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"
if [[ "${LISTEN_ADDR}" == :* ]]; then
  PLATFORM_URL="http://127.0.0.1${LISTEN_ADDR}"
else
  PLATFORM_URL="http://${LISTEN_ADDR}"
fi

OUTBOUND_ADDR="${OMA_OUTBOUND_PROXY_ADDR:-:8790}"
if [[ "${OUTBOUND_ADDR}" == :* ]]; then
  OUTBOUND_HOST="127.0.0.1${OUTBOUND_ADDR}"
else
  OUTBOUND_HOST="${OUTBOUND_ADDR}"
fi

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")

json_field() {
  local field="$1"
  python3 -c 'import json,sys; print(json.load(sys.stdin)[sys.argv[1]])' "$field"
}

api_post_json() {
  local path="$1"
  local body="$2"
  curl -sf -X POST "${PLATFORM_URL}${path}" \
    -H "content-type: application/json" \
    "${API_HEADERS[@]}" \
    -d "${body}"
}

cleanup() {
  if [[ -n "${MOCK_PID:-}" ]] && kill -0 "${MOCK_PID}" 2>/dev/null; then
    kill "${MOCK_PID}" 2>/dev/null || true
    wait "${MOCK_PID}" 2>/dev/null || true
  fi
}
trap cleanup EXIT

echo "==> preflight: platform + outbound proxy on ${OUTBOUND_HOST}"
curl -sf "${PLATFORM_URL}/health" >/dev/null

echo "==> mock upstream ${OUTBOUND_MOCK_URL}"
python3 scripts/mock-outbound-server.py &
MOCK_PID=$!
sleep 0.5

echo "==> vault + credential"
VAULT_ID="$(
  api_post_json "/v1/vaults" '{"name":"outbound-proxy-smoke"}' \
    | json_field id
)"
api_post_json "/v1/vaults/${VAULT_ID}/credentials" "$(cat <<EOF
{
  "display_name": "outbound-proxy-smoke",
  "auth": {
    "type": "static_bearer",
    "mcp_server_url": "${OUTBOUND_MOCK_URL}",
    "token": "${MOCK_OUTBOUND_TOKEN}"
  }
}
EOF
)" >/dev/null

AGENT_ID="$(
  api_post_json "/v1/agents" '{
    "name": "outbound-proxy-smoke-agent",
    "model": "claude-sonnet-4-6",
    "system_prompt": "x",
    "tools": [{"type": "bash"}]
  }' | json_field id
)"

SESSION_ID="$(
  api_post_json "/v1/sessions" "$(cat <<EOF
{
  "title": "outbound-proxy-smoke",
  "agent": {"id": "${AGENT_ID}"}
}
EOF
)" | json_field id
)"

echo "==> curl via outbound proxy (session=${SESSION_ID})"
BODY="$(
  curl -sf \
    -H "X-OMA-Session-Id: ${SESSION_ID}" \
    -H "Proxy-Authorization: Bearer ${OMA_API_KEY}" \
    -x "http://${OUTBOUND_HOST}" \
    "${OUTBOUND_MOCK_URL}/secret"
)"

python3 -c 'import json,sys
body=json.loads(sys.argv[1])
assert body.get("secret")=="vault-injected-ok", body
print("OUTBOUND_PROXY_SMOKE_OK", body["secret"])' "${BODY}"

echo "==> direct mock must reject without bearer"
if curl -sf "${OUTBOUND_MOCK_URL}/secret" 2>/dev/null; then
  echo "OUTBOUND_PROXY_SMOKE_FAIL mock accepted unauthenticated request" >&2
  exit 1
fi
echo "OUTBOUND_PROXY_SMOKE_OK unauthenticated blocked"
