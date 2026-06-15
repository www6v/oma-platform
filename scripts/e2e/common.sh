#!/usr/bin/env bash
# Shared E2E endpoint configuration.
# Source after project .env. Optional override: scripts/e2e/target.env
#
# Defaults target local oma-server (:8787) and harness (:8090).
# Remote example:
#   cp scripts/e2e/target.env.example scripts/e2e/target.env
#   # edit URLs, then run smoke scripts as usual

_e2e_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "${_e2e_dir}/target.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${_e2e_dir}/target.env"
  set +a
fi

_e2e_strip_slash() {
  local u="$1"
  while [[ "${u}" == */ && -n "${u}" ]]; do
    u="${u%/}"
  done
  printf '%s' "${u}"
}

_e2e_default_platform() {
  local listen="${OMA_LISTEN_ADDR:-:8787}"
  if [[ "${listen}" == :* ]]; then
    printf 'http://127.0.0.1%s' "${listen}"
  else
    printf 'http://%s' "${listen}"
  fi
}

if [[ -z "${PLATFORM_URL:-}" ]]; then
  if [[ -n "${OMA_BASE_URL:-}" ]]; then
    PLATFORM_URL="${OMA_BASE_URL}"
  elif [[ -n "${OMA_API_URL:-}" ]]; then
    PLATFORM_URL="${OMA_API_URL}"
  else
    PLATFORM_URL="$(_e2e_default_platform)"
  fi
fi

PLATFORM_URL="$(_e2e_strip_slash "${PLATFORM_URL}")"
HARNESS_URL="$(_e2e_strip_slash "${HARNESS_URL:-http://127.0.0.1:8090}")"

export PLATFORM_URL
export OMA_BASE_URL="$(_e2e_strip_slash "${OMA_BASE_URL:-${PLATFORM_URL}}")"
export OMA_API_URL="$(_e2e_strip_slash "${OMA_API_URL:-${PLATFORM_URL}}")"
export CONSOLE_URL="$(_e2e_strip_slash "${CONSOLE_URL:-${PLATFORM_URL}}")"
export OMA_BASE="${OMA_BASE:-${PLATFORM_URL}}"
export HARNESS_URL

# Local E2E: harness MCP/schedule callbacks must hit the same stack as PLATFORM_URL,
# not a remote PUBLIC_BASE_URL baked into oma-server at startup.
if [[ "${PLATFORM_URL}" == http://127.0.0.1:* || "${PLATFORM_URL}" == http://localhost:* ]]; then
  export OMA_HARNESS_PLATFORM_BASE="${OMA_HARNESS_PLATFORM_BASE:-${PLATFORM_URL}}"
fi

_e2e_setup_go_toolchain() {
  if command -v go >/dev/null 2>&1; then
    return 0
  fi
  local go_env="${_e2e_dir}/../go-env.sh"
  if [[ -f "${go_env}" ]]; then
    # shellcheck disable=SC1091
    source "${go_env}"
  fi
}

_e2e_setup_go_toolchain

_e2e_is_local_target() {
  case "${PLATFORM_URL}" in
    *127.0.0.1*|*localhost*) return 0 ;;
    *) return 1 ;;
  esac
}

_e2e_outbound_host() {
  local addr="${OMA_OUTBOUND_PROXY_ADDR:-:8790}"
  if [[ "${addr}" == :* ]]; then
    printf '127.0.0.1%s' "${addr}"
  else
    printf '%s' "${addr}"
  fi
}

# Outbound smokes start mock upstream on 127.0.0.1; platform API, outbound
# proxy (:8790), and harness must share that same local oma-server DB.
_e2e_pin_local_outbound_stack() {
  local local_platform="${OUTBOUND_SMOKE_PLATFORM_URL:-$(_e2e_default_platform)}"
  local_platform="$(_e2e_strip_slash "${local_platform}")"
  local local_harness="${OUTBOUND_SMOKE_HARNESS_URL:-http://127.0.0.1:8090}"
  local_harness="$(_e2e_strip_slash "${local_harness}")"

  if _e2e_is_local_target; then
    return 0
  fi

  if ! curl -sf "${local_platform}/health" >/dev/null 2>&1; then
    echo "OUTBOUND smoke requires local oma-server at ${local_platform}" >&2
    echo "  (mock upstream binds 127.0.0.1; remote ${PLATFORM_URL} cannot use it)" >&2
    exit 1
  fi

  if ! curl -sf "${local_harness}/health" >/dev/null 2>&1; then
    echo "OUTBOUND smoke requires local harness at ${local_harness}" >&2
    exit 1
  fi

  echo "==> outbound smoke: local stack ${local_platform} (ignoring remote target.env)" >&2
  export PLATFORM_URL="${local_platform}"
  export OMA_BASE_URL="${local_platform}"
  export OMA_API_URL="${local_platform}"
  export CONSOLE_URL="${local_platform}"
  export HARNESS_URL="${local_harness}"
  export OMA_HARNESS_PLATFORM_BASE="${local_platform}"
}

_e2e_start_outbound_mock() {
  local root_dir="$1"
  local port="${OUTBOUND_MOCK_PORT:-9888}"
  if command -v lsof >/dev/null 2>&1; then
    local stale=""
    stale="$(lsof -ti:"${port}" 2>/dev/null || true)"
    if [[ -n "${stale}" ]]; then
      echo "==> stopping stale mock on port ${port}" >&2
      kill ${stale} 2>/dev/null || true
      sleep 0.3
    fi
  fi
  OUTBOUND_MOCK_PORT="${port}" python3 "${root_dir}/scripts/e2e/mock-outbound-server.py" &
  MOCK_PID=$!
  sleep 0.5
}

# Ensure a default model card exists for console UI tests (needs ANTHROPIC_API_KEY).
_e2e_ensure_model_card() {
  if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
    return 0
  fi

  local card_id="${SMOKE_MODEL_CARD_ID:-smoke-claude}"
  local model="${SMOKE_MODEL:-claude-sonnet-4-6}"
  local api_key="${OMA_API_KEY:-dev-key}"
  local card_body row_id http_code card_list

  card_body="$(
    python3 -c 'import json,os,sys
print(json.dumps({
    "model_id": sys.argv[1],
    "model": sys.argv[2],
    "provider": "ant",
    "api_key": os.environ["ANTHROPIC_API_KEY"],
    "is_default": True,
}))' "${card_id}" "${model}"
  )"

  http_code="$(
    curl -s -o /tmp/oma-e2e-card.json -w "%{http_code}" -X POST \
      "${PLATFORM_URL}/v1/model_cards" \
      -H "content-type: application/json" \
      -H "x-api-key: ${api_key}" \
      -d "${card_body}"
  )"

  if [[ "${http_code}" == "201" ]]; then
    return 0
  fi

  if [[ "${http_code}" != "409" ]]; then
    echo "warning: model card ensure failed (HTTP ${http_code})" >&2
    return 0
  fi

  card_list="$(
    curl -sf "${PLATFORM_URL}/v1/model_cards" -H "x-api-key: ${api_key}"
  )"
  row_id="$(
    python3 -c 'import json,sys
target=sys.argv[1]
for row in json.load(sys.stdin).get("data", []):
    if row.get("model_id") == target:
        print(row["id"])
        raise SystemExit(0)
raise SystemExit(1)' "${card_id}" <<<"${card_list}"
  )" || return 0

  curl -sf -X POST "${PLATFORM_URL}/v1/model_cards/${row_id}" \
    -H "content-type: application/json" \
    -H "x-api-key: ${api_key}" \
    -d "$(python3 -c 'import json,os,sys; print(json.dumps({
        "model": sys.argv[1],
        "provider": "ant",
        "api_key": os.environ["ANTHROPIC_API_KEY"],
        "is_default": True,
    }))' "${model}")" >/dev/null || true
}
