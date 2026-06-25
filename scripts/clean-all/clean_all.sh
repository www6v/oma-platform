#!/bin/bash
# Script to clean all user resources using the OMA SDK
# This script calls clean_user_resources.py to delete agents, sessions,
# environments, vaults, memory stores, and skills.
#
# Agent deletion uses AgentExamples.cleanup_agent (archive + DELETE /v1/agents/:id).
# Shortcut: ./clean_all.sh --agents-only [--localhost|--remote URL]

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/../.." && pwd)"

# Load oma-platform/.env when present (OMA_API_KEY, OMA_BASE_URL, etc.)
if [[ -f "${ROOT_DIR}/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "${ROOT_DIR}/.env"
    set +a
fi

export OMA_API_KEY="${OMA_API_KEY:-dev-key}"

# Path to the Python script
PYTHON_SCRIPT="$SCRIPT_DIR/clean_user_resources.py"

# Default values
LOCALHOST=false
REMOTE_URL=""
BASE_URL=""
ARGS=()

# Normalize glued flags (e.g. --agents-only--localhost → two flags)
NORMALIZED=()
for arg in "$@"; do
    case "$arg" in
        --agents-only--*)
            NORMALIZED+=("--agents-only" "--${arg#--agents-only--}")
            ;;
        *)
            NORMALIZED+=("$arg")
            ;;
    esac
done
set -- "${NORMALIZED[@]}"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --localhost)
            LOCALHOST=true
            shift
            ;;
        --remote)
            REMOTE_URL="$2"
            shift 2
            ;;
        --base-url)
            BASE_URL="$2"
            shift 2
            ;;
        --agents-only)
            ARGS+=("--resource-type" "agents")
            shift
            ;;
        *)
            # Pass through other arguments to Python script
            ARGS+=("$1")
            shift
            ;;
    esac
done

# Ensure oma_sdk is importable by clean_user_resources.py
export PYTHONPATH="${ROOT_DIR}/sdk${PYTHONPATH:+:${PYTHONPATH}}"

# Check if Python script exists
if [ ! -f "$PYTHON_SCRIPT" ]; then
    echo "Error: Python script not found at $PYTHON_SCRIPT"
    exit 1
fi

# Build Python script arguments
PY_ARGS=()

if [ "$LOCALHOST" = true ]; then
    PY_ARGS+=("--localhost")
fi

if [ -n "$REMOTE_URL" ]; then
    PY_ARGS+=("--remote" "$REMOTE_URL")
fi

if [ -n "$BASE_URL" ]; then
    PY_ARGS+=("--base-url" "$BASE_URL")
fi

# Add any additional arguments
PY_ARGS+=("${ARGS[@]}")

# Run the Python script
python3 "$PYTHON_SCRIPT" "${PY_ARGS[@]}"
