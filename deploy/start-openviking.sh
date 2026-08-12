#!/usr/bin/env bash
# Start a local OpenViking server (external memory provider for piPy-hermes-memory).
#
# Usage:
#   deploy/start-openviking.sh            # start (idempotent)
#   deploy/start-openviking.sh stop       # stop
#   deploy/start-openviking.sh status     # pid + health
#
# Env overrides:
#   OPENVIKING_CONFIG    config json   (default deploy/openviking.conf.json)
#   OPENVIKING_HOST      bind host     (default 127.0.0.1)
#   OPENVIKING_PORT      bind port     (default 1933)
#   OPENVIKING_VENV      venv dir      (default: reuse /tmp/ov-e2e-venv, else deploy/.openviking-venv)
#   OPENVIKING_VERSION   package pin   (default 0.4.13)
#   DASHSCOPE_API_KEY    fills the placeholder when the config is generated
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"

CONFIG="${OPENVIKING_CONFIG:-$SCRIPT_DIR/openviking.conf.json}"
HOST="${OPENVIKING_HOST:-127.0.0.1}"
PORT="${OPENVIKING_PORT:-1933}"
LOG="${OPENVIKING_LOG:-/tmp/openviking-server.log}"
PID_FILE="${OPENVIKING_PID_FILE:-/tmp/openviking-server.pid}"
OV_VERSION="${OPENVIKING_VERSION:-0.4.13}"

if [ -n "${OPENVIKING_VENV:-}" ]; then
  VENV="$OPENVIKING_VENV"
elif [ -x /tmp/ov-e2e-venv/bin/openviking-server ]; then
  VENV=/tmp/ov-e2e-venv
else
  VENV="$SCRIPT_DIR/.openviking-venv"
fi

health() {
  curl -s -m 3 "http://$HOST:$PORT/health" 2>/dev/null | grep -q '"healthy":true'
}

ensure_venv() {
  if [ ! -x "$VENV/bin/openviking-server" ]; then
    echo "[ov] creating venv at $VENV and installing openviking==$OV_VERSION ..."
    python3 -m venv "$VENV"
    "$VENV/bin/pip" install --quiet \
      -i "${PIP_INDEX_URL:-https://pypi.tuna.tsinghua.edu.cn/simple}" \
      "openviking==$OV_VERSION"
  fi
}

ensure_config() {
  if [ ! -f "$CONFIG" ]; then
    echo "[ov] generating $CONFIG from example (workspace: $REPO_ROOT/data/openviking)"
    mkdir -p "$REPO_ROOT/data/openviking"
    sed -e "s|YOUR_DASHSCOPE_API_KEY|${DASHSCOPE_API_KEY:-YOUR_DASHSCOPE_API_KEY}|g" \
        -e "s|/var/lib/openviking|$REPO_ROOT/data/openviking|g" \
        "$SCRIPT_DIR/openviking.conf.example.json" > "$CONFIG"
  fi
  if grep -q YOUR_DASHSCOPE_API_KEY "$CONFIG"; then
    echo "[ov] ERROR: $CONFIG contains placeholder keys — set DASHSCOPE_API_KEY and delete it, or edit by hand." >&2
    exit 1
  fi
}

running_pid() {
  if [ -f "$PID_FILE" ] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    cat "$PID_FILE"
    return 0
  fi
  lsof -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | head -1
}

cmd_stop() {
  local pid
  pid="$(running_pid)" || true
  if [ -n "$pid" ]; then
    echo "[ov] stopping pid $pid"
    kill "$pid" 2>/dev/null || true
    for _ in $(seq 1 10); do kill -0 "$pid" 2>/dev/null || break; sleep 0.5; done
    kill -9 "$pid" 2>/dev/null || true
  else
    echo "[ov] not running"
  fi
  rm -f "$PID_FILE"
}

cmd_status() {
  local pid
  pid="$(running_pid)" || true
  if [ -n "$pid" ] && health; then
    echo "[ov] running pid=$pid http://$HOST:$PORT $(curl -s -m 3 "http://$HOST:$PORT/health")"
  elif [ -n "$pid" ]; then
    echo "[ov] pid=$pid but health check failed — see $LOG"
  else
    echo "[ov] not running"
  fi
}

cmd_start() {
  if health; then
    echo "[ov] already running: http://$HOST:$PORT"
    return 0
  fi
  ensure_venv
  ensure_config
  echo "[ov] starting (venv=$VENV config=$CONFIG log=$LOG)"
  # double-fork + setsid: fully detach so the server survives shell/session exit
  python3 - "$VENV/bin/openviking-server" "$CONFIG" "$HOST" "$PORT" "$LOG" "$PID_FILE" << 'PYLAUNCH'
import os, sys

server, config, host, port, log, pidfile = sys.argv[1:7]
pid = os.fork()
if pid == 0:
    os.setsid()
    grand = os.fork()
    if grand == 0:
        fd = os.open(log, os.O_CREAT | os.O_APPEND | os.O_WRONLY, 0o644)
        os.dup2(fd, 1)
        os.dup2(fd, 2)
        os.execv(server, [server, "--config", config, "--host", host, "--port", port])
    with open(pidfile, "w") as f:
        f.write(str(grand))
    os._exit(0)
os.waitpid(pid, 0)
PYLAUNCH
  for _ in $(seq 1 30); do
    if health; then
      echo "[ov] up: http://$HOST:$PORT pid=$(cat "$PID_FILE")"
      return 0
    fi
    sleep 1
  done
  echo "[ov] ERROR: did not become healthy in 30s — tail $LOG" >&2
  tail -20 "$LOG" >&2 || true
  exit 1
}

case "${1:-start}" in
  start) cmd_start ;;
  stop) cmd_stop ;;
  status) cmd_status ;;
  restart) cmd_stop; cmd_start ;;
  *) echo "usage: $0 [start|stop|status|restart]" >&2; exit 2 ;;
esac
