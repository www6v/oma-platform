#!/usr/bin/env bash
# Deploy oma-platform + oma-harness with Docker Compose.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${DEPLOY_DIR}/.." && pwd)"
COMPOSE_FILE="${DEPLOY_DIR}/docker-compose.yml"

load_env() {
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "${ROOT_DIR}/.env"
    set +a
    return 0
  fi

  if [[ -f "${ROOT_DIR}/.env.example" ]]; then
    echo "hint: copy ${ROOT_DIR}/.env.example to ${ROOT_DIR}/.env" >&2
  fi
}

compose() {
  docker compose -f "${COMPOSE_FILE}" "$@"
}

ensure_data_dir() {
  mkdir -p "${ROOT_DIR}/data"
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  up          Build (if needed) and start services in the background (default)
  up-fg       Build and start in the foreground
  down        Stop and remove containers
  build       Build images without starting
  restart     Restart all services
  logs        Tail logs (optional service: oma-platform | oma-harness)
  ps          Show container status
  smoke       Run scripts/smoke-test.sh against the running stack

Examples:
  $(basename "$0")
  $(basename "$0") up
  $(basename "$0") logs oma-platform
  $(basename "$0") down

Environment:
  Loads ${ROOT_DIR}/.env when present.
  Platform API: http://localhost:8787
  Harness API:  http://localhost:8090
EOF
}

print_endpoints() {
  echo "oma-platform: http://localhost:8787"
  echo "oma-harness:  http://localhost:8090"
}

cmd="${1:-up}"
if [[ "${cmd}" == "-h" || "${cmd}" == "--help" || "${cmd}" == "help" ]]; then
  usage
  exit 0
fi
shift || true

load_env

case "${cmd}" in
  up)
    ensure_data_dir
    compose up -d --build "$@"
    print_endpoints
    ;;
  up-fg)
    ensure_data_dir
    compose up --build "$@"
    ;;
  down)
    compose down "$@"
    ;;
  build)
    compose build "$@"
    ;;
  restart)
    compose restart "$@"
    print_endpoints
    ;;
  logs)
    compose logs -f "$@"
    ;;
  ps|status)
    compose ps "$@"
    ;;
  smoke)
    ensure_data_dir
    if ! compose ps --status running --quiet oma-platform | grep -q .; then
      echo "error: oma-platform is not running; run $(basename "$0") up first" >&2
      exit 1
    fi
    export HARNESS_URL="${HARNESS_URL:-http://127.0.0.1:8090}"
    "${ROOT_DIR}/scripts/smoke-test.sh"
    ;;
  *)
    echo "error: unknown command: ${cmd}" >&2
    echo >&2
    usage >&2
    exit 1
    ;;
esac
