#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

if [[ -f "${ROOT}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT}/.env"
  set +a
fi

echo "== T15 smoke: internal API unit tests =="

GOPROXY="${GOPROXY:-https://goproxy.cn,direct}" \
  go test ./internal/api/... \
  -run 'TestInternal|TestInjectMcp|TestAppendToAgent' \
  -count=1 -v

BASE_URL="${OMA_BASE_URL:-http://127.0.0.1:8787}"
INTERNAL_SECRET="${OMA_INTERNAL_SECRET:-}"

if [[ -n "${INTERNAL_SECRET}" ]] && curl -sf "${BASE_URL}/health" >/dev/null 2>&1; then
  echo "== T15 smoke: live internal sessions preflight =="
  internal_ok="$(
    curl -sS -o /dev/null -w '%{http_code}' \
      -H "x-internal-secret: ${INTERNAL_SECRET}" \
      "${BASE_URL}/v1/internal/model_cards/resolve?model_id=__t15__" || true
  )"
  if [[ "${internal_ok}" == "404" ]]; then
    echo "error: /v1/internal/* not mounted — oma-server likely needs restart" >&2
    echo "  set OMA_INTERNAL_SECRET in .env and restart (e.g. ./start-console.sh)" >&2
    exit 1
  fi
  code="$(
    curl -sS -o /dev/null -w '%{http_code}' \
      -H "x-internal-secret: ${INTERNAL_SECRET}" \
      -H "Content-Type: application/json" \
      -d '{"action":"create"}' \
      "${BASE_URL}/v1/internal/sessions" || true
  )"
  if [[ "${code}" == "404" ]]; then
    echo "error: POST /v1/internal/sessions returned 404 (stale oma-server binary)" >&2
    echo "  source scripts/go-env.sh && go run ./cmd/oma-server/  # restart platform" >&2
    exit 1
  fi
  if [[ "${code}" != "400" ]]; then
    echo "error: POST /v1/internal/sessions expected 400, got ${code}" >&2
    exit 1
  fi
  echo "live internal POST /sessions ok (400 for invalid body)"
fi

echo "T15 smoke passed"
