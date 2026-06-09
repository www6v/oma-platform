# oma-platform

> 中文文档：[readme-zh.md](./readme-zh.md)

Self-hostable **Open Managed Agents (OMA)** MVP: a Go platform runtime plus a Python piPy harness sidecar. The platform owns durability, concurrency, and the HTTP API; the harness owns the LLM loop and tool execution.

## Features

- **Versioned agents** — Create, update, and archive agents with immutable version snapshots (`/v1/agents`).
- **Durable sessions** — Append-only event logs in SQLite; sessions pin an agent version and environment at creation time.
- **Harness turns** — User messages trigger stateless LLM turns via the piPy sidecar (`POST /internal/turn`).
- **Real-time streaming** — Server-Sent Events (`GET /v1/sessions/:id/events/stream`) with optional replay.
- **Turn interruption** — `user.interrupt` cancels the active harness turn and returns the session to idle.
- **Crash recovery** — Orphan `running` sessions are reset to `idle` on platform startup.
- **Per-session sandboxes** — Isolated workdirs under `SANDBOX_WORKDIR/<session_id>/`; Go provisions paths, piPy runs tools inside them.
- **Environments** — Named execution contexts with config/metadata; a default local environment is created on boot.
- **Model cards** — Per-tenant model credentials and provider config; resolved at turn time when an agent references a model.
- **Agent toolsets** — OMA `agent_toolset_20260401` maps to piPy builtins: `bash`, `read`, `write`, `edit`, `glob`, `grep`, and more.
- **Console UI** — Optional SPA from `open-managed-agents/apps/console`, served on the same port as the API.
- **Docker Compose** — Two-service stack (`oma-platform` + `oma-harness`) with health checks and volume mounts.
- **Fake harness mode** — `OMA_FAKE_HARNESS=1` for local dev and CI without LLM API keys.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         Client (curl / Console / SDK)                   │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ HTTP + SSE
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-server (Go)                         :8787                          │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────┐  ┌───────────────┐  │
│  │  chi router │  │ Auth (x-api- │  │ Console SPA │  │ /health       │  │
│  │  /v1/*      │  │ key)         │  │ static      │  │               │  │
│  └──────┬──────┘  └──────────────┘  └─────────────┘  └───────────────┘  │
│         │                                                                 │
│  ┌──────▼──────────────────────────────────────────────────────────────┐  │
│  │  API layer (agents, sessions, environments, model_cards)         │  │
│  └──────┬──────────────────────────────────────────────────────────────┘  │
│         │                                                                 │
│  ┌──────▼──────────┐  ┌────────────────┐  ┌─────────────────────────┐  │
│  │ Session Registry │  │ Session Machine │  │ SSE Hub (stream.Hub)    │  │
│  │ (per-session     │  │ (turn lifecycle,│  │ live event fan-out      │  │
│  │  turn queue)     │  │  interrupt)     │  │                         │  │
│  └──────┬──────────┘  └────────┬───────┘  └─────────────────────────┘  │
│         │                      │                                          │
│  ┌──────▼──────────────────────▼──────────────────────────────────────┐  │
│  │  SQLite store (modernc.org/sqlite)                                   │  │
│  │  agents · agent_versions · sessions · session_events                 │  │
│  │  environments · model_cards                                          │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│         │                      │                                          │
│  ┌──────▼──────────┐  ┌───────▼────────┐  ┌──────────────────────────┐  │
│  │ Workdir Manager │  │ Model Resolver │  │ Harness HTTP Client      │  │
│  │ SANDBOX_WORKDIR │  │ (model cards)  │  │ POST /internal/turn      │  │
│  └─────────────────┘  └────────────────┘  └────────────┬─────────────┘  │
└────────────────────────────────────────────────────────┼────────────────┘
                                                         │ HTTP
                                                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-harness (Python / FastAPI)                      :8090              │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │  oma_adapter                                                     │   │
│  │  · project OMA SessionEvent[] → piPy messages                     │   │
│  │  · create_agent_session(in_memory=True, cwd=workdir)              │   │
│  │  · run one prompt turn (stateless)                                │   │
│  │  · emit OMA-shaped events (assistant, tool_use, tool_result, …)  │   │
│  └──────────────────────────────────────────────────────────────────┘   │
│  Tools execute in $SANDBOX_WORKDIR/<session_id>/ via piPy builtins      │
└─────────────────────────────────────────────────────────────────────────┘
```

### Component responsibilities

| Layer | Component | Responsibility |
|-------|-----------|----------------|
| **Platform (Go)** | `cmd/oma-server` | Process entrypoint; wires DB, harness client, and HTTP server. |
| | `internal/api` | REST routes, auth middleware, console dev stubs. |
| | `internal/store` | SQLite persistence, migrations, repositories. |
| | `internal/session` | Turn state machine, per-session async queue, interrupt handling. |
| | `internal/stream` | In-memory SSE pub/sub per session. |
| | `internal/workdir` | Create and isolate per-session sandbox directories. |
| | `internal/modelresolve` | Resolve agent model strings to model-card credentials. |
| | `internal/harness` | HTTP client to the Python sidecar (or `FakeClient` in dev). |
| | `internal/console` | Static file handler for the Console SPA. |
| **Harness (Python)** | `harness/oma_adapter` | Thin FastAPI adapter over piPy `create_agent_session`. |
| | `turn.py` | Stateless turn: project events → run prompt → emit OMA events. |
| | `tools.py` | Map OMA tool declarations to piPy builtin names. |
| | `emit.py` / `project.py` | OMA ↔ piPy event shape translation. |

### Request flow (one user turn)

1. Client `POST /v1/sessions/:id/events` with a `user.message` event.
2. API validates event types, appends to `session_events`, and enqueues a turn on the session registry.
3. Session machine loads history, ensures the session workdir, resolves the model card, and calls `POST /internal/turn` on the harness.
4. Harness projects persisted events into piPy messages, runs one in-memory agent session with `cwd=workdir`, and returns new OMA events.
5. Platform persists harness output, updates session status, and publishes each event to SSE subscribers.
6. Clients poll `GET /v1/sessions/:id/events` or tail `GET /v1/sessions/:id/events/stream`.

Turns are **stateless** on the harness side: every call carries the full event history needed for context. The platform is the source of truth for durability.

### Storage layout

| Path | Purpose |
|------|---------|
| `DATABASE_PATH` (default `./data/oma.db`) | SQLite database |
| `SANDBOX_WORKDIR` (default `./data/sandboxes`) | Per-session tool execution directories |

## API overview

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/health` | Liveness check |
| `POST` | `/v1/agents` | Create agent |
| `GET` | `/v1/agents` | List agents |
| `GET` | `/v1/agents/:id` | Get agent |
| `PATCH` | `/v1/agents/:id` | Update agent (new version) |
| `POST` | `/v1/agents/:id/archive` | Archive agent |
| `GET` | `/v1/agents/:id/versions` | List versions |
| `POST` | `/v1/sessions` | Create session |
| `GET` | `/v1/sessions` | List sessions |
| `GET` | `/v1/sessions/:id` | Get session |
| `POST` | `/v1/sessions/:id/events` | Append events / trigger turn |
| `GET` | `/v1/sessions/:id/events` | List events (paginated) |
| `GET` | `/v1/sessions/:id/events/stream` | SSE event stream |
| `POST` | `/v1/environments` | Create environment |
| `GET` | `/v1/environments` | List environments |
| `POST` | `/v1/model_cards` | Create model card |
| `GET` | `/v1/model_cards` | List model cards |

Authenticated routes expect `x-api-key: $OMA_API_KEY` unless `OMA_CONSOLE_DEV=1` (dev only).

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

Or use helper scripts from the repo root:

```bash
./start-harness.sh    # Terminal 1
./start-platform.sh   # Terminal 2
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

Full smoke script: `./smoke-test.sh`

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

**Scope:** Agents, sessions, environments, and model cards work against oma-platform APIs. Vault, skills, runtimes, integrations, evals, and other main-node-only routes return empty-list stubs (P2) so Console pages degrade gracefully; full implementations are deferred.

**Production:** `OMA_CONSOLE_DEV=1` disables API-key checks — dev only. Production console needs better-auth or another browser auth path; until then use API clients with `x-api-key`.

## Docker

```bash
docker compose up --build
```

Copy `.env.example` to `.env`. For real model calls set `OMA_FAKE_HARNESS=0` and configure piPy via `~/.pi/agent/settings.json`, `models.json`, and `auth.json` (mounted into the harness container in compose).

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OMA_LISTEN_ADDR` | `:8787` | Platform HTTP listen address |
| `OMA_API_KEY` | — | API key for `x-api-key` auth |
| `DATABASE_PATH` | `./data/oma.db` | SQLite database path |
| `SANDBOX_WORKDIR` | `./data/sandboxes` | Per-session sandbox root |
| `HARNESS_URL` | `http://127.0.0.1:8090` | Harness sidecar base URL |
| `OMA_FAKE_HARNESS` | — | `1` = in-process fake harness (no Python) |
| `HARNESS_HTTP_TIMEOUT_SEC` | `600` | Platform → harness HTTP timeout |
| `CONSOLE_DIR` | — | Path to built Console `dist/` |
| `OMA_CONSOLE_DEV` | — | `1` = dev auth stubs + relaxed API-key rules |

## Tech stack

- **Platform:** Go 1.22+, chi, modernc.org/sqlite (pure Go, no CGO)
- **Harness:** Python 3.11+, FastAPI, piPy (`pi_coding_agent`)
- **Deploy:** Single static Go binary + Python sidecar; Docker Compose for local/prod-like runs

## Out of scope (MVP)

Cloudflare Workers / SessionDO, multi-tenant routing, Vault, skills marketplace, billing, integrations (Linear/GitHub/Slack), and Postgres storage are deferred. The API surface is a subset of full OMA, designed for SDK compatibility as features land.
