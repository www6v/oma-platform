#!/usr/bin/env bash
# Verify host can run @boxlite-ai/boxlite (Apple Silicon macOS or Linux KVM).
#
# Exit codes:
#   0 — supported
#   1 — hard error (missing node, install failed)
#   2 — unsupported platform (e.g. Intel Mac) — smoke may skip
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/sandbox/litebox-env.sh"

log() {
  echo "[litebox-precheck] $*"
}

if ! command -v "${NODE_BIN}" >/dev/null 2>&1; then
  echo "error: node not found (set NODE_BIN or install Node 18+)" >&2
  exit 1
fi

arch="$(uname -m)"
os="$(uname -s)"

case "${os}" in
  Darwin)
    if [[ "${arch}" != "arm64" ]]; then
      log "SKIP: LiteBox does not support macOS Intel (${arch})"
      log "  Use Apple Silicon Mac, Linux with KVM, or SANDBOX_PROVIDER=boxrun"
      log "  See https://docs.boxlite.ai/getting-started"
      exit 2
    fi
    ;;
  Linux)
    if [[ ! -e /dev/kvm ]]; then
      log "WARN: /dev/kvm missing — LiteBox may fail (enable KVM or add user to kvm group)"
    fi
    ;;
  *)
    log "WARN: untested OS ${os}; proceeding"
    ;;
esac

if [[ ! -f "${LITEBOX_BRIDGE_PATH}" ]]; then
  echo "error: bridge missing at ${LITEBOX_BRIDGE_PATH}" >&2
  exit 1
fi

if [[ ! -d "${ROOT_DIR}/scripts/sandbox/node_modules/@boxlite-ai/boxlite" ]]; then
  log "LiteBox npm package not installed — run scripts/sandbox/install-litebox.sh"
  exit 1
fi

log "OK: ${os}/${arch}, node=$("${NODE_BIN}" --version), provider=${SANDBOX_PROVIDER}"
exit 0
