#!/usr/bin/env bash
# E2E: Sandbox Phase A — memory mounts, promoteSandboxFile, workdir lifecycle.
#
# Runs:
#   - Go workdir path normalize + memory symlink + Remove
#   - Go harness memory materialize
#   - Go API promoteSandboxFile + session DELETE workdir cleanup
#   - Python harness sandbox_paths + resource_mounter (memory no-op)
#
# Mirrors open-managed-agents/apps/main-node/test/promote-sandbox-file.test.ts
# at the unit/smoke layer (no /exec on oma-platform yet).
#
# Usage:
#   ./scripts/e2e/smoke-promote-sandbox-e2e.sh
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
source "${ROOT_DIR}/scripts/go-env.sh"

log() {
  echo "[promote-sandbox] $*"
}

log "Go workdir sandbox paths + memory mounts"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/workdir/... \
  -run 'Normalize|Resolve|EnsureMounts|Remove|Backup' -count=1 -v

log "Go workspace backup repo"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/store/... \
  -run 'WorkspaceBackup' -count=1 -v

log "Go harness memory materialize"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/harness/... \
  -run 'Materialize' -count=1 -v

log "Go API promoteSandboxFile + session DELETE workdir"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/api/... \
  -run 'PromoteSandbox|SessionDelete|SessionDeleteSnapshots' -count=1 -v

log "Python harness sandbox paths + resource mounter"
(
  cd harness
  if command -v uv >/dev/null 2>&1; then
    uv run pytest tests/test_sandbox_paths.py tests/test_resource_mounter.py -v
  else
    python3 -m pytest tests/test_sandbox_paths.py tests/test_resource_mounter.py -v
  fi
)

log "PASS: sandbox Phase A smoke completed"
