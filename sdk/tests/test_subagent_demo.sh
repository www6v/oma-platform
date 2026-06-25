#!/bin/bash
# Run live sub-agent delegation demo — keeps resources for Console UI inspection.
#
# Prerequisites:
#   - oma-server running (OMA_FAKE_HARNESS=0, harness sidecar up)
#   - LLM credentials configured for piPy harness
#
# Usage:
#   OMA_API_KEY=dev-key OMA_BASE_URL=http://localhost:8787 ./tests/test_subagent_demo.sh
#
# After the run, open the printed Console links to view:
#   - Agents: sdk-subagent-coordinator, sdk-subagent-worker
#   - Session: "SDK Subagent Demo" with sub-agent thread tabs

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ -z "${OMA_API_KEY:-}" ]]; then
  echo "Error: OMA_API_KEY is required"
  exit 1
fi

export OMA_BASE_URL="${OMA_BASE_URL:-http://localhost:8787}"
export OMA_KEEP_RESOURCES=1
export SUBAGENT_DEMO_TIMEOUT_SEC="${SUBAGENT_DEMO_TIMEOUT_SEC:-180}"

echo "Running live sub-agent demo (resources will be kept):"
echo "  OMA_BASE_URL=${OMA_BASE_URL}"
echo "  SUBAGENT_DEMO_TIMEOUT_SEC=${SUBAGENT_DEMO_TIMEOUT_SEC}"
echo ""

pytest tests/test_subagents.py::test_subagent_live_delegation_visible_in_console -v -s
