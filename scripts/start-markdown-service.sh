#!/usr/bin/env bash
set -euo pipefail

# Start the Markdown conversion service
# This service should be deployed on an overseas server with good network connectivity

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Check if fastapi and uvicorn are installed
if ! python3 -c "import fastapi" 2>/dev/null; then
  echo "Installing required packages..."
  pip3 install fastapi uvicorn httpx
fi

PORT="${MARKDOWN_SERVICE_PORT:-8899}"

echo "Starting Markdown conversion service on port ${PORT}..."
echo "Endpoint: http://0.0.0.0:${PORT}/convert"
echo "Health check: http://0.0.0.0:${PORT}/health"
echo ""
echo "To use this service from China, set in .env:"
echo "  MARKDOWN_SERVICE_URL=http://your-server-ip:${PORT}"
echo ""

cd "${ROOT_DIR}"
exec python3 markdown_service.py
