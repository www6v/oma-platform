#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
CONSOLE="${CONSOLE_ROOT:-$ROOT/../open-managed-agents/apps/console}"
MONOREPO="${OMA_MONOREPO_ROOT:-$(cd "$(dirname "$CONSOLE")/.." && pwd)}"

if [[ ! -d "$CONSOLE" ]]; then
  echo "Console source not found at $CONSOLE" >&2
  echo "Set CONSOLE_ROOT to open-managed-agents/apps/console" >&2
  exit 1
fi

if [[ ! -f "$MONOREPO/pnpm-lock.yaml" ]]; then
  echo "pnpm lockfile not found at $MONOREPO/pnpm-lock.yaml" >&2
  echo "Set OMA_MONOREPO_ROOT to the open-managed-agents repo root" >&2
  exit 1
fi

if ! command -v pnpm >/dev/null 2>&1; then
  echo "pnpm is required to build the console (open-managed-agents uses pnpm workspaces)" >&2
  echo "Install: npm install -g pnpm" >&2
  exit 1
fi

cd "$MONOREPO"
if [[ ! -d node_modules ]] || [[ ! -d "$CONSOLE/node_modules" ]]; then
  # Console depends on workspace packages; install only its dependency tree.
  pnpm install --frozen-lockfile --filter managed-agents-console...
fi
pnpm build:console
echo "Built console dist at $CONSOLE/dist"
