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

## Console UI

The OMA Console SPA from `open-managed-agents/apps/console` is served on the same port as the API (main-node `CONSOLE_DIR` pattern). Use the workspace Go toolchain at `.tools/go`:

```bash
# Terminal 1 — harness
./start-harness.sh

# Terminal 2 — platform + console (builds dist if missing)
./start-console.sh
```

Open http://localhost:8787 — `OMA_CONSOLE_DEV=1` stubs `/auth-info` and `/auth/get-session` so the login gate passes without better-auth.

**Docker:** `docker compose up` mounts `../open-managed-agents/apps/console/dist` at `/app/console` when present. Build the console first, or set `CONSOLE_DIST` to another path.

**Scope:** Agents, sessions, environments, and model cards work against oma-platform APIs. Vault, skills, billing, and other main-node-only routes will 404 until implemented.

**Production:** `OMA_CONSOLE_DEV=1` disables API-key checks — dev only. Production console needs better-auth or another browser auth path; until then use API clients with `x-api-key`.

## Docker

```bash
docker compose up --build
```

Copy `.env.example` to `.env`. For real model calls set `OMA_FAKE_HARNESS=0` and configure piPy via `~/.pi/agent/settings.json`, `models.json`, and `auth.json` (mounted into the harness container in compose).
