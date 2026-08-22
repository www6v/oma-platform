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

# Check whether Docker Hub mirrors are configured (info only, no warning).
check_mirror() {
  if [[ -f /etc/docker/daemon.json ]] && grep -q 'registry-mirrors' /etc/docker/daemon.json 2>/dev/null; then
    local mirror
    mirror="$(grep -A1 'registry-mirrors' /etc/docker/daemon.json | grep -oE 'https?://[^"]+' | head -1)"
    if [[ -n "${mirror}" ]]; then
      echo "[OK] Docker Hub mirror: ${mirror}"
    fi
  fi
}

# Configure Docker daemon to use a domestic registry mirror.
cmd_setup_mirror() {
  local mirror="${1:-https://registry.cn-hangzhou.aliyuncs.com}"
  local daemon_json="/etc/docker/daemon.json"

  echo "Configuring Docker daemon to use mirror: ${mirror}"

  if [[ ! -f "${daemon_json}" ]]; then
    # Create new daemon.json
    sudo tee "${daemon_json}" > /dev/null <<EOF
{
  "registry-mirrors": ["${mirror}"]
}
EOF
  else
    # Backup existing config
    if [[ ! -f "${daemon_json}.bak" ]]; then
      sudo cp "${daemon_json}" "${daemon_json}.bak"
      echo "Backup saved to ${daemon_json}.bak"
    fi

    # Use jq if available, otherwise sed
    if command -v jq >/dev/null 2>&1; then
      local tmp
      tmp="$(mktemp)"
      jq --arg m "${mirror}" '.["registry-mirrors"] = [$m]' "${daemon_json}" > "${tmp}"
      sudo mv "${tmp}" "${daemon_json}"
    else
      # Simple sed-based approach (assumes no existing registry-mirrors)
      if grep -q 'registry-mirrors' "${daemon_json}" 2>/dev/null; then
        echo "[WARN] registry-mirrors already configured. Edit ${daemon_json} manually." >&2
        return 0
      fi
      # Insert before the closing brace
      sudo sed -i "s/}[[:space:]]*$/{\"registry-mirrors\": [\"${mirror}\"]}/" "${daemon_json}"
    fi
  fi

  echo "Restarting Docker daemon..."
  if sudo systemctl restart docker 2>/dev/null; then
    echo "[OK] Docker daemon restarted with mirror configured."
    echo "    Current mirror: ${mirror}"
  else
    echo "[WARN] Could not restart Docker via systemctl. Please restart Docker manually:" >&2
    echo "    sudo systemctl restart docker" >&2
  fi
}

# Pre-flight: verify Docker, disk space, and required ports before starting.
preflight() {
  # Check Docker daemon
  if ! docker info >/dev/null 2>&1; then
    echo "error: Docker daemon is not running or unreachable." >&2
    echo "  sudo systemctl status docker" >&2
    return 1
  fi

  # Check disk space — warn if / has less than 5 GB free
  local avail_gb
  avail_gb="$(df -BG / | awk 'NR==2 {gsub(/G/,"",$4); print $4}')"
  if [[ "${avail_gb}" -lt 5 ]]; then
    echo "[WARN] Low disk space: ${avail_gb}GB free on / (recommend >= 5GB)." >&2
    echo "  docker system prune -af  # remove unused images/layers" >&2
    echo "  df -h /" >&2
  else
    echo "[OK] Disk: ${avail_gb}GB free on /"
  fi

  # Check Docker buildx
  if ! docker buildx version >/dev/null 2>&1; then
    echo "[WARN] buildx not found, falling back to legacy build" >&2
    export DOCKER_BUILDKIT=0
    export COMPOSE_DOCKER_CLI_BUILD=0
  else
    echo "[OK] Docker buildx: $(docker buildx version 2>/dev/null)"
  fi
}

usage() {
  cat <<EOF
Usage: $(basename "$0") <command> [options]

Commands:
  up            Build (if needed) and start services in the background (default)
  up --no-build Start services without rebuilding (faster restart)
  up-fg         Build and start in the foreground
  down          Stop and remove containers (also removes orphans)
  build         Build images without starting
  pull          Pull base images (useful for warming up cache before build)
  restart       Restart all services
  logs          Tail logs (optional service: oma-platform | oma-auth | oma-harness-lb)
  ps            Show container status
  preflight     Run pre-flight checks without starting anything
  setup-mirror  Configure Docker daemon to use a domestic registry mirror
  smoke         Run scripts/e2e/smoke-test.sh against the running stack

Independent services (managed separately):
  oma-openviking  ./openviking/start-openviking.sh {start|stop|restart|status|logs}
  oma-deepseek    ./deepseek/start-deepseek.sh   {start|stop|restart|status|logs}

Examples:
  $(basename "$0")
  $(basename "$0") up
  $(basename "$0") setup-mirror
  $(basename "$0") setup-mirror https://docker.m.daocloud.io
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
    preflight
    check_mirror
    free_stale_harness_port
    check_harness_port

    build_args=()
    if [[ "${1:-}" == "--no-build" ]]; then
      echo "[info] skipping build (--no-build), starting existing images..."
      shift || true
    else
      build_args=(--build)
    fi
    compose up -d "${build_args[@]}" "$@"
    print_endpoints
    ;;
  up-fg)
    ensure_data_dir
    preflight
    check_mirror
    free_stale_harness_port
    check_harness_port
    compose up --build "$@"
    ;;
  down)
    compose down --remove-orphans "$@"
    ;;
  build)
    preflight
    check_mirror
    compose build "$@"
    ;;
  pull)
    preflight
    check_mirror
    echo "Pulling base images (this may take a while on first run)..."
    compose pull "$@"
    ;;
  restart)
    ensure_data_dir
    preflight
    check_harness_port
    compose restart "$@"
    print_endpoints
    ;;
  logs)
    compose logs -f "$@"
    ;;
  ps|status)
    compose ps "$@"
    ;;
  preflight)
    preflight
    check_mirror
    check_harness_port 2>/dev/null || true
    ;;
  setup-mirror)
    cmd_setup_mirror "$@"
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
