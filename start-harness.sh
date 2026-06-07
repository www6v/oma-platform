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

exec uvicorn oma_adapter.main:app \
  --host 0.0.0.0 \
  --port 8090
