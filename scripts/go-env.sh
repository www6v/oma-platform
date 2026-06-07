#!/usr/bin/env bash
# Shared workspace Go toolchain (.tools/go). Source from bash scripts only.
set -euo pipefail

_script="${BASH_SOURCE[0]:-${0:-}}"
OMA_PLATFORM_ROOT="$(cd "$(dirname "${_script}")/.." && pwd)"
WORKSPACE_ROOT="$(cd "${OMA_PLATFORM_ROOT}/.." && pwd)"
GO_BIN="${WORKSPACE_ROOT}/.tools/go/bin/go"

if [[ ! -x "${GO_BIN}" ]]; then
  echo "error: Go toolchain not found at ${GO_BIN}" >&2
  exit 1
fi

export GOROOT="${WORKSPACE_ROOT}/.tools/go"
export PATH="${GOROOT}/bin:${PATH}"
