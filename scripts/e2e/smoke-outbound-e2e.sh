#!/usr/bin/env bash
# End-to-end smoke: vault credential -> outbound proxy -> curl via harness bash
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

_e2e_pin_local_outbound_stack

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-180}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export OUTBOUND_MOCK_PORT="${OUTBOUND_MOCK_PORT:-9888}"
export MOCK_OUTBOUND_TOKEN="${MOCK_OUTBOUND_TOKEN:-outbound-smoke-token}"
export OUTBOUND_MOCK_URL="http://127.0.0.1:${OUTBOUND_MOCK_PORT}"

OUTBOUND_HOST="$(_e2e_outbound_host)"

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")

json_field() {
  _e2e_json_field "$1"
}

api_get() {
  curl -sf "${PLATFORM_URL}$1" "${API_HEADERS[@]}"
}

api_post_json() {
  _e2e_api_post_json "$1" "$2"
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

wait_for_curl_secret() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TOOL_TIMEOUT_SEC))
  local events=""

  while (( SECONDS < deadline )); do
    events="$(
      api_get "/v1/sessions/${sid}/events?order=asc" | normalize_events_response
    )"
    if OUTBOUND_OK="$(
      python3 -c 'import json,sys
needle="vault-injected-ok"
for ev in json.load(sys.stdin)["data"]:
    if ev.get("type") != "agent.message":
        continue
    for block in ev.get("content") or []:
        text=block.get("text") or ""
        if needle in text:
            print("yes")
            raise SystemExit(0)
raise SystemExit(1)' <<<"${events}"
    )"; then
      echo "OUTBOUND_E2E_OK session=${sid} saw_secret=${OUTBOUND_OK}"
      return 0
    fi
    sleep "${SMOKE_POLL_SEC}"
  done
  echo "OUTBOUND_E2E_FAIL timeout waiting for vault-injected response" >&2
  echo "${events}" | python3 -m json.tool >&2 || true
  return 1
}

echo "==> preflight: platform + harness"
api_get "/health" >/dev/null
curl -sf "${HARNESS_URL:-http://127.0.0.1:8090}/health" >/dev/null
echo "platform + harness ok (outbound proxy expected on ${OUTBOUND_HOST})"

echo "== outbound e2e: mock upstream on ${OUTBOUND_MOCK_URL}"
_e2e_start_outbound_mock "${ROOT_DIR}"
trap 'kill ${MOCK_PID} 2>/dev/null || true' EXIT

echo "== create vault + credential for ${OUTBOUND_MOCK_URL}"
VAULT_ID="$(
  api_post_json "/v1/vaults" '{"name":"outbound-smoke-vault"}' \
    | json_field id
)"
api_post_json "/v1/vaults/${VAULT_ID}/credentials" "$(cat <<EOF
{
  "display_name": "outbound-smoke",
  "auth": {
    "type": "static_bearer",
    "mcp_server_url": "${OUTBOUND_MOCK_URL}",
    "token": "${MOCK_OUTBOUND_TOKEN}"
  }
}
EOF
)" >/dev/null

echo "== create agent + session"
AGENT_ID="$(
  api_post_json "/v1/agents" "$(cat <<EOF
{
  "name": "outbound-smoke-agent",
  "model": "${SMOKE_MODEL}",
  "system_prompt": "You must run exactly one bash command and report stdout verbatim.",
  "tools": [{"type": "bash"}]
}
EOF
)" | json_field id
)"

SESSION_ID="$(
  api_post_json "/v1/sessions" "$(cat <<EOF
{
  "title": "outbound-smoke",
  "agent": {"id": "${AGENT_ID}"}
}
EOF
)" | json_field id
)"

echo "== enqueue user message (curl protected API via outbound proxy ${OUTBOUND_HOST})"
api_post_json "/v1/sessions/${SESSION_ID}/events" "$(cat <<EOF
{
  "events": [
    {
      "type": "user.message",
      "content": [
        {
          "type": "text",
          "text": "Run bash: curl -s ${OUTBOUND_MOCK_URL}/secret and reply with only the raw stdout."
        }
      ]
    }
  ]
}
EOF
)" >/dev/null

wait_for_curl_secret "${SESSION_ID}"

WORKDIR="${SANDBOX_WORKDIR:-./data/sandboxes}/${SESSION_ID}"
if [[ -f "${WORKDIR}/.curlrc" ]]; then
  if grep -q "Proxy-Authorization" "${WORKDIR}/.curlrc"; then
    echo "OUTBOUND_E2E_OK curlrc_has_proxy_auth=true"
  else
    echo "OUTBOUND_E2E_WARN curlrc missing proxy auth header" >&2
  fi
  if grep -q "${MOCK_OUTBOUND_TOKEN}" "${WORKDIR}/.curlrc"; then
    echo "OUTBOUND_E2E_FAIL token leaked into workdir .curlrc" >&2
    exit 1
  fi
fi

echo "OUTBOUND_E2E_OK complete"
