#!/usr/bin/env bash
# Deploy oma-platform + oma-harness with Docker Compose.
# Independent services (oma-openviking / oma-deepseek) are managed by their
# own scripts: start-openviking.sh / start-deepseek.sh.
set -euo pipefail

DEPLOY_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${DEPLOY_DIR}/.." && pwd)"
COMPOSE_FILE="${DEPLOY_DIR}/docker-compose.yml"

export DOCKER_BUILDKIT=1
export COMPOSE_DOCKER_CLI_BUILD=1

# Domestic Docker Hub mirror for base image pulls.
# Set in .env: REGISTRY_MIRROR=registry.cn-hangzhou.aliyuncs.com/library
# This overrides FROM images in Dockerfiles to pull from the mirror registry.
export REGISTRY_MIRROR="${REGISTRY_MIRROR:-}"

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

# Check whether Docker Hub pulls are slow and suggest a domestic mirror.
check_mirror() {
  if [[ -n "${REGISTRY_MIRROR}" ]]; then
    echo "[OK] Using Docker Hub mirror: ${REGISTRY_MIRROR}"
    return 0
  fi

  # Check if Docker daemon has a mirror configured.
  if [[ -f /etc/docker/daemon.json ]] && grep -q 'registry-mirrors' /etc/docker/daemon.json 2>/dev/null; then
    local mirrors
    mirrors="$(grep -A1 'registry-mirrors' /etc/docker/daemon.json | tail -1)"
    echo "[OK] Docker daemon has registry mirror configured: ${mirrors}"
    return 0
  fi

  echo "[WARN] No Docker Hub mirror configured. Base image pulls may be slow in China." >&2
  echo "  Option 1 (recommended): Add to /etc/docker/daemon.json and restart Docker:" >&2
  echo '    {"registry-mirrors": ["https://registry.cn-hangzhou.aliyuncs.com"]}' >&2
  echo "  Option 2: Set in .env:" >&2
  echo "    REGISTRY_MIRROR=registry.cn-hangzhou.aliyuncs.com/library" >&2
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  up          Build (if needed) and start services in the background (default)
  up-fg       Build and start in the foreground
  down        Stop and remove containers (also removes orphans)
  build       Build images without starting
  pull        Pull base images (useful for warming up cache before build)
  restart     Restart all services
  logs        Tail logs (optional service: oma-platform | oma-auth | oma-harness-lb)
  ps          Show container status
  smoke       Run scripts/e2e/smoke-test.sh against the running stack

Independent services (managed separately):
  oma-openviking  ./openviking/start-openviking.sh {start|stop|restart|status|logs}
  oma-deepseek    ./deepseek/start-deepseek.sh   {start|stop|restart|status|logs}

Examples:
  $(basename "$0")
  $(basename "$0") up
  $(basename "$0") logs oma-platform
  $(basename "$0") down

  ./openviking/start-openviking.sh start
  ./deepseek/start-deepseek.sh   start

  # Or without docker.sh (must pass parent .env for build-arg substitution):
  docker compose --env-file ../.env -f docker-compose.yml up -d --build --remove-orphans

Environment:
  Loads ${ROOT_DIR}/.env when present (via --env-file and service env_file).
  Platform API: http://localhost:8787
  Harness LB:   http://localhost:8090  (oma-harness-lb → harness-1/2)
  OpenViking:   http://localhost:1933  (oma-openviking, managed by start-openviking.sh)
  DeepSeek:     http://localhost:3080  (oma-deepseek, managed by start-deepseek.sh)
EOF
}

print_endpoints() {
  echo "oma-platform: http://localhost:8787  (Console UI + /health)"
  echo "oma-harness:  http://localhost:8090  (LB → oma-harness-1/2)"
  echo "oma-openviking: http://localhost:1933  (use openviking/start-openviking.sh to start)"
  echo "oma-deepseek:   http://localhost:3080  (use deepseek/start-deepseek.sh to start)"
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
    check_mirror
    free_stale_harness_port
    check_harness_port
    compose up -d --build "$@"
    print_endpoints
    ;;
  up-fg)
    ensure_data_dir
    check_mirror
    free_stale_harness_port
    check_harness_port
    compose up --build "$@"
    ;;
  down)
    compose down --remove-orphans "$@"
    ;;
  build)
    compose build "$@"
    ;;
  pull)
    check_mirror
    echo "Pulling base images (this may take a while on first run)..."
    compose pull "$@"
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
  *)
    echo "error: unknown command: ${cmd}" >&2
    echo >&2
    usage >&2
    exit 1
    ;;
esac
