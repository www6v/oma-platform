#!/usr/bin/env bash
# Live team smoke — real harness (OMA_FAKE_HARNESS=0) + real LLM via eval T13.
#
# Thin wrapper around scripts/multi-agent/run.sh. Default runs T13.1 only
# (team_create + spawn_teammate). Set TEAM_LIVE_FULL=1 to also run T13.2
# (send_team_message mailbox collaboration).
#
# Prerequisites (same as smoke-subagent-live-e2e.sh):
#   - oma-server on PLATFORM_URL with OMA_FAKE_HARNESS=0
#   - harness sidecar on HARNESS_URL
#   - LLM credentials for piPy
#
# Usage:
#   ./scripts/multi-agent/smoke-team-live-e2e.sh
#   TEAM_LIVE_FULL=1 ./scripts/multi-agent/smoke-team-live-e2e.sh
#   SMOKE_MODEL=claude-sonnet-4-6 ./scripts/multi-agent/smoke-team-live-e2e.sh
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

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-0}"
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export OMA_MODEL="${OMA_MODEL:-${SMOKE_MODEL}}"

if [[ "${TEAM_LIVE_FULL:-0}" == "1" ]]; then
  EVAL_TASKS="T13.1-team-spawn,T13.2-team-send-message"
else
  EVAL_TASKS="T13.1-team-spawn"
fi

log() {
  echo "[team-live] $*"
}

if [[ "${OMA_FAKE_HARNESS}" == "1" ]]; then
  echo "error: OMA_FAKE_HARNESS=1 skips real harness — set OMA_FAKE_HARNESS=0" >&2
  exit 1
fi

log "preflight: platform + harness"
curl -sf "${PLATFORM_URL}/health" >/dev/null
curl -sf "${HARNESS_URL}/health" >/dev/null
log "platform + harness ok"

log "running eval tasks: ${EVAL_TASKS}"
"${ROOT_DIR}/scripts/multi-agent/run.sh" \
  --suite multi-agent \
  --task "${EVAL_TASKS}"

log "PASS: live team eval smoke completed (${EVAL_TASKS})"
