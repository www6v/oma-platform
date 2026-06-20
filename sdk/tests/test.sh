#!/bin/bash
# Run E2E tests for oma-sdk
# Requires OMA_API_KEY and OMA_BASE_URL environment variables
# Example: OMA_API_KEY=dev-key OMA_BASE_URL=http://localhost:8787 ./tests/test.sh

cd "$(dirname "$0")/.."

# Check for required environment variables
if [ -z "$OMA_API_KEY" ]; then
    echo "Error: OMA_API_KEY environment variable is required"
    echo "Usage: OMA_API_KEY=dev-key OMA_BASE_URL=http://localhost:8787 ./tests/test.sh"
    exit 1
fi

# Set default BASE_URL if not provided
export OMA_BASE_URL="${OMA_BASE_URL:-http://localhost:8787}"

echo "Running tests with:"
echo "  OMA_BASE_URL=$OMA_BASE_URL"
echo "  OMA_KEEP_RESOURCES=${OMA_KEEP_RESOURCES:-0}"
echo ""

# Run pytest
pytest tests/
