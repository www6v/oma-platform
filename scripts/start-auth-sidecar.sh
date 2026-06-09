#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

export AUTH_LISTEN_ADDR="${AUTH_LISTEN_ADDR:-127.0.0.1:8788}"
export AUTH_DATABASE_PATH="${AUTH_DATABASE_PATH:-${ROOT_DIR}/data/auth.db}"
export OMA_DATABASE_PATH="${OMA_DATABASE_PATH:-${DATABASE_PATH:-${ROOT_DIR}/data/oma.db}}"
export PUBLIC_BASE_URL="${PUBLIC_BASE_URL:-http://127.0.0.1:8787}"

if [[ -z "${BETTER_AUTH_SECRET:-}" ]]; then
  export BETTER_AUTH_SECRET="$(openssl rand -hex 32)"
  echo "Generated ephemeral BETTER_AUTH_SECRET for this session"
fi

cd "${ROOT_DIR}/auth-sidecar"
if [[ ! -d node_modules ]]; then
  echo "Installing auth-sidecar dependencies..."
  npm install --no-fund --no-audit
fi

exec node server.mjs
