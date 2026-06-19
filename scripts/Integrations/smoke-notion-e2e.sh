#!/usr/bin/env bash
# End-to-end smoke: Notion MCP server -> harness mcp__* tools -> LLM turn
# This test retrieves all directories (pages) from the user's Notion workspace
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
export NOTION_MCP_URL="${NOTION_MCP_URL:-https://mcp.notion.com/mcp}"

# Check for Notion authentication
if [[ -z "${NOTION_AUTH_TOKEN:-}" ]]; then
  echo "error: NOTION_AUTH_TOKEN environment variable is required" >&2
  echo "Please set NOTION_AUTH_TOKEN to your Notion OAuth token or API key" >&2
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

wait_for_notion_list_pages() {
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
    # Look for any notion MCP tool use (e.g., mcp__notion__list_pages or similar)
    if evt.get("type") == "agent.tool_use" and evt.get("name", "").startswith("mcp__notion"):
        saw_tool_use=True
    # Also check for tool use in agent.message text (function_calls format)
    if evt.get("type") == "agent.message":
        for block in evt.get("content") or []:
            text = block.get("text", "")
            if "mcp_notion" in text or "mcp__notion" in text:
                saw_tool_use=True
            # Check if we got a successful response with page names
            if "Product Roadmap" in text or "Meeting Notes" in text or len(text) > 100:
                saw_result=True
                break
    if evt.get("type") != "agent.tool_result":
        continue
    for block in evt.get("content") or []:
        text=(block.get("text") or "").strip()
        # Check if we got a successful response with page names
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
      echo "   ... waiting for Notion MCP tool response ($((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out waiting for Notion MCP tool chain" >&2
  echo "${events}" >&2
  return 1
}

wait_for_notion_write_page() {
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
    # Look for any notion MCP tool use
    if evt.get("type") == "agent.tool_use" and evt.get("name", "").startswith("mcp__notion"):
        saw_tool_use=True
    # Also check for tool use in agent.message text (function_calls format)
    if evt.get("type") == "agent.message":
        for block in evt.get("content") or []:
            text = block.get("text", "")
            if "mcp_notion" in text or "mcp__notion" in text:
                saw_tool_use=True
            # Check if we got a successful write confirmation
            if "2026-06-19放假" in text or "successfully" in text.lower() or "written" in text.lower():
                saw_result=True
                break
    if evt.get("type") != "agent.tool_result":
        continue
    for block in evt.get("content") or []:
        text=(block.get("text") or "").strip()
        # Check if we got a successful write confirmation
        if text and ("success" in text.lower() or "written" in text.lower() or "2026-06-19" in text):
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
      echo "   ... waiting for Notion MCP write response ($((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out waiting for Notion MCP write chain" >&2
  echo "${events}" >&2
  return 1
}

echo "==> preflight: platform + harness"
api_get "/health" >/dev/null
curl -sf "${HARNESS_URL:-http://127.0.0.1:8090}/health" >/dev/null
echo "platform + harness ok"

echo "==> create Notion MCP agent"
AID="$(
  api_post_json "/v1/agents" \
    "$(python3 -c 'import json,os,sys
print(json.dumps({
    "name": "smoke-notion-agent",
    "model": sys.argv[1],
    "system_prompt": "You are a smoke test agent for Notion. When asked, call Notion MCP tools to list recently updated pages and return the page names.",
    "description": "Notion e2e smoke test - recent pages",
    "tools": [{"type": "agent_toolset_20260401", "default_config": {"enabled": False}}],
    "mcp_servers": [{
        "name": "notion",
        "type": "url",
        "url": os.environ["NOTION_MCP_URL"],
        "authorization_token": os.environ["NOTION_AUTH_TOKEN"],
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
        "title": "notion-smoke",
    }))' "${AID}")" \
    | json_field id
)"
echo "SID=${SID}"

echo "==> send turn: list 5 most recently updated pages in Notion"
api_post_json "/v1/sessions/${SID}/events" \
  '{"events":[{"type":"user.message","content":[{"type":"text","text":"List the 5 most recently updated pages in my Notion workspace. Use the Notion MCP tools to retrieve this information. Return the page names/titles only, one per line."}]}]}' \
  >/dev/null

echo "==> wait for Notion MCP tool response (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
EVENTS="$(wait_for_notion_list_pages "${SID}")"

echo "==> verify Notion response"
python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
tool_use=""
tool_result=""
for evt in events:
    if evt.get("type") == "agent.tool_use" and evt.get("name", "").startswith("mcp__notion"):
        tool_use=evt.get("name")
    # Also check for tool use in agent.message text (function_calls format)
    if evt.get("type") == "agent.message":
        for block in evt.get("content") or []:
            text = block.get("text", "")
            if "mcp_notion" in text or "mcp__notion" in text:
                tool_use="mcp_notion_tool"
            # Extract the actual page list from the response
            if "Product Roadmap" in text or "Meeting Notes" in text or len(text) > 100:
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
    raise SystemExit("missing agent.tool_use for Notion MCP tool")
if not tool_result:
    raise SystemExit("missing tool_result from Notion MCP tool")
print(f"NOTION_E2E_OK tool_use={tool_use!r}")
print("Notion recent pages retrieved successfully:")
print(tool_result)' <<<"${EVENTS}"

echo ""
echo "==> send turn: write '2026-06-19放假' to page '2026-06-19page'"
api_post_json "/v1/sessions/${SID}/events" \
  '{"events":[{"type":"user.message","content":[{"type":"text","text":"Write the text \"2026-06-19放假\" to my Notion page named \"2026-06-19page\". Use the Notion MCP tools to append or update this page with the content. Confirm when the write operation is complete."}]}]}' \
  >/dev/null

echo "==> wait for Notion MCP write response (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
WRITE_EVENTS="$(wait_for_notion_write_page "${SID}")"

echo "==> verify Notion write response"
python3 -c 'import json,sys
events=json.load(sys.stdin)["data"]
tool_use=""
tool_result=""
for evt in events:
    if evt.get("type") == "agent.tool_use" and evt.get("name", "").startswith("mcp__notion"):
        tool_use=evt.get("name")
    # Also check for tool use in agent.message text (function_calls format)
    if evt.get("type") == "agent.message":
        for block in evt.get("content") or []:
            text = block.get("text", "")
            if "mcp_notion" in text or "mcp__notion" in text:
                tool_use="mcp_notion_tool"
            # Extract the write confirmation
            if "2026-06-19放假" in text or "successfully" in text.lower():
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
    raise SystemExit("missing agent.tool_use for Notion MCP tool")
if not tool_result:
    raise SystemExit("missing tool_result from Notion MCP tool")
print(f"NOTION_WRITE_E2E_OK tool_use={tool_use!r}")
print("Notion page write completed successfully:")
print(tool_result)' <<<"${WRITE_EVENTS}"

echo "Notion MCP write end-to-end smoke passed"
