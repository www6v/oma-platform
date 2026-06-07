#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-1}"

cd "${ROOT_DIR}/harness"

if [[ ! -x "${ROOT_DIR}/harness/.venv/bin/uvicorn" ]]; then
  if ! command -v uv >/dev/null 2>&1; then
    echo "error: uv is required to install harness dependencies" >&2
    echo "install uv, then rerun ./start-harness.sh" >&2
    exit 1
  fi
  uv sync
fi

exec "${ROOT_DIR}/harness/.venv/bin/uvicorn" oma_adapter.main:app \
  --host 0.0.0.0 \
  --port 8090
