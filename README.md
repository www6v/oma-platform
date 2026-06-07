# oma-building

Greenfield OMA MVP: Go platform + Python piPy harness sidecar.

## Quick start (local)

```bash
# Terminal 1 — harness (fake mode, no API key)
cd harness
OMA_FAKE_HARNESS=1 uvicorn oma_adapter.main:app --port 8090

# Terminal 2 — platform
export OMA_FAKE_HARNESS=1
export HARNESS_URL=http://127.0.0.1:8090
export OMA_API_KEY=dev-key
go run ./cmd/oma-server/
```

## Smoke test

```bash
AID=$(curl -s -X POST localhost:8787/v1/agents \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d '{"name":"hello","model":"claude-sonnet-4-20250514","system_prompt":"You are helpful."}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')

SID=$(curl -s -X POST localhost:8787/v1/sessions \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d "{\"agent\":\"$AID\"}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')

curl -s -X POST localhost:8787/v1/sessions/$SID/events \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d '{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}'
```

## Docker

```bash
docker compose up --build
```

Copy `.env.example` to `.env`. For real model calls set `OMA_FAKE_HARNESS=0` and configure piPy via `~/.pi/agent/settings.json`, `models.json`, and `auth.json` (mounted into the harness container in compose).
