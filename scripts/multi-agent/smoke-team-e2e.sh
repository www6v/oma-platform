#!/usr/bin/env bash
# Smoke: team store + internal API + pi_team harness tools.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

log() {
  echo "[team-smoke] $*"
}

log "Go team store + API tests"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/store/... ./internal/api/... \
  -run 'Team' -count=1 -v

log "Python pi_team tools"
(
  cd harness
  if command -v uv >/dev/null 2>&1; then
    uv run pytest tests/test_team_tools.py tests/test_team_tenant_isolation.py tests/test_team_broadcast.py tests/test_team_shutdown.py -v
  else
    python3 -m pytest tests/test_team_tools.py tests/test_team_tenant_isolation.py tests/test_team_broadcast.py tests/test_team_shutdown.py -v
  fi
)

log "Optional: Console Team tab Playwright E2E"
log "  ./scripts/multi-agent/smoke-team-console-e2e.sh"
log "Optional: live harness + LLM (eval T13 wrapper)"
log "  ./scripts/multi-agent/smoke-team-live-e2e.sh"

log "PASS: team smoke checks completed"
