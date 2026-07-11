#!/usr/bin/env bash
# Shared LiteBox / BoxLite environment for oma-platform.
# Source from start scripts and smoke tests.
set -euo pipefail

if [[ -z "${OMA_ROOT_DIR:-}" ]]; then
  _litebox_env_self="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
  OMA_ROOT_DIR="$(cd "${_litebox_env_self}/../.." && pwd)"
fi

export SANDBOX_PROVIDER="${SANDBOX_PROVIDER:-litebox}"
export SANDBOX_IMAGE="${SANDBOX_IMAGE:-node:22-slim}"
export LITEBOX_MEMORY_MIB="${LITEBOX_MEMORY_MIB:-512}"
export LITEBOX_CPUS="${LITEBOX_CPUS:-2}"
export NODE_BIN="${NODE_BIN:-node}"
export LITEBOX_BRIDGE_PATH="${LITEBOX_BRIDGE_PATH:-${OMA_ROOT_DIR}/scripts/sandbox/litebox-bridge.mjs}"
export SANDBOX_WORKDIR="${SANDBOX_WORKDIR:-${OMA_ROOT_DIR}/data/sandboxes-litebox}"
export MEMORY_DATA_DIR="${MEMORY_DATA_DIR:-${OMA_ROOT_DIR}/data/memory}"
export SESSION_OUTPUTS_DIR="${SESSION_OUTPUTS_DIR:-${OMA_ROOT_DIR}/data/session-outputs}"

mkdir -p "${SANDBOX_WORKDIR}" "${MEMORY_DATA_DIR}" "${SESSION_OUTPUTS_DIR}"
