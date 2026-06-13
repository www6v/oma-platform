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

BASE_URL="${OMA_BASE_URL:-http://127.0.0.1:8787}"
API_KEY="${OMA_API_KEY:-dev-key}"
BETA="managed-agents-2026-04-01,dreaming-2026-04-21"

auth=(-H "Authorization: Bearer ${API_KEY}")

echo "== T14 smoke: dreams + cost_report =="

if ! curl -sf "${BASE_URL}/health" >/dev/null 2>&1; then
  echo "error: platform not reachable at ${BASE_URL}" >&2
  echo "  start oma-server (e.g. ./start-console.sh) and retry" >&2
  exit 1
fi

dreams_preflight="$(
  curl -sS -w $'\n%{http_code}' \
    "${auth[@]}" \
    -H "anthropic-beta: ${BETA}" \
    "${BASE_URL}/v1/dreams" 2>/dev/null || true
)"
dreams_body="${dreams_preflight%$'\n'*}"
dreams_code="${dreams_preflight##*$'\n'}"
if [[ "${dreams_code}" == "404" ]]; then
  echo "error: GET /v1/dreams returned 404 — oma-server likely needs rebuild/restart" >&2
  echo "  fix: source scripts/go-env.sh && go build ./cmd/oma-server/ && restart server" >&2
  exit 1
fi
if [[ "${dreams_code}" != "200" ]]; then
  echo "error: GET /v1/dreams returned HTTP ${dreams_code}" >&2
  echo "  body: ${dreams_body}" >&2
  exit 1
fi

store_id="$(
  curl -sf "${auth[@]}" -H "Content-Type: application/json" \
    -d '{"name":"t14-dream-input"}' \
    "${BASE_URL}/v1/memory_stores" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
)"
echo "input memory store: ${store_id}"

curl -sf "${auth[@]}" -H "Content-Type: application/json" \
  -d '{"path":"/t14/note.md","content":"dream smoke memory"}' \
  "${BASE_URL}/v1/memory_stores/${store_id}/memories" >/dev/null

dream_id="$(
  curl -sf "${auth[@]}" \
    -H "Content-Type: application/json" \
    -H "anthropic-beta: ${BETA}" \
    -d "{\"inputs\":[{\"type\":\"memory_store\",\"memory_store_id\":\"${store_id}\"}],\"model\":\"claude-sonnet-4-6\"}" \
    "${BASE_URL}/v1/dreams" | python3 -c 'import json,sys; print(json.load(sys.stdin)["id"])'
)"
echo "dream: ${dream_id}"

for _ in $(seq 1 40); do
  status="$(
    curl -sf "${auth[@]}" -H "anthropic-beta: ${BETA}" \
      "${BASE_URL}/v1/dreams/${dream_id}" \
      | python3 -c 'import json,sys; print(json.load(sys.stdin)["status"])'
  )"
  if [[ "${status}" == "completed" ]]; then
    echo "dream completed"
    break
  fi
  sleep 0.25
done

if [[ "${status:-}" != "completed" ]]; then
  echo "dream did not complete: ${status:-unknown}" >&2
  exit 1
fi

curl -sf "${auth[@]}" -H "anthropic-beta: ${BETA}" \
  -X POST "${BASE_URL}/v1/dreams/${dream_id}/archive" >/dev/null

report="$(
  curl -sf "${auth[@]}" "${BASE_URL}/v1/cost_report?days=30"
)"
echo "${report}" | python3 -c 'import json,sys; d=json.load(sys.stdin); assert d.get("type")=="cost_report"; print("cost_report ok", d.get("span_count"))'

echo "T14 smoke passed"
