#!/usr/bin/env bash
# Run all scripts/e2e smoke cases as one test suite.
#
# Suites:
#   core     — platform API, console contracts, internal API, schedules, resources, MCP, live harness
#   noncore  — integrations, dreaming, runtime, browser console
#   all      — core then noncore (default)
#
# Usage:
#   ./scripts/e2e/smoke-all.sh
#   ./scripts/e2e/smoke-all.sh core
#   ./scripts/e2e/smoke-all.sh noncore
#   SMOKE_FAIL_FAST=1 ./scripts/e2e/smoke-all.sh
#   SMOKE_SKIP_BROWSER=1 ./scripts/e2e/smoke-all.sh noncore
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
E2E_DIR="${ROOT_DIR}/scripts/e2e"
MA_DIR="${ROOT_DIR}/scripts/multi-agent"

_E2E_ANTHROPIC_KEY_BEFORE_ENV="${ANTHROPIC_API_KEY:-}"
if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi
if [[ -z "${ANTHROPIC_API_KEY:-}" && -n "${_E2E_ANTHROPIC_KEY_BEFORE_ENV}" ]]; then
  export ANTHROPIC_API_KEY="${_E2E_ANTHROPIC_KEY_BEFORE_ENV}"
fi
unset _E2E_ANTHROPIC_KEY_BEFORE_ENV

# shellcheck disable=SC1091
source "${E2E_DIR}/common.sh"

SUITE="${1:-all}"
PASSED=0
FAILED=0
SKIPPED=0
declare -a FAILED_CASES=()
declare -a SKIPPED_CASES=()

usage() {
  cat <<EOF
usage: $0 [core|noncore|all]

  core     run system core smoke cases
  noncore  run optional / integration smoke cases
  all      run core then noncore (default)

env:
  SMOKE_FAIL_FAST=1      stop on first failure
  SMOKE_SKIP_BROWSER=1   skip Playwright / browser console cases
  SMOKE_SKIP_LLM=1       passed to smoke-test.sh (API-only platform check)
  target.env             optional remote endpoints (see target.env.example)
EOF
}

is_local_target() {
  case "${PLATFORM_URL}" in
    *127.0.0.1*|*localhost*) return 0 ;;
    *) return 1 ;;
  esac
}

log_suite() {
  echo ""
  echo "============================================================"
  echo "  $*"
  echo "============================================================"
}

run_case() {
  local name="$1"
  shift

  echo ""
  echo "------------------------------------------------------------"
  echo "▶ ${name}"
  echo "------------------------------------------------------------"

  if "$@"; then
    PASSED=$((PASSED + 1))
    echo "✓ ${name}"
    return 0
  fi

  FAILED=$((FAILED + 1))
  FAILED_CASES+=("${name}")
  echo "✗ ${name}" >&2
  if [[ "${SMOKE_FAIL_FAST:-0}" == "1" ]]; then
    return 1
  fi
  return 0
}

skip_case() {
  local name="$1"
  local reason="$2"
  SKIPPED=$((SKIPPED + 1))
  SKIPPED_CASES+=("${name}: ${reason}")
  echo ""
  echo "------------------------------------------------------------"
  echo "⊘ SKIP ${name} — ${reason}"
  echo "------------------------------------------------------------"
}

run_shell() {
  local script="$1"
  bash "${E2E_DIR}/${script}"
}

run_multi_agent_shell() {
  local script="$1"
  bash "${MA_DIR}/${script}"
}

run_with_target() {
  bash "${E2E_DIR}/with-target.sh" "$@"
}

run_core_suite() {
  log_suite "CORE — system core functionality"

  run_case "resource mounter + outcome evaluator" \
    run_shell smoke-resource-outcome-e2e.sh

  run_case "sub-agent delegation (sim + unit)" \
    run_multi_agent_shell smoke-subagent-e2e.sh

  run_case "team store + harness tools" \
    run_multi_agent_shell smoke-team-e2e.sh

  run_case "internal model card key" \
    run_shell smoke-internal-model-key.sh

  run_case "internal API" \
    run_shell smoke-internal-api-e2e.sh

  run_case "session wakeup schedules" \
    run_shell smoke-schedule-e2e.sh

  run_case "console API integration" \
    run_shell console-integration.sh

  run_case "platform smoke (agents, sessions, tools)" \
    run_shell smoke-test.sh

  _e2e_ensure_model_card

  if is_local_target; then
    run_case "MCP proxy + harness tools" \
      run_shell smoke-mcp-e2e.sh
  else
    skip_case "MCP proxy + harness tools" \
      "mock MCP binds 127.0.0.1; remote ${PLATFORM_URL} cannot reach it"
  fi

  run_case "sub-agent live harness" \
    run_multi_agent_shell smoke-subagent-live-e2e.sh

  run_case "team live harness (eval T13)" \
    run_multi_agent_shell smoke-team-live-e2e.sh

  run_case "web_search tool (API)" \
    run_shell smoke-web-search-e2e.sh
}

run_noncore_suite() {
  log_suite "NON-CORE — integrations and extended features"

  _e2e_ensure_model_card

  run_case "outbound proxy (no LLM)" \
    run_shell smoke-outbound-proxy.sh

  # run_case "GitHub webhook dispatch" \
  #   run_shell smoke-github-webhook.sh

  # run_case "Linear webhook dispatch" \
  #   run_shell smoke-linear-webhook.sh

  # run_case "Slack webhook dispatch" \
  #   run_shell smoke-slack-webhook.sh

  run_case "outbound vault + harness curl" \
    run_shell smoke-outbound-e2e.sh

  run_case "dreaming API + cost_report" \
    run_shell smoke-dreams-e2e.sh

  run_case "local ACP runtime" \
    bash -c "RUNTIME_ACP=\"\${RUNTIME_ACP:-0}\" \"${E2E_DIR}/smoke-runtime-e2e.sh\""

  run_case "resource mount live harness" \
    run_shell smoke-resource-live-e2e.sh

  if [[ "${SMOKE_SKIP_BROWSER:-0}" == "1" ]]; then
    skip_case "web_search eval client" "SMOKE_SKIP_BROWSER=1"
    skip_case "console UI check" "SMOKE_SKIP_BROWSER=1"
    skip_case "console dogfood" "SMOKE_SKIP_BROWSER=1"
    skip_case "console comprehensive E2E" "SMOKE_SKIP_BROWSER=1"
    skip_case "team console E2E" "SMOKE_SKIP_BROWSER=1"
  else
    _e2e_ensure_model_card

    if [[ ! -f "${ROOT_DIR}/console/dist/index.html" ]]; then
      echo "==> building console dist for browser smoke tests"
      "${ROOT_DIR}/scripts/build-console.sh"
    fi

    if [[ ! -d "${E2E_DIR}/node_modules/@playwright/test" ]]; then
      echo "==> installing scripts/e2e npm deps"
      (cd "${E2E_DIR}" && npm install --no-fund --no-audit)
    fi

    run_case "web_search eval client" \
      run_with_target npx --yes tsx "${E2E_DIR}/web-search-smoke.ts"

    run_case "console UI check" \
      run_with_target node "${E2E_DIR}/console-ui-check.mjs"

    run_case "console dogfood" \
      run_with_target node "${E2E_DIR}/console-dogfood.mjs"

    run_case "console comprehensive E2E" \
      run_with_target node "${E2E_DIR}/console-comprehensive-e2e.mjs"

    run_case "team console E2E" \
      run_multi_agent_shell smoke-team-console-e2e.sh
  fi

  if is_local_target; then
    run_case "console auth (ephemeral QA stack)" \
      run_shell run-console-qa-auth.sh
  else
    skip_case "console auth (ephemeral QA stack)" \
      "remote target (${PLATFORM_URL}); needs local ephemeral stack"
  fi
}

print_summary() {
  local total=$((PASSED + FAILED + SKIPPED))
  echo ""
  echo "============================================================"
  echo "  SMOKE SUITE SUMMARY (${SUITE})"
  echo "  target: platform=${PLATFORM_URL} harness=${HARNESS_URL}"
  echo "============================================================"
  echo "  total:   ${total}"
  echo "  passed:  ${PASSED}"
  echo "  failed:  ${FAILED}"
  echo "  skipped: ${SKIPPED}"

  if ((${#SKIPPED_CASES[@]} > 0)); then
    echo ""
    echo "  skipped cases:"
    local item
    for item in "${SKIPPED_CASES[@]}"; do
      echo "    - ${item}"
    done
  fi

  if ((${#FAILED_CASES[@]} > 0)); then
    echo ""
    echo "  failed cases:"
    local item
    for item in "${FAILED_CASES[@]}"; do
      echo "    - ${item}"
    done
    echo ""
    return 1
  fi

  echo ""
  echo "  all executed cases passed"
  echo ""
  return 0
}

case "${SUITE}" in
  core)
    run_core_suite
    ;;
  noncore)
    run_noncore_suite
    ;;
  all)
    run_core_suite
    run_noncore_suite
    ;;
  -h|--help|help)
    usage
    exit 0
    ;;
  *)
    echo "error: unknown suite '${SUITE}'" >&2
    usage >&2
    exit 1
    ;;
esac

print_summary
