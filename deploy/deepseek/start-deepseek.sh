#!/usr/bin/env bash
# Manage oma-deepseek as a standalone Docker service.
# Usage: ./start-deepseek.sh {start|stop|restart|status|logs|rebuild}

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
SERVICE_NAME="oma-deepseek"

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

cmd_start() {
  # Ensure the shared network exists (created by main stack or here).
  docker network create oma-network 2>/dev/null || true
  echo "Starting $SERVICE_NAME..."
  compose up -d
  echo "Waiting for health check (this may take a few minutes on first build)..."
  compose ps --format json 2>/dev/null | grep -q '"Health":"healthy"' && echo "[OK] $SERVICE_NAME is healthy." || {
    for i in $(seq 1 60); do
      sleep 5
      if compose ps --format json 2>/dev/null | grep -q '"Health":"healthy"'; then
        echo "[OK] $SERVICE_NAME is healthy."
        return 0
      fi
    done
    echo "[WARN] $SERVICE_NAME may not be healthy yet. Check with: $0 status"
  }
}

cmd_stop() {
  echo "Stopping $SERVICE_NAME..."
  compose down
  echo "[OK] $SERVICE_NAME stopped."
}

cmd_restart() {
  cmd_stop
  cmd_start
}

cmd_status() {
  compose ps
  echo ""
  echo "--- Health ---"
  compose ps --format json 2>/dev/null | grep -o '"Health":"[^"]*"' || echo "(no health data)"
}

cmd_logs() {
  compose logs -f "${1:-}"
}

cmd_rebuild() {
  echo "Rebuilding $SERVICE_NAME image..."
  compose build --no-cache
  cmd_stop
  cmd_start
}

cmd_clean() {
  echo "Stopping $SERVICE_NAME and removing volumes..."
  compose down -v
  echo "[OK] $SERVICE_NAME cleaned."
}

case "${1:-help}" in
  start)   cmd_start ;;
  stop)    cmd_stop ;;
  restart) cmd_restart ;;
  status)  cmd_status ;;
  logs)    cmd_logs "$2" ;;
  rebuild) cmd_rebuild ;;
  clean)   cmd_clean ;;
  help|*)
    echo "Usage: $0 {start|stop|restart|status|logs|rebuild|clean}"
    echo ""
    echo "  start   - Build (if needed) and start the container"
    echo "  stop    - Stop and remove the container"
    echo "  restart - Stop then start"
    echo "  status  - Show container status and health"
    echo "  logs    - Follow container logs"
    echo "  rebuild - Rebuild image from scratch, then restart"
    echo "  clean   - Stop and remove volume data"
    ;;
esac
