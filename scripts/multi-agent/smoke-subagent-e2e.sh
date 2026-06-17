#!/usr/bin/env bash
# End-to-end verification for sub-agent delegation wiring.
#
# Runs Go integration tests that exercise:
#   create worker + coordinator (callable_agents) → session → turn →
#   harness SubAgents resolution → delegation events → threads + trajectory APIs
#
# Usage:
#   ./scripts/multi-agent/smoke-subagent-e2e.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

log() {
  echo "[subagent-smoke] $*"
}

log "Go sub-agent E2E (platform API + harness sim client)"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/harness/... ./internal/api/... \
  -run 'SubAgent|E2ESubAgent' -count=1 -v

log "Python harness sub-agent wiring"
(
  cd harness
  if command -v uv >/dev/null 2>&1; then
    uv run pytest tests/test_subagent_tools.py tests/test_subagent_extension_path.py -v
  else
    python3 -m pytest tests/test_subagent_tools.py tests/test_subagent_extension_path.py -v
  fi
)

PIPY_SUBAGENT_DIR="${ROOT_DIR}/../piPy-subagent"
log "Python pi-subagent delegate E2E (${PIPY_SUBAGENT_DIR})"
(
  cd "${PIPY_SUBAGENT_DIR}"
  if command -v uv >/dev/null 2>&1; then
    uv run pytest \
      packages/pi_subagent/tests/test_subagent_e2e.py \
      packages/pi_subagent/tests/test_call_agent.py \
      -v
  else
    python3 -m pytest \
      packages/pi_subagent/tests/test_subagent_e2e.py \
      packages/pi_subagent/tests/test_call_agent.py \
      -v
  fi
)

log "PASS: sub-agent E2E checks completed"
