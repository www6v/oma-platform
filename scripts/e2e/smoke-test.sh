#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/e2e/common.sh"

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_MODEL_CARD_ID="${SMOKE_MODEL_CARD_ID:-smoke-claude}"
export SMOKE_TIMEOUT_SEC="${SMOKE_TIMEOUT_SEC:-120}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export SMOKE_SKIP_LLM="${SMOKE_SKIP_LLM:-0}"
export SMOKE_SKIP_TOOLS="${SMOKE_SKIP_TOOLS:-0}"
export SMOKE_TOOLS_ONLY="${SMOKE_TOOLS_ONLY:-0}"
export SMOKE_SKIP_P2="${SMOKE_SKIP_P2:-0}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-180}"

DEFAULT_ENV_ID="env-local-default"

if [[ "${SMOKE_SKIP_LLM}" != "1" && "${OMA_FAKE_HARNESS}" == "1" ]]; then
  echo "error: OMA_FAKE_HARNESS=1 uses fake responses, not a real LLM" >&2
  echo "set OMA_FAKE_HARNESS=0 in .env and restart start-platform.sh + start-harness.sh" >&2
  echo "or set SMOKE_SKIP_LLM=1 to exercise P1 APIs only" >&2
  exit 1
fi

API_HEADERS=(
  -H "x-api-key: ${OMA_API_KEY}"
)

json_field() {
  local field="$1"
  python3 "${SMOKE_UTILS}" json-field "$field"
}

# PR-07 events list returns { data: [{ seq, type, ts, data }] }; unwrap inner payloads.
normalize_events_response() {
  python3 "${SMOKE_UTILS}" normalize-events
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

api_patch_json() {
  local path="$1"
  local body="$2"
  curl -sf -X PATCH "${PLATFORM_URL}${path}" \
    -H "content-type: application/json" \
    "${API_HEADERS[@]}" \
    -d "${body}"
}

assert_events_ama_shape() {
  local label="$1"
  python3 "${SMOKE_UTILS}" assert-events-ama-shape "${label}"
}

assert_trajectory_shape() {
  local label="$1"
  local min_events="${2:-0}"
  python3 "${SMOKE_UTILS}" assert-trajectory-shape "${label}" "${min_events}"
}

wait_for_agent_reply() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TIMEOUT_SEC))
  local events=""
  local status=0
  local polls=0

  while (( SECONDS < deadline )); do
    events="$(
      api_get "/v1/sessions/${sid}/events?order=asc" | normalize_events_response
    )"
    status=0
    TURN_ERR="$(
      python3 "${SMOKE_UTILS}" check-events-agent-reply <<<"${events}"
    )" || status=$?

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
    if [[ "${status}" -eq 3 ]]; then
      echo "error: harness turn failed: ${TURN_ERR}" >&2
      echo "check start-harness.sh logs; refresh model card api_key if using smoke-claude" >&2
      echo "${events}" >&2
      return 1
    fi
    polls=$((polls + 1))
    if (( polls % 5 == 0 )); then
      echo "   ... still waiting (${polls} polls, $((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out after ${SMOKE_TIMEOUT_SEC}s waiting for real agent.message" >&2
  echo "hint: ensure start-platform.sh and start-harness.sh are running with OMA_FAKE_HARNESS=0" >&2
  echo "${events}" >&2
  return 1
}

wait_for_bash_uname_chain() {
  local sid="$1"
  local deadline=$((SECONDS + SMOKE_TOOL_TIMEOUT_SEC))
  local events=""
  local status=0
  local polls=0
  local chain_err=""

  while (( SECONDS < deadline )); do
    events="$(
      api_get "/v1/sessions/${sid}/events?order=asc" | normalize_events_response
    )"
    status=0
    chain_err="$(
      python3 "${SMOKE_UTILS}" check-events-bash-uname <<<"${events}"
    )" || status=$?

    if [[ "${status}" -eq 0 ]]; then
      echo "${events}"
      return 0
    fi
    if [[ "${status}" -eq 1 ]]; then
      echo "error: got evt_fake during tool chain smoke" >&2
      echo "${events}" >&2
      return 1
    fi
    if [[ "${status}" -eq 3 ]]; then
      echo "error: harness tool turn failed: ${chain_err}" >&2
      echo "${events}" >&2
      return 1
    fi
    if [[ "${status}" -eq 4 ]]; then
      echo "error: ${chain_err}" >&2
      echo "restart ./start-platform.sh so workdir paths are absolute for harness" >&2
      echo "${events}" >&2
      return 1
    fi
    polls=$((polls + 1))
    if (( polls % 5 == 0 )); then
      echo "   ... waiting for bash+uname ($((polls)) polls, $((deadline - SECONDS))s left)" >&2
    fi
    sleep "${SMOKE_POLL_SEC}"
  done

  echo "error: timed out after ${SMOKE_TOOL_TIMEOUT_SEC}s waiting for bash uname tool chain" >&2
  echo "hint: agent needs tools=[{type:agent_toolset_20260401}] and OMA_FAKE_HARNESS=0" >&2
  echo "${events}" >&2
  return 1
}

SMOKE_ENV_ID="${DEFAULT_ENV_ID}"
SMOKE_VAULT_ID=""
SMOKE_SKILL_ID=""
SID=""
AID=""
AGENT_MODEL="${SMOKE_MODEL}"
MODEL_CARD_ROW_ID=""

print_leftover_resources() {
  echo ""
  echo "leftover resources (not cleaned up):"
  echo "  SMOKE_ENV_ID=${SMOKE_ENV_ID}"
  echo "  AID=${AID}"
  echo "  SID=${SID}"
  if [[ -n "${SMOKE_SKILL_ID}" ]]; then
    echo "  SMOKE_SKILL_ID=${SMOKE_SKILL_ID}"
  fi
  if [[ -n "${SMOKE_VAULT_ID}" ]]; then
    echo "  SMOKE_VAULT_ID=${SMOKE_VAULT_ID}"
  fi
  if [[ -n "${MODEL_CARD_ROW_ID}" ]]; then
    echo "  MODEL_CARD_ROW_ID=${MODEL_CARD_ROW_ID}"
  fi
}

echo "==> health ${PLATFORM_URL}/health"
api_get "/health" >/dev/null
echo "platform ok"

if [[ "${SMOKE_SKIP_LLM}" != "1" ]]; then
  echo "==> health ${HARNESS_URL}/health"
  curl -sf "${HARNESS_URL}/health" >/dev/null
  echo "harness ok"
fi

echo "==> get /v1/me"
ME_JSON="$(api_get "/v1/me")"
python3 "${SMOKE_UTILS}" check-me <<<"${ME_JSON}"

echo "==> get /v1/stats"
STATS_JSON="$(api_get "/v1/stats")"
python3 "${SMOKE_UTILS}" check-stats <<<"${STATS_JSON}"

echo "==> list environments (expect ${DEFAULT_ENV_ID})"
ENV_LIST="$(api_get "/v1/environments")"
python3 "${SMOKE_UTILS}" check-environments "${DEFAULT_ENV_ID}" <<<"${ENV_LIST}"

echo "==> get default environment"
DEFAULT_ENV="$(api_get "/v1/environments/${DEFAULT_ENV_ID}")"
python3 "${SMOKE_UTILS}" check-environment "${DEFAULT_ENV_ID}" <<<"${DEFAULT_ENV}"

echo "==> create smoke environment"
SMOKE_ENV_ID="$(
  api_post_json "/v1/environments" \
    '{"name":"smoke-test","description":"smoke-test.sh","config":{"type":"local"}}' \
    | json_field id
)"
echo "SMOKE_ENV_ID=${SMOKE_ENV_ID}"

if [[ "${SMOKE_SKIP_P2}" != "1" ]]; then
  echo "==> console stub endpoints (empty states for Console SPA)"
  for stub_path in \
    "/v1/runtimes" \
    "/v1/models/list" \
    "/v1/files" \
    "/v1/memory_stores" \
    "/v1/evals/runs" \
    "/v1/integrations/linear/installations" \
    "/v1/integrations/github/publications"
  do
    api_get "${stub_path}" >/dev/null
    echo "   ok ${stub_path}"
  done

  echo "==> list builtin skills"
  SKILLS_LIST="$(api_get "/v1/skills")"
  python3 "${SMOKE_UTILS}" check-skills <<<"${SKILLS_LIST}"

  echo "==> get builtin skill builtin_pdf"
  api_get "/v1/skills/builtin_pdf" >/dev/null
  echo "builtin_pdf ok"

  echo "==> create custom skill"
  SMOKE_SKILL_ID="$(
    api_post_json "/v1/skills" \
      "$(python3 "${SMOKE_UTILS}" make-skill-body)" \
      | json_field id
  )"
  echo "SMOKE_SKILL_ID=${SMOKE_SKILL_ID}"

  echo "==> list custom skill versions"
  SKILL_VERSIONS="$(api_get "/v1/skills/${SMOKE_SKILL_ID}/versions")"
  python3 "${SMOKE_UTILS}" check-skill-versions <<<"${SKILL_VERSIONS}"

  echo "==> create vault + credential"
  SMOKE_VAULT_ID="$(
    api_post_json "/v1/vaults" '{"name":"smoke-vault"}' | json_field id
  )"
  echo "SMOKE_VAULT_ID=${SMOKE_VAULT_ID}"
  CRED_JSON="$(
    api_post_json "/v1/vaults/${SMOKE_VAULT_ID}/credentials" \
      '{"display_name":"Smoke MCP","auth":{"type":"mcp_oauth","mcp_server_url":"https://mcp.example.com","access_token":"smoke-secret"}}'
  )"
  python3 "${SMOKE_UTILS}" check-credential <<<"${CRED_JSON}"
fi

echo "==> list model cards"
CARD_LIST="$(api_get "/v1/model_cards")"
CARD_COUNT="$(python3 "${SMOKE_UTILS}" count-model-cards <<<"${CARD_LIST}")"
echo "model_cards=${CARD_COUNT}"

if [[ -n "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "==> ensure model card (model_id=${SMOKE_MODEL_CARD_ID}, model=${SMOKE_MODEL})"
  CARD_BODY="$(python3 "${SMOKE_UTILS}" make-model-card-body "${SMOKE_MODEL_CARD_ID}" "${SMOKE_MODEL}")"
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
      python3 "${SMOKE_UTILS}" find-model-card "${SMOKE_MODEL_CARD_ID}" <<<"${CARD_LIST}"
    )"
    echo "MODEL_CARD_ROW_ID=${MODEL_CARD_ROW_ID} (existing, refreshing api_key)"
    api_post_json "/v1/model_cards/${MODEL_CARD_ROW_ID}" \
      "$(python3 "${SMOKE_UTILS}" make-model-card-update-body "${SMOKE_MODEL}")" >/dev/null
  else
    echo "error: model card POST status=${CARD_HTTP}" >&2
    cat /tmp/oma-smoke-card.json >&2 || true
    exit 1
  fi

  echo "==> get model card key (redacted)"
  KEY_PREVIEW="$(
    api_get "/v1/model_cards/${MODEL_CARD_ROW_ID}/key" \
      | python3 "${SMOKE_UTILS}" check-key-preview
  )"
  echo "api_key ${KEY_PREVIEW}"

  AGENT_MODEL="${SMOKE_MODEL_CARD_ID}"
else
  echo "==> no ANTHROPIC_API_KEY — piPy local auth (agent.model=${SMOKE_MODEL})"
  echo "==> remove stale model cards (they inject test api keys and hang LLM)"
  while IFS= read -r card_id; do
    [[ -z "${card_id}" ]] && continue
    echo "    delete model card ${card_id}"
    curl -sf -X DELETE "${PLATFORM_URL}/v1/model_cards/${card_id}" \
      "${API_HEADERS[@]}" >/dev/null
  done < <(
    python3 "${SMOKE_UTILS}" list-model-card-ids <<<"${CARD_LIST}"
  )
fi

echo "==> create agent (model=${AGENT_MODEL})"
AID="$(
  api_post_json "/v1/agents" \
    "$(python3 "${SMOKE_UTILS}" make-agent-body "${AGENT_MODEL}")" \
    | json_field id
)"
echo "AID=${AID}"

echo "==> list agent versions (historical only)"
VERSIONS="$(api_get "/v1/agents/${AID}/versions")"
VERSION_COUNT="$(python3 "${SMOKE_UTILS}" count-agent-versions <<<"${VERSIONS}")"
echo "agent_versions=${VERSION_COUNT}"

echo "==> get agent"
AGENT_JSON="$(api_get "/v1/agents/${AID}")"
python3 "${SMOKE_UTILS}" check-agent "${AID}" <<<"${AGENT_JSON}"

echo "==> list agents (search by id; default page is oldest-first)"
AGENT_LIST="$(api_get "/v1/agents?q=${AID}")"
python3 "${SMOKE_UTILS}" check-agents-list "${AID}" <<<"${AGENT_LIST}"

echo "==> patch agent description"
api_patch_json "/v1/agents/${AID}" \
  '{"description":"smoke-test.sh patched"}' >/dev/null
echo "agent patched"

echo "==> create session (environment_id=${SMOKE_ENV_ID})"
SESSION_JSON="$(
  api_post_json "/v1/sessions" \
    "$(python3 "${SMOKE_UTILS}" make-session-body "${AID}" "${SMOKE_ENV_ID}")"
)"
SID="$(echo "${SESSION_JSON}" | json_field id)"
SESSION_ENV_ID="$(python3 "${SMOKE_UTILS}" check-session-env "${SMOKE_ENV_ID}" <<<"${SESSION_JSON}")"
echo "SID=${SID} environment_id=${SESSION_ENV_ID}"

echo "==> get session"
api_get "/v1/sessions/${SID}" >/dev/null
echo "session ok"

echo "==> list sessions (search by id; default page is oldest-first)"
SESSION_LIST="$(api_get "/v1/sessions?q=${SID}")"
python3 "${SMOKE_UTILS}" check-sessions-list "${SID}" <<<"${SESSION_LIST}"

echo "==> session aux: threads / pending / trajectory / outputs"
api_get "/v1/sessions/${SID}/threads" >/dev/null
api_get "/v1/sessions/${SID}/pending" >/dev/null
TRAJ_JSON="$(api_get "/v1/sessions/${SID}/trajectory")"
assert_trajectory_shape "trajectory(empty)" 0 <<<"${TRAJ_JSON}"
api_get "/v1/sessions/${SID}/outputs" >/dev/null
echo "session aux ok"

echo "==> list session files (scope_id=${SID})"
FILES_JSON="$(api_get "/v1/files?scope_id=${SID}")"
python3 "${SMOKE_UTILS}" check-files <<<"${FILES_JSON}"

echo "==> list session events (AMA wire shape)"
EVENTS_EMPTY="$(api_get "/v1/sessions/${SID}/events?order=asc")"
assert_events_ama_shape "events(empty)" <<<"${EVENTS_EMPTY}"

if [[ "${SMOKE_SKIP_LLM}" == "1" ]]; then
  echo "smoke test passed (P1+P2 APIs only, SMOKE_SKIP_LLM=1)"
  print_leftover_resources
  exit 0
fi

if [[ "${SMOKE_TOOLS_ONLY}" != "1" ]]; then
  echo "==> send message (basic LLM)"
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
    python3 "${SMOKE_UTILS}" extract-reply-text <<<"${EVENTS}"
  )"

  echo "AGENT_REPLY=${REPLY_TEXT}"

  echo "==> verify post-LLM events + trajectory"
  EVENTS_RAW="$(api_get "/v1/sessions/${SID}/events?order=asc")"
  assert_events_ama_shape "events(after_llm)" <<<"${EVENTS_RAW}"
  TRAJ_AFTER="$(api_get "/v1/sessions/${SID}/trajectory")"
  assert_trajectory_shape "trajectory(after_llm)" 1 <<<"${TRAJ_AFTER}"
fi

if [[ "${SMOKE_SKIP_TOOLS}" == "1" ]]; then
  echo "smoke test passed (P1+P2 APIs + LLM only, SMOKE_SKIP_TOOLS=1)"
  print_leftover_resources
  exit 0
fi

UNAME_LINE="$(uname -a)"
if _e2e_is_local_target; then
  echo "==> tool chain smoke: bash + uname (local=${UNAME_LINE})"
else
  echo "==> tool chain smoke: bash + uname (remote harness; local=${UNAME_LINE})"
fi

echo "==> send message (Run: uname -a)"
TOOL_RESP="$(
  api_post_json "/v1/sessions/${SID}/events" \
    '{"events":[{"type":"user.message","content":[{"type":"text","text":"Run: uname -a"}]}]}'
)"
echo "${TOOL_RESP}"

echo "==> wait for bash tool_use + uname output (timeout=${SMOKE_TOOL_TIMEOUT_SEC}s)"
TOOL_EVENTS="$(wait_for_bash_uname_chain "${SID}")"
echo "${TOOL_EVENTS}"
echo ""

TOOL_SUMMARY="$(
  python3 "${SMOKE_UTILS}" extract-tool-summary <<<"${TOOL_EVENTS}"
)"

echo "${TOOL_SUMMARY}"

echo "==> verify post-tool events + trajectory"
TOOL_EVENTS_RAW="$(api_get "/v1/sessions/${SID}/events?order=asc")"
assert_events_ama_shape "events(after_tools)" <<<"${TOOL_EVENTS_RAW}"
TRAJ_FINAL="$(api_get "/v1/sessions/${SID}/trajectory")"
assert_trajectory_shape "trajectory(after_tools)" 2 <<<"${TRAJ_FINAL}"

echo "==> refresh /v1/stats after turns"
STATS_AFTER="$(api_get "/v1/stats")"
python3 "${SMOKE_UTILS}" check-stats-after <<<"${STATS_AFTER}"

if [[ "${SMOKE_SKIP_P2}" == "1" ]]; then
  echo "smoke test passed (P1 APIs + real LLM + bash/uname tool chain)"
else
  echo "smoke test passed (P1+P2 APIs + real LLM + bash/uname tool chain)"
fi
print_leftover_resources
