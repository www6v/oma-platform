#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONSOLE="${CONSOLE_ROOT:-$ROOT/console}"

if [[ ! -d "$CONSOLE" ]]; then
  echo "Console source not found at $CONSOLE" >&2
  echo "Set CONSOLE_ROOT to oma-platform/console" >&2
  exit 1
fi

if ! command -v npm >/dev/null 2>&1; then
  echo "npm is required to build the console" >&2
  exit 1
fi

cd "$CONSOLE"
if [[ ! -d node_modules ]]; then
  npm install
fi
npm run build
echo "Built console dist at $CONSOLE/dist"
