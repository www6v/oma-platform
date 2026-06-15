#!/usr/bin/env bash
# Run a command with project .env + e2e target.env loaded (for node/playwright).
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

# shellcheck disable=SC1091
source "${ROOT_DIR}/scripts/e2e/common.sh"

if [[ $# -lt 1 ]]; then
  echo "usage: $0 <command> [args...]" >&2
  echo "example: $0 node scripts/e2e/console-dogfood.mjs" >&2
  exit 1
fi

exec "$@"
