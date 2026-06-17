#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

if [[ -f "${PROJECT_ROOT}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${PROJECT_ROOT}/.env"
  set +a
fi

# Defaults (align with scripts/e2e/smoke-*.sh)
: "${OMA_API_URL:="http://localhost:8787"}"
: "${OMA_API_KEY:=dev-key}"
: "${OMA_MODEL:="${SMOKE_MODEL:-claude-sonnet-4-6}"}"
: "${SMOKE_MODEL:="${OMA_MODEL}"}"

export OMA_API_URL OMA_API_KEY OMA_MODEL SMOKE_MODEL

# Ensure a default model card exists when ANTHROPIC_API_KEY is set.
# shellcheck disable=SC1091
source "${PROJECT_ROOT}/scripts/e2e/common.sh"
_e2e_ensure_model_card || true

echo "OMA Eval Runner"
echo "  API URL: $OMA_API_URL"
echo "  API Key: ${OMA_API_KEY:0:8}..."
echo ""

cd "$PROJECT_ROOT"
exec npx tsx --tsconfig test/eval/tsconfig.json test/eval/runner.ts "$@"
