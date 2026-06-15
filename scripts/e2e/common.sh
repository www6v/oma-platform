#!/usr/bin/env bash
# Shared E2E endpoint configuration.
# Source after project .env. Optional override: scripts/e2e/target.env
#
# Defaults target local oma-server (:8787) and harness (:8090).
# Remote example:
#   cp scripts/e2e/target.env.example scripts/e2e/target.env
#   # edit URLs, then run smoke scripts as usual

_e2e_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

if [[ -f "${_e2e_dir}/target.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${_e2e_dir}/target.env"
  set +a
fi

_e2e_strip_slash() {
  local u="$1"
  while [[ "${u}" == */ && -n "${u}" ]]; do
    u="${u%/}"
  done
  printf '%s' "${u}"
}

_e2e_default_platform() {
  local listen="${OMA_LISTEN_ADDR:-:8787}"
  if [[ "${listen}" == :* ]]; then
    printf 'http://127.0.0.1%s' "${listen}"
  else
    printf 'http://%s' "${listen}"
  fi
}

if [[ -z "${PLATFORM_URL:-}" ]]; then
  if [[ -n "${OMA_BASE_URL:-}" ]]; then
    PLATFORM_URL="${OMA_BASE_URL}"
  elif [[ -n "${OMA_API_URL:-}" ]]; then
    PLATFORM_URL="${OMA_API_URL}"
  else
    PLATFORM_URL="$(_e2e_default_platform)"
  fi
fi

PLATFORM_URL="$(_e2e_strip_slash "${PLATFORM_URL}")"
HARNESS_URL="$(_e2e_strip_slash "${HARNESS_URL:-http://127.0.0.1:8090}")"

export PLATFORM_URL
export OMA_BASE_URL="$(_e2e_strip_slash "${OMA_BASE_URL:-${PLATFORM_URL}}")"
export OMA_API_URL="$(_e2e_strip_slash "${OMA_API_URL:-${PLATFORM_URL}}")"
export CONSOLE_URL="$(_e2e_strip_slash "${CONSOLE_URL:-${PLATFORM_URL}}")"
export OMA_BASE="${OMA_BASE:-${PLATFORM_URL}}"
export HARNESS_URL
