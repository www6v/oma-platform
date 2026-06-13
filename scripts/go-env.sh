#!/usr/bin/env bash
# Shared workspace Go toolchain (.tools/go).
# Usage: source scripts/go-env.sh   (bash or zsh)
#
# Do not use set -e / set -u here: sourcing this file runs in your interactive
# shell; go with no subcommand exits 2 and would close the terminal.

_oma_go_env_is_sourced() {
  if [[ -n "${ZSH_VERSION:-}" ]]; then
    [[ ${ZSH_EVAL_CONTEXT:-} == *:file* ]]
    return
  fi
  if [[ -n "${BASH_VERSION:-}" ]]; then
    [[ "${BASH_SOURCE[0]}" != "${0}" ]]
    return
  fi
  return 1
}

_oma_go_env_fail() {
  echo "error: $*" >&2
  if _oma_go_env_is_sourced; then
    return 1
  fi
  exit 1
}

if [[ -n "${ZSH_VERSION:-}" ]]; then
  _script="${(%):-%x}"
else
  _script="${BASH_SOURCE[0]:-${0:-}}"
fi

_oma_platform_root="$(cd "$(dirname "${_script}")/.." && pwd)" || \
  _oma_go_env_fail "cannot resolve oma-platform root from ${_script}"
_oma_workspace_root="$(cd "${_oma_platform_root}/.." && pwd)" || \
  _oma_go_env_fail "cannot resolve workspace root from ${_oma_platform_root}"

GO_BIN="${_oma_workspace_root}/.tools/go/bin/go"
if [[ ! -x "${GO_BIN}" ]]; then
  _oma_go_env_fail "Go toolchain not found at ${GO_BIN}"
fi

export GOROOT="${_oma_workspace_root}/.tools/go"
export PATH="${GOROOT}/bin:${PATH}"

unset _oma_platform_root _oma_workspace_root _script
unset -f _oma_go_env_is_sourced _oma_go_env_fail 2>/dev/null || true
