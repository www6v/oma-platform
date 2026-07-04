#!/usr/bin/env bash
# OG5 live soak — EV fast-charging outcome grader cookbook.
#
# Requires live oma-server + harness (real LLM, web_search/web_fetch).
# Not part of default CI — run manually or in a soak workflow.
#
# Usage:
#   OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
#     ./scripts/e2e/smoke-outcome-ev-charging-live-e2e.sh
#
# Optional:
#   OMA_USE_INLINE_RUBRIC=1   notebook-style inline rubric
#   OMA_EV_STRICT=1           require terminal result satisfied
#   OMA_KEEP_RESOURCES=1      skip archive
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "${ROOT_DIR}"

log() {
  echo "[outcome-ev-live] $*"
}

if [[ -z "${OMA_API_KEY:-}" ]]; then
  echo "OMA_API_KEY is required" >&2
  exit 1
fi

log "fixture smoke (pytest, no LLM)"
(
  cd sdk
  python3 -m pytest tests/test_outcome_cookbook.py -k "fixture" -v
)

log "live EV charging outcome soak"
python3 sdk/example/example4/outcome_grader_ev_charging.py

log "PASS: OG5 live soak completed"
