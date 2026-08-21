#!/usr/bin/env bash
# Deploy oma-platform + oma-harness with Docker Compose.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${DEPLOY_DIR}/.." && pwd)"
COMPOSE_FILE="${DEPLOY_DIR}/docker-compose.yml"
DEEPSEEK_COMPOSE_FILE="${DEPLOY_DIR}/docker-compose.deepseek.yml"

export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1

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
  local env_args=()
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    env_args=(--env-file "${ROOT_DIR}/.env")
  fi
  docker compose -f "${COMPOSE_FILE}" "${env_args[@]}" "$@"
}

compose_deepseek() {
  local env_args=()
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    env_args=(--env-file "${ROOT_DIR}/.env")
  fi
  docker compose -f "${DEEPSEEK_COMPOSE_FILE}" "${env_args[@]}" "$@"
}

ensure_data_dir() {
  mkdir -p "${ROOT_DIR}/data"
}

# Host :8090 is published by oma-harness-lb. After migrating from a single
# oma-harness service, the old container (e.g. deploy-oma-harness-1) is an
# orphan and still maps 8090 — remove it before starting the LB.
free_stale_harness_port() {
  local port=8090
  local line name

  while IFS= read -r line; do
    [[ -z "${line}" ]] && continue
    name="$(echo "${line}" | awk '{print $2}')"
    # Keep the current LB; drop everything else publishing host :8090.
    if [[ "${name}" == *oma-harness-lb* ]]; then
      continue
    fi
    echo "hint: removing stale container holding :${port}: ${name}" >&2
    docker rm -f "${name}" >/dev/null
  done < <(
    docker ps --format '{{.ID}} {{.Names}} {{.Ports}}' 2>/dev/null \
      | grep -E "0\.0\.0\.0:${port}->|:::${port}->" || true
  )
}

check_harness_port() {
  local port=8090
  local holders=""

  holders="$(
    docker ps --format '{{.ID}}\t{{.Names}}\t{{.Ports}}' 2>/dev/null \
      | grep -E "0\.0\.0\.0:${port}->|:::${port}->" || true
  )"

  if [[ -n "${holders}" ]]; then
    if echo "${holders}" | grep -q 'oma-harness-lb'; then
      return 0
    fi
    echo "error: host port ${port} is already allocated — oma-harness-lb cannot bind." >&2
    echo "holders:" >&2
    echo "${holders}" | sed 's/^/  /' >&2
    echo >&2
    echo "fix:" >&2
    echo "  docker ps -a --format '{{.Names}}\t{{.Ports}}' | grep ${port}" >&2
    echo "  docker rm -f <container>" >&2
    echo "  $(basename "$0") up" >&2
    return 1
  fi

  if command -v ss >/dev/null 2>&1; then
    if ss -ltn "( sport = :${port} )" 2>/dev/null | grep -q ":${port}"; then
      echo "error: host port ${port} is in use by a non-docker process." >&2
      echo "  ss -ltnp 'sport = :${port}'" >&2
      return 1
    fi
  elif command -v lsof >/dev/null 2>&1; then
    if lsof -nP -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1; then
      echo "error: host port ${port} is in use by a non-docker process." >&2
      lsof -nP -iTCP:"${port}" -sTCP:LISTEN >&2 || true
      return 1
    fi
  fi
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  up          Build (if needed) and start services in the background (default)
  up-fg       Build and start in the foreground
  down        Stop and remove containers (also removes orphans)
  build       Build images without starting
  restart     Restart all services
  logs        Tail logs (optional service: oma-platform | oma-auth | oma-harness-lb)
  ps          Show container status
  smoke       Run scripts/e2e/smoke-test.sh against the running stack

DeepSeek Commands:
  deepseek-up           Start oma-deepseek independently (background)
  deepseek-down         Stop and remove oma-deepseek
  deepseek-logs         Tail oma-deepseek logs
  deepseek-ps           Show oma-deepseek container status
  deepseek-connect      Connect oma-deepseek to main stack network (run after deepseek-up)

Examples:
  $(basename "$0")
  $(basename "$0") up
  $(basename "$0") logs oma-platform
  $(basename "$0") down

  # DeepSeek standalone:
  $(basename "$0") deepseek-up
  $(basename "$0") deepseek-connect    # connect to main network
  $(basename "$0") deepseek-logs
  $(basename "$0") deepseek-down

  # Or without docker.sh (must pass parent .env for build-arg substitution):
  docker compose --env-file ../.env up -d --build --remove-orphans

Environment:
  Loads ${ROOT_DIR}/.env when present (via --env-file and service env_file).
  Platform API: http://localhost:8787
  Harness LB:   http://localhost:8090  (oma-harness-lb → harness-1/2)
  DeepSeek:     http://localhost:3080  (oma-deepseek)
EOF
}

print_endpoints() {
  echo "oma-platform: http://localhost:8787  (Console UI + /health)"
  echo "oma-harness:  http://localhost:8090  (LB → oma-harness-1/2)"
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
    free_stale_harness_port
    check_harness_port
    compose up -d --build --remove-orphans "$@"
    print_endpoints
    ;;
  up-fg)
    ensure_data_dir
    free_stale_harness_port
    check_harness_port
    compose up --build --remove-orphans "$@"
    ;;
  down)
    compose down --remove-orphans "$@"
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
    "${ROOT_DIR}/scripts/e2e/smoke-test.sh"
    ;;
  deepseek-up)
    compose_deepseek up -d --build --remove-orphans "$@"
    echo "oma-deepseek started at http://localhost:3080"
    echo "Run '$(basename "$0") deepseek-connect' to connect it to the main network"
    ;;
  deepseek-down)
    compose_deepseek down --remove-orphans "$@"
    echo "oma-deepseek stopped"
    ;;
  deepseek-logs)
    compose_deepseek logs -f "$@"
    ;;
  deepseek-ps|deepseek-status)
    compose_deepseek ps "$@"
    ;;
  deepseek-connect)
    # Get the main stack's network name (usually deploy_default)
    MAIN_NETWORK=$(compose config --format json 2>/dev/null | grep -o '"name":"[^"]*"' | head -1 | cut -d'"' -f4)
    if [[ -z "${MAIN_NETWORK}" ]]; then
      MAIN_NETWORK="deploy_default"
    fi
    
    # Get deepseek container ID
    DEEPSEEK_CONTAINER=$(compose_deepseek ps -q oma-deepseek 2>/dev/null || true)
    if [[ -z "${DEEPSEEK_CONTAINER}" ]]; then
      echo "error: oma-deepseek is not running; run '$(basename "$0") deepseek-up' first" >&2
      exit 1
    fi
    
    # Connect to main network
    echo "Connecting oma-deepseek to network: ${MAIN_NETWORK}"
    docker network connect "${MAIN_NETWORK}" "${DEEPSEEK_CONTAINER}" 2>/dev/null || {
      echo "warning: already connected or network not found"
      echo "Available networks:"
      docker network ls --format '{{.Name}}' | grep -E 'deploy|oma' || true
    }
    
    echo "oma-deepseek is now accessible at http://oma-deepseek:3080 from oma-platform"
    ;;
  *)
    echo "error: unknown command: ${cmd}" >&2
    echo >&2
    usage >&2
    exit 1
    ;;
esac
