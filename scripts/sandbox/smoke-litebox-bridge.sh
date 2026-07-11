#!/usr/bin/env bash
# Direct smoke: litebox-bridge.mjs exec without oma-server.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/sandbox/litebox-env.sh"

BRIDGE_DIR="${ROOT_DIR}/scripts/sandbox"
MARKER="${LITEBOX_SMOKE_MARKER:-litebox-ok}"

printf '%s\n' \
  '{"id":1,"op":"init","image":"'"${SANDBOX_IMAGE}"'","name":"oma-litebox-smoke","memoryMib":'"${LITEBOX_MEMORY_MIB}"',"cpus":'"${LITEBOX_CPUS}"'}' \
  '{"id":2,"op":"exec","command":"echo '"${MARKER}"'","timeoutMs":120000}' \
  '{"id":3,"op":"destroy"}' \
  | (cd "${BRIDGE_DIR}" && "${NODE_BIN}" litebox-bridge.mjs) \
  | tee /tmp/oma-litebox-bridge-smoke.jsonl

if ! grep -q '"id":2,"ok":true' /tmp/oma-litebox-bridge-smoke.jsonl; then
  echo "error: litebox bridge exec failed" >&2
  cat /tmp/oma-litebox-bridge-smoke.jsonl >&2
  exit 1
fi

if ! grep -q "${MARKER}" /tmp/oma-litebox-bridge-smoke.jsonl; then
  echo "error: expected output containing ${MARKER}" >&2
  exit 1
fi

echo "[litebox-bridge] PASS"
