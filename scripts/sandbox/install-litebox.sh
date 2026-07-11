#!/usr/bin/env bash
# Install @boxlite-ai/boxlite for the Node bridge (scripts/sandbox/litebox-bridge.mjs).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SANDBOX_DIR="${ROOT_DIR}/scripts/sandbox"

if ! command -v npm >/dev/null 2>&1; then
  echo "error: npm required — install Node.js 18+" >&2
  exit 1
fi

echo "[install-litebox] npm install in ${SANDBOX_DIR}"
(
  cd "${SANDBOX_DIR}"
  npm install --no-fund --no-audit
)

echo "[install-litebox] done"
