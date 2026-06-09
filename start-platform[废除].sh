#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/go-env.sh"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

export OMA_FAKE_HARNESS="${OMA_FAKE_HARNESS:-1}"
export HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}"
export OMA_API_KEY="${OMA_API_KEY:-dev-key}"
export DATABASE_PATH="${DATABASE_PATH:-${ROOT_DIR}/data/oma.db}"
export SANDBOX_WORKDIR="${SANDBOX_WORKDIR:-${ROOT_DIR}/data/sandboxes}"
export OMA_LISTEN_ADDR="${OMA_LISTEN_ADDR:-:8787}"

mkdir -p "$(dirname "${DATABASE_PATH}")" "${SANDBOX_WORKDIR}"

cd "${ROOT_DIR}"

exec "${GO_BIN}" run ./cmd/oma-server/
