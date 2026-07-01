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
export HARNESS_TURN_TIMEOUT_SEC="${HARNESS_TURN_TIMEOUT_SEC:-900}"

export OMA_DATABASE_PATH="${OMA_DATABASE_PATH:-${DATABASE_PATH:-${ROOT_DIR}/data/oma.db}}"

mkdir -p "$(dirname "${OMA_DATABASE_PATH}")"

# Shell HTTP(S)_PROXY breaks piPy LLM clients (empty assistant + connection errors).
# Sandbox outbound uses per-turn .curlrc instead (see outbound/setup.py).
unset HTTP_PROXY HTTPS_PROXY http_proxy https_proxy ALL_PROXY all_proxy || true
export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,::1}"
export no_proxy="${no_proxy:-$NO_PROXY}"

cd "${ROOT_DIR}/harness"

# Kill any process occupying port 8090
lsof -ti:8090 | xargs kill -9 2>/dev/null || true

if [[ ! -x "${ROOT_DIR}/harness/.venv/bin/uvicorn" ]]; then
  if ! command -v uv >/dev/null 2>&1; then
    echo "error: uv is required to install harness dependencies" >&2
    echo "install uv, then rerun ./start-harness.sh" >&2
    exit 1
  fi
  uv sync
fi

# Optional: pre-install common cookbook deps (E1 still installs from env.config at turn time).
if [[ "${OMA_COOKBOOK_PACKAGES:-0}" == "1" ]]; then
  echo "Installing cookbook packages (pandas, plotly) into harness venv..."
  if ! command -v uv >/dev/null 2>&1; then
    echo "error: uv is required for OMA_COOKBOOK_PACKAGES=1" >&2
    exit 1
  fi
  uv pip install --python "${ROOT_DIR}/harness/.venv/bin/python" pandas plotly
fi

exec "${ROOT_DIR}/harness/.venv/bin/uvicorn" oma_adapter.main:app \
  --host 0.0.0.0 \
  --port 8090
