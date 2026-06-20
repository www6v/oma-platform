#!/bin/bash
# Script to clean all user resources using the OMA SDK
# This script calls clean_user_resources.py to delete agents, sessions, environments, vaults, memory stores, and skills

# Get the directory where this script is located
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Path to the Python script
PYTHON_SCRIPT="$SCRIPT_DIR/clean_user_resources.py"

# Default values
LOCALHOST=false
REMOTE_URL=""
BASE_URL=""

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
        *)
            # Pass through other arguments to Python script
            ARGS+=("$1")
            shift
            ;;
    esac
done

# Check if Python script exists
if [ ! -f "$PYTHON_SCRIPT" ]; then
    echo "Error: Python script not found at $PYTHON_SCRIPT"
    exit 1
fi

# Check if ANTHROPIC_API_KEY is set
if [ -z "$ANTHROPIC_API_KEY" ]; then
    echo "Error: ANTHROPIC_API_KEY environment variable is not set"
    echo "Please set it with: export ANTHROPIC_API_KEY=your_key"
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
