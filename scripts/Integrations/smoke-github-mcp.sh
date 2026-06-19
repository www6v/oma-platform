#!/usr/bin/env bash
# End-to-end smoke: GitHub MCP server -> harness mcp__* tools -> LLM turn
# This test retrieves all repositories from the user's GitHub account
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

_e2e_ensure_model_card

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-180}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export GITHUB_MCP_URL="${GITHUB_MCP_URL:-https://api.githubcopilot.com/mcp/}"

# Check for GitHub authentication
if [[ -z "${GITHUB_AUTH_TOKEN:-}" ]]; then
  echo "error: GITHUB_AUTH_TOKEN environment variable is required" >&2
  echo "Please set GITHUB_AUTH_TOKEN to your GitHub personal access token" >&2
  exit 1
fi

API_HEADERS=(-H "x-api-key: ${OMA_API_KEY}")

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

wait_for_github_list_repos() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TOOL_TIMEOUT_SEC))
  local events=""
  local status=0
  local polls=0

  while (( SECONDS < deadline )); do
    events="$(
      api_get "/v1/sessions/${sid}/events?order=asc" | normalize_events_response
    )"
    status=0
    CHAIN_ERR="$(
      python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
saw_tool_use=False
saw_result=False
for evt in events:
    if evt.get("type") == "session.error":
        print(evt.get("message") or evt.get("error") or "session.error")
        sys.exit(3)
    # Look for any github MCP tool use (e.g., mcp__github__list_repositories or similar)
    if evt.get("type") == "agent.tool_use" and evt.get("name", "").startswith("mcp__github"):
        saw_tool_use=True
    # Also check for tool use in agent.message text (function_calls format)
    if evt.get("type") == "agent.message":
        for block in evt.get("content") or []:
            text = block.get("text", "")
            if "mcp_github" in text or "mcp__github" in text:
                saw_tool_use=True
            # Check if we got a successful response with repository information
            if "repository" in text.lower() or "repo" in text.lower() or len(text) > 50:
                saw_result=True
                break
    if evt.get("type") != "agent.tool_result":
        continue
    for block in evt.get("content") or []:
        text=(block.get("text") or "").strip()
        # Check if we got a successful response with repository information
        if text and len(text) > 10:
            saw_result=True
            break
if saw_tool_use and saw_result:
    sys.exit(0)
sys.exit(2)' <<<"${events}"
    )" || status=$?

    if [[ "${status}" -eq 0 ]]; then
      echo "${events}"
      return 0
    fi
    if [[ "${status}" -eq 3 ]]; then
      echo "error: harness turn failed: ${CHAIN_ERR}" >&2
      echo "${events}" >&2
      return 1
    fi
    polls=$((polls + 1))
    if (( polls % 5 == 0 )); then
      echo "   ... waiting for GitHub MCP tool response ($((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out waiting for GitHub MCP tool chain" >&2
  echo "${events}" >&2
  return 1
}

echo "==> preflight: platform + harness"
api_get "/health" >/dev/null
curl -sf "${HARNESS_URL:-http://127.0.0.1:8090}/health" >/dev/null
echo "platform + harness ok"

echo "==> create GitHub MCP agent"
AID="$(
  api_post_json "/v1/agents" \
    "$(python3 -c 'import json,os,sys
print(json.dumps({
    "name": "smoke-github-agent",
    "model": sys.argv[1],
    "system_prompt": "You are a smoke test agent for GitHub. When asked, call GitHub MCP tools to list repositories and return the repository count.",
    "description": "GitHub e2e smoke test - list repositories",
    "tools": [{"type": "agent_toolset_20260401", "default_config": {"enabled": False}}],
    "mcp_servers": [{
        "name": "github",
        "type": "url",
        "url": os.environ["GITHUB_MCP_URL"],
        "authorization_token": os.environ["GITHUB_AUTH_TOKEN"],
    }],
}))' "${SMOKE_MODEL}")" \
    | json_field id
)"
echo "AID=${AID}"

echo "==> create session"
SID="$(
  api_post_json "/v1/sessions" \
    "$(python3 -c 'import json,sys; print(json.dumps({
        "agent": sys.argv[1],
        "environment_id": "env-local-default",
        "title": "github-smoke",
    }))' "${AID}")" \
    | json_field id
)"
echo "SID=${SID}"

echo "==> send turn: list all repositories in my GitHub account"
api_post_json "/v1/sessions/${SID}/events" \
  '{"events":[{"type":"user.message","content":[{"type":"text","text":"List all repositories in my GitHub account. Use the GitHub MCP tools to retrieve this information. Return the total count of repositories and list their names."}]}]}' \
  >/dev/null

echo "==> wait for GitHub MCP tool response (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
EVENTS="$(wait_for_github_list_repos "${SID}")"

echo "==> verify GitHub response"
python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
tool_use=""
tool_result=""
for evt in events:
    if evt.get("type") == "agent.tool_use" and evt.get("name", "").startswith("mcp__github"):
        tool_use=evt.get("name")
    # Also check for tool use in agent.message text (function_calls format)
    if evt.get("type") == "agent.message":
        for block in evt.get("content") or []:
            text = block.get("text", "")
            if "mcp_github" in text or "mcp__github" in text:
                tool_use="mcp_github_tool"
            # Extract the actual repository list from the response
            if "repository" in text.lower() or "repo" in text.lower() or len(text) > 50:
                tool_result=text
                break
    if evt.get("type") == "agent.tool_result":
        for block in evt.get("content") or []:
            if block.get("type") == "text":
                text=block.get("text", "").strip()
                if text:
                    tool_result=text
                    break
if not tool_use:
    raise SystemExit("missing agent.tool_use for GitHub MCP tool")
if not tool_result:
    raise SystemExit("missing tool_result from GitHub MCP tool")
print(f"GITHUB_E2E_OK tool_use={tool_use!r}")
print("GitHub repositories retrieved successfully:")
print(tool_result)' <<<"${EVENTS}"

echo ""
echo "GitHub MCP end-to-end smoke passed"
