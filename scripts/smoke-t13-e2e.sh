#!/usr/bin/env bash
# T13 end-to-end: resource mounter + outcome evaluator (P2-3 / P2-4).
#
# Runs:
#   - Go resource resolver unit test
#   - Go eval worker rubric integration tests
#   - Go API eval worker rubric integration test
#   - Python resource_mounter + outcome_evaluator tests
#   - Python faux in-process harness turn (bash reads mount; no platform)
#
# For real platform agent/session + Console UI, run:
#   ./scripts/smoke-resource-live-e2e.sh
#
# Usage:
#   ./scripts/smoke-t13-e2e.sh
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

log() {
  echo "[t13-smoke] $*"
}

log "Go resource resolver"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/harness/... \
  -run 'TestResourceResolver' -count=1 -v

log "Go eval worker rubric integration"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/eval/... \
  -run 'TestWorkerRubric|TestAgentOutput' -count=1 -v

log "Go API eval worker rubric integration"
GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/api/... \
  -run 'TestEvalWorkerRubric' -count=1 -v

log "Python resource mounter + outcome evaluator"
(
  cd harness
  if command -v uv >/dev/null 2>&1; then
    uv run pytest tests/test_resource_mounter.py tests/test_outcome_evaluator.py -v
    log "Python resource mounter faux harness (in-process pi turn)"
    uv run pytest tests/test_resource_live_harness.py::test_resource_mounter_faux_harness_turn -v
  else
    python3 -m pytest tests/test_resource_mounter.py tests/test_outcome_evaluator.py -v
    echo "skip test_resource_live_harness.py (uv not installed; use uv for pi harness)" >&2
  fi
)

log "PASS: T13 resource mounter + outcome evaluator checks completed"
