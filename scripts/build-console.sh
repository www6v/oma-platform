#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONSOLE="${CONSOLE_ROOT:-$ROOT/../open-managed-agents/apps/console}"

if [[ ! -d "$CONSOLE" ]]; then
  echo "Console source not found at $CONSOLE" >&2
  echo "Set CONSOLE_ROOT to open-managed-agents/apps/console" >&2
  exit 1
fi

cd "$CONSOLE"
if [[ ! -d node_modules ]]; then
  npm ci
fi
npm run build
echo "Built console dist at $CONSOLE/dist"
