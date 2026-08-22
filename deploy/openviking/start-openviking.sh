#!/usr/bin/env bash
# Manage oma-openviking as a standalone Docker service.
# Usage: ./start-openviking.sh {start|stop|restart|status|logs|rebuild}

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
COMPOSE_FILE="$SCRIPT_DIR/docker-compose.yml"
CONF_FILE="$SCRIPT_DIR/openviking.conf.json"
CONF_EXAMPLE="$SCRIPT_DIR/openviking.conf.example.json"
SERVICE_NAME="oma-openviking"

compose() {
  docker compose -f "$COMPOSE_FILE" "$@"
}

# Check whether Docker Hub mirrors are configured.
check_mirror() {
  if [[ -f /etc/docker/daemon.json ]] && grep -q 'registry-mirrors' /etc/docker/daemon.json 2>/dev/null; then
    local mirror
    mirror="$(grep -A1 'registry-mirrors' /etc/docker/daemon.json | grep -oE 'https?://[^"]+' | head -1)"
    if [[ -n "${mirror}" ]]; then
      echo "[OK] Docker Hub mirror configured: ${mirror}"
      return 0
    fi
  fi
  echo "[WARN] No Docker Hub mirror configured. Base image pulls may be slow in China." >&2
  echo "  Run: ../docker.sh setup-mirror" >&2
}

check_config() {
  if [ ! -f "$CONF_FILE" ]; then
    echo "[WARN] $CONF_FILE not found."
    if [ -f "$CONF_EXAMPLE" ]; then
      echo "[INFO] Copying example config: $CONF_EXAMPLE -> $CONF_FILE"
      cp "$CONF_EXAMPLE" "$CONF_FILE"
      echo "[WARN] Please edit $CONF_FILE and set your DashScope API key before starting."
    fi
    return 1
  fi
  if grep -q "YOUR_DASHSCOPE_API_KEY" "$CONF_FILE" 2>/dev/null; then
    echo "[WARN] openviking.conf.json still contains placeholder API key."
    return 1
  fi
  return 0
}

cmd_start() {
  check_mirror
  check_config || true
  # Ensure the shared network exists (created by main stack or here).
  docker network create oma-network 2>/dev/null || true
  echo "Starting $SERVICE_NAME..."
  compose up -d
  echo "Waiting for health check..."
  for i in $(seq 1 40); do
    sleep 3
    if compose ps --format json 2>/dev/null | grep -q '"Health":"healthy"'; then
      echo "[OK] $SERVICE_NAME is healthy (took ~$((i * 3))s)."
      return 0
    fi
  done
  echo "[WARN] $SERVICE_NAME may not be healthy yet. Check with: $0 status"
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
  echo "Stopping $SERVICE_NAME and removing volume data..."
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
    echo "  logs    - Follow container logs (optional: pass service name)"
    echo "  rebuild - Rebuild image from scratch, then restart"
    echo "  clean   - Stop and remove volume data (destroys all stored context)"
    ;;
esac
