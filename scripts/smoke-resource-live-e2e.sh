#!/usr/bin/env bash
# Live end-to-end: real harness (OMA_FAKE_HARNESS=0) + mounted file/env resources.
#
# Creates a real agent and session on oma-server (visible in Console) and verifies
# the harness turn reads mounted resources via bash.
#
# Prerequisites:
#   - oma-server on OMA_LISTEN_ADDR (default :8787) with OMA_FAKE_HARNESS=0
#   - harness sidecar on HARNESS_URL (default :8090)
#   - LLM credentials for piPy (see README / ~/.pi/agent/auth.json)
#
# Usage:
#   ./scripts/smoke-resource-live-e2e.sh
#   SMOKE_MODEL=claude-sonnet-4-6 ./scripts/smoke-resource-live-e2e.sh
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
export OMA_FAKE_HARNESS=0
export SMOKE_MODEL="${SMOKE_MODEL:-claude-sonnet-4-6}"
export SMOKE_TOOL_TIMEOUT_SEC="${SMOKE_TOOL_TIMEOUT_SEC:-180}"
export SMOKE_POLL_SEC="${SMOKE_POLL_SEC:-2}"
export HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}"
export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,::1,*}"
export no_proxy="${no_proxy:-$NO_PROXY}"
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy ALL_PROXY all_proxy || true

log() {
  echo "[resource-live] $*"
}

if [[ "${OMA_FAKE_HARNESS}" == "1" ]]; then
  echo "error: OMA_FAKE_HARNESS=1 skips real harness — set OMA_FAKE_HARNESS=0" >&2
  exit 1
fi

log "run platform live pytest (real agent + session in Console)"
(
  cd harness
  if command -v uv >/dev/null 2>&1; then
    uv run pytest tests/test_resource_live_harness.py::test_resource_mounter_platform_live_harness -m live -s -v
  else
    python3 -m pytest tests/test_resource_live_harness.py::test_resource_mounter_platform_live_harness -m live -s -v
  fi
)

log "PASS: resource mount live harness smoke completed"
