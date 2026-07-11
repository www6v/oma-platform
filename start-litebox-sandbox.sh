#!/usr/bin/env bash
# Start oma-server with SANDBOX_PROVIDER=litebox (BoxLite micro-VM).
#
# Prerequisites: Apple Silicon macOS or Linux with KVM.
# Intel Mac: use SANDBOX_PROVIDER=boxrun + remote boxlite serve instead.
#
# Usage:
#   ./start-litebox-sandbox.sh
#
# Full agent turns (optional second terminal):
#   ./start-harness.sh
#   # ensure .env has OMA_FAKE_HARNESS=0 for real LLM
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/sandbox/litebox-env.sh"
"${ROOT_DIR}/scripts/sandbox/install-litebox.sh"

precheck_rc=0
"${ROOT_DIR}/scripts/sandbox/litebox-precheck.sh" || precheck_rc=$?
if [[ "${precheck_rc}" == "2" ]]; then
  echo "" >&2
  echo "Cannot start LiteBox on this host. Options:" >&2
  echo "  1) Use Apple Silicon Mac or Linux with /dev/kvm" >&2
  echo "  2) Run boxlite serve on a KVM host and use:" >&2
  echo "       export SANDBOX_PROVIDER=boxrun" >&2
  echo "       export BOXRUN_URL=http://<host>:8100/v1/default" >&2
  echo "       ./start-console.sh" >&2
  exit 1
fi
if [[ "${precheck_rc}" != "0" ]]; then
  exit "${precheck_rc}"
fi

echo "[litebox] SANDBOX_PROVIDER=${SANDBOX_PROVIDER}"
echo "[litebox] bridge=${LITEBOX_BRIDGE_PATH}"
echo "[litebox] workdir=${SANDBOX_WORKDIR}"
echo "[litebox] starting oma-server (Console + API on ${OMA_LISTEN_ADDR:-:8787})"
echo "[litebox] for agent turns, also run: ./start-harness.sh"

cd "${ROOT_DIR}"
exec "${ROOT_DIR}/start-console.sh"
