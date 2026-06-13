#!/usr/bin/env bash
# End-to-end verification for sub-agent delegation wiring.
#
# Runs Go integration tests that exercise:
#   create worker + coordinator (callable_agents) → session → turn →
#   harness SubAgents resolution → delegation events → threads + trajectory APIs
#
# Usage:
#   ./scripts/smoke-subagent-e2e.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

log() {
  echo "[subagent-smoke] $*"
}

log "Go sub-agent E2E (platform API + harness sim client)"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/harness/... ./internal/api/... \
  -run 'SubAgent|E2ESubAgent' -count=1 -v

log "Python harness sub-agent E2E"
(
  cd harness
  if command -v uv >/dev/null 2>&1; then
    uv run pytest tests/test_subagent_e2e.py tests/test_call_agent.py -v
    log "Python real harness (pi session + faux call_agent tool call)"
    uv run pytest tests/test_subagent_live_harness.py -v
  else
    python3 -m pytest tests/test_subagent_e2e.py tests/test_call_agent.py -v
    echo "skip test_subagent_live_harness.py (uv not installed; use uv for pi harness)" >&2
  fi
)

log "PASS: sub-agent E2E checks completed"
