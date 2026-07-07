# oma-platform

> 中文文档：[readme-zh.md](./readme-zh.md)

Self-hostable **Open Managed Agents (OMA)** stack: a Go platform runtime plus a Python piPy harness sidecar. The platform owns durability, concurrency, and the HTTP API; the harness owns the LLM loop and tool execution.

This repo implements a large slice of the [open-managed-agents](https://github.com/open-ma/open-managed-agents) contract for self-hosted deployments. As of 2026-07-07, **~91% feature parity** (44/52 domains aligned). See [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md) for the full matrix and backlog.

## Features

### Core agent loop

- **Versioned agents** — CRUD, archive, immutable version snapshots (`/v1/agents`).
- **Durable sessions** — Append-only event log in SQLite; sessions pin agent version and environment at creation.
- **Harness turns** — Stateless LLM turns via the piPy sidecar (`POST /internal/turn`).
- **Real-time streaming** — Server-Sent Events (`GET /v1/sessions/:id/events/stream`) with optional replay.
- **Turn interruption** — `user.interrupt` cancels the active harness turn (optionally scoped by `session_thread_id`), clears HITL pending state, and returns the session to idle. Console Stop button supported.
- **Crash recovery** — Orphan `running` sessions reset to `idle` on platform startup.
- **Per-session sandboxes** — Isolated workdirs under `SANDBOX_WORKDIR/<session_id>/`.
- **Agent toolset** — OMA `agent_toolset_20260401` maps to piPy builtins plus `web_fetch` and `web_search`.
- **Custom tools + HITL** — Agent-declared custom tools with `requires_action` / `user.custom_tool_result` human-in-the-loop flow ([design doc](./docs/design/loop-task-termination.md)).
- **Session resources** — Mount skills, files, and other resources at session creation or mid-session (`/v1/sessions/:id/resources`).
- **Sub-agents** — `call_agent_*` and `general_subagent` delegation with session threads ([design doc](./docs/design/subagent.md)).
- **Agent teams** — Multi-agent coordination via `team_*` harness tools and `/v1/sessions/:id/teams/*` ([design doc](./docs/design/agent-team.md)).
- **Compaction** — Context summarization before long turns (`harness/oma_adapter/compaction.py`).
- **Schedule tools** — `schedule`, `cancel_schedule`, `list_schedules` with SQLite-backed worker.
- **MCP tools** — Agent-declared MCP servers via harness loader + `/v1/mcp-proxy`.
- **Resource mounter + outcome evaluator** — Session resource mounting and rubric-based outcome grading ([design doc](./docs/design/resource-mounter-and-outcome-evaluator.md)).

### Platform APIs (Console-aligned)

- **Environments** — Named execution contexts; default `env-local-default` on boot.
- **Model cards** — Per-tenant credentials; resolved at turn time; internal key endpoints for the harness.
- **Skills** — Builtin catalog + custom skills with zip/file upload (`/v1/skills`).
- **Files** — Upload/download blobs scoped to sessions (`/v1/files`).
- **Vaults & credentials** — Secret storage with OAuth refresh; outbound HTTP proxy injects credentials.
- **Session aux** — Threads (derived from events), pending confirmations, trajectory export, outputs.
- **Stats & identity** — `/v1/stats`, `/v1/me`, `/v1/api_keys`.
- **Integrations** — Linear, GitHub, and Slack publications, OAuth, install proxy, and webhook dispatch.
- **Eval runs** — CRUD plus background worker (`internal/eval/worker.go`).
- **Dreams** — CRUD plus background dream worker (`internal/dream/worker.go`).
- **Cost report** — Usage and cost aggregation (`/v1/cost_report`).
- **Runtimes** — ACP daemon connect/exchange for local IDE attach ([design doc](./docs/design/runtime-architecture.md)).
- **Memory stores** — Large-object storage with retention worker.

### Python SDK

- **`oma-sdk` v0.1.0** — Python client in `sdk/` using `anthropic` SDK with custom `base_url` for managed-agents resources plus `httpx` for OMA-only endpoints.
- **Cookbook parity** — Managed Agents Cookbook examples 1–9 under `sdk/example/` with shared helpers in `oma_sdk.cookbook`.
- **E2E tests** — pytest suite in `sdk/tests/` validates the full API surface against a running server.

### Operations

- **Console UI** — SPA in `console/`, same origin as the API.
- **Auth** — API key (`x-api-key` / `Authorization: Bearer`) or better-auth cookie session.
- **Docker Compose** — Three-service stack (`oma-platform` + `oma-auth` + `oma-harness`) with health checks.
- **Fake harness mode** — `OMA_FAKE_HARNESS=1` for local dev and CI without LLM API keys.
- **Smoke & integration scripts** — `scripts/e2e/smoke-all.sh` (full suite), `scripts/e2e/smoke-test.sh`, `scripts/e2e/console-integration.sh`, provider webhooks, MCP, runtime, sub-agent E2E.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Client (Console / curl / SDK)                        │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ HTTP + SSE
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-server (Go)                                           :8787        │
│  agents · sessions · vaults · skills · files · model_cards              │
│  integrations · eval worker · dream worker · runtimes · memory_stores   │
│  teams · session resources · custom tool HITL                           │
│  mcp-proxy · outbound-proxy · internal API · Console SPA                │
│  session.Registry + stream.Hub (SSE)                                    │
├─────────────────────────────────────────────────────────────────────────┤
│  Storage: SQLite (oma.db) + local filesystem                            │
│    sandboxes/ · skills/ · files/ · memory/ · session-outputs/           │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ POST /internal/turn
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-harness (Python / FastAPI)                            :8090        │
│  turn · tools · compaction · web_fetch · web_search · mcp_loader        │
│  call_agent · custom tools · team_* · outcome supervisor                │
│  Tools execute in $SANDBOX_WORKDIR/<session_id>/                        │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│  oma-sdk (Python, optional client)                                      │
│  anthropic base_url + httpx OMA-only resources · cookbook helpers       │
└─────────────────────────────────────────────────────────────────────────┘
```

### Component responsibilities

| Layer | Component | Responsibility |
|-------|-----------|----------------|
| **Platform (Go)** | `cmd/oma-server` | Process entrypoint; wires DB, workers, harness client, HTTP server. |
| | `internal/api` | REST routes, auth, integrations, console stubs. |
| | `internal/store` | SQLite persistence, migrations, repositories. |
| | `internal/session` | Turn state machine, per-session async queue, interrupt handling. |
| | `internal/stream` | In-memory SSE pub/sub per session. |
| | `internal/workdir` | Per-session sandbox directories. |
| | `internal/modelresolve` | Resolve agent model strings to model-card credentials. |
| | `internal/harness` | HTTP client to the Python sidecar (or `FakeClient` in dev). |
| | `internal/outbound` | Vault credential injection for sandbox HTTP. |
| | `internal/eval` | Background eval-run worker. |
| | `internal/dream` | Background dream worker. |
| | `internal/memory` | Memory retention cron worker. |
| | `internal/runtime` | Runtime room registry for ACP daemon. |
| | `internal/integrations/*` | Linear, GitHub, Slack gateway handlers. |
| **Harness (Python)** | `harness/oma_adapter` | FastAPI adapter over piPy `create_agent_session`. |
| | `turn.py` | Stateless turn: project events → run prompt → emit OMA events. |
| | `tools.py` | Map OMA tool declarations to piPy builtin/extension names. |
| | `custom_tools.py` | Custom tool execution and HITL `requires_action` flow. |
| | `compaction.py` | Pre-turn context compaction. |
| | `call_agent/` | Sub-agent delegation runtime. |
| | `extensions/` | `web_fetch`, `web_search`, `mcp_loader`, `call_agent` piPy extensions. |
| **SDK (Python)** | `sdk/oma_sdk` | `OMAClient` + resource classes; cookbook streaming helpers. |

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
| `SKILLS_DATA_DIR` (default `./data/skills`) | Skill file blobs |
| `FILES_DATA_DIR` (default `./data/files`) | Uploaded file blobs |
| `MEMORY_DATA_DIR` (default `./data/memory`) | Memory store large objects |
| `SESSION_OUTPUTS_DIR` (default `./data/session-outputs`) | Session output artifacts |
| `AUTH_DATABASE_PATH` (default `./data/auth.db`) | better-auth SQLite database |

## API overview

### Core

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
| `GET` | `/v1/sessions/:id/threads` | Session threads (sub-agents) |
| `GET` | `/v1/sessions/:id/pending` | Pending tool confirmations |
| `GET` | `/v1/sessions/:id/trajectory` | Trajectory export |
| `GET` | `/v1/sessions/:id/outputs` | Session output files |
| `GET` / `POST` / `DELETE` | `/v1/sessions/:id/resources` | Session resource mount CRUD |
| `GET` / `POST` | `/v1/sessions/:id/teams/*` | Agent team coordination |

### Platform resources

| Method | Path | Description |
|--------|------|-------------|
| `POST` / `GET` | `/v1/environments` | Environments |
| `POST` / `GET` | `/v1/model_cards` | Model cards |
| `GET` | `/v1/models/list` | Provider model list |
| `POST` / `GET` | `/v1/skills` | Skills |
| `POST` / `GET` | `/v1/files` | File blobs |
| `POST` / `GET` | `/v1/vaults` | Vaults and credentials |
| `GET` | `/v1/stats` | Tenant stats |
| `GET` | `/v1/me` | Current user / tenant |
| `POST` / `GET` | `/v1/api_keys` | API keys |
| `POST` / `GET` | `/v1/runtimes` | Runtimes |
| `POST` / `GET` | `/v1/memory_stores` | Memory stores |
| `POST` / `GET` | `/v1/evals/runs` | Eval runs |
| `POST` / `GET` | `/v1/dreams` | Dreams |
| `GET` | `/v1/cost_report` | Usage and cost report |
| `GET` | `/v1/integrations/*` | Integration publications |

Authenticated routes accept `x-api-key: $OMA_API_KEY`, `Authorization: Bearer $OMA_API_KEY`, or a better-auth cookie session. Set `AUTH_DISABLED=1` only for API-key-free local dev (not production).

## Quick start (local)

Prerequisites: workspace Go toolchain at `../.tools/go` (used by helper scripts), Python 3.11+ with [uv](https://docs.astral.sh/uv/) for the harness.

```bash
cp .env.example .env

# Terminal 1 — harness (fake mode, no API key)
./start-harness.sh

# Terminal 2 — platform (API only)
source scripts/go-env.sh
export OMA_FAKE_HARNESS=1
export HARNESS_URL=http://127.0.0.1:8090
export OMA_API_KEY=dev-key
go run ./cmd/oma-server/
```

For Console + auth sidecar:

```bash
# Terminal 1
./start-harness.sh

# Terminal 2 — builds console dist if missing, starts auth sidecar
./start-console.sh
```

Open http://localhost:8787

## Python SDK

The SDK lives in `sdk/` and ships as `oma-sdk` v0.1.0 (local install only; not yet on PyPI).

```bash
# With platform running on :8787
cd sdk
uv sync
export OMA_API_KEY=dev-key

# Run an example (requires real LLM for full output)
uv run python example/example1/data_analyst_agent.py

# Run SDK E2E tests against a live server
uv run pytest tests/ -x
```

```python
from oma_sdk import OMAClient

client = OMAClient()  # defaults to http://localhost:8787
agent = client.agents.create(name="hello", model={"id": "claude-sonnet-4-6"})
session = client.sessions.create(agent=agent.id)
```

See [sdk/SDK-PLAN.md](./sdk/SDK-PLAN.md) and [sdk/example/README.md](./sdk/example/README.md) for resource coverage and cookbook examples.

## Verification

### Smoke test (full P1+P2 API + optional real LLM)

```bash
# With platform + harness running (OMA_FAKE_HARNESS=0 for real LLM)
./scripts/e2e/smoke-test.sh

# API-only, no harness / no LLM
SMOKE_SKIP_LLM=1 ./scripts/e2e/smoke-test.sh

# Full E2E suite (core + integrations + runtime + dreams)
./scripts/e2e/smoke-all.sh
```

Set `ANTHROPIC_API_KEY` in `.env` or configure piPy via `~/.pi/agent/{settings,models,auth}.json` for real model calls.

### Other scripts

| Script | Purpose |
|--------|---------|
| `scripts/e2e/smoke-all.sh` | Run all smoke suites (core + noncore) |
| `scripts/e2e/console-integration.sh` | Console wire-shape integration tests |
| `scripts/e2e/smoke-mcp-e2e.sh` | MCP proxy + harness MCP loader |
| `scripts/e2e/smoke-web-search-e2e.sh` | web_search tool (DDG / Tavily) |
| `scripts/e2e/smoke-schedule-e2e.sh` | Schedule / cancel / list tools |
| `scripts/e2e/smoke-resource-outcome-e2e.sh` | Resource mounter + outcome evaluator |
| `scripts/e2e/smoke-dreams-e2e.sh` | Dreams API + dream worker |
| `scripts/multi-agent/smoke-subagent-e2e.sh` | Sub-agent delegation E2E |
| `scripts/e2e/smoke-runtime-e2e.sh` | Runtime / ACP daemon |
| `scripts/e2e/smoke-linear-webhook.sh` | Linear webhook dispatch |
| `scripts/e2e/smoke-github-webhook.sh` | GitHub webhook dispatch |
| `scripts/e2e/smoke-slack-webhook.sh` | Slack webhook dispatch |

## Console UI

The OMA Console SPA in `console/` is served on the same port as the API when `CONSOLE_DIR` is set. `./start-console.sh` builds `console/dist/` if missing, starts the better-auth sidecar, and proxies `/auth/*` for email/password sign-in.

**Docker:** `./deploy/docker.sh up` can mount `./console/dist` at `/app/console` when present. Build the console first (`./scripts/build-console.sh`), or set `CONSOLE_DIST` in compose.

**Coverage:** Agents, sessions, environments, model cards, skills, vaults, files, integrations, evals, dreams, runtimes, and memory stores are wired to oma-platform APIs. Browser tools, `/v1/cap-cli/oauth` (Vault CLI tab), and some CF-only features remain deferred — see [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md).

## Docker

```bash
./deploy/docker.sh up
```

Copy `.env.example` to `.env`. For remote Docker hosts set `PUBLIC_BASE_URL` to the browser-facing URL (e.g. `http://124.221.28.203:8787`) and set `BETTER_AUTH_SECRET`. For real model calls set `OMA_FAKE_HARNESS=0` and configure piPy via `~/.pi/agent/settings.json`, `models.json`, and `auth.json` (mounted into the harness container in compose).

Compose mounts a shared `/data` volume for `SESSION_OUTPUTS_DIR`, `FILES_DATA_DIR`, and other artifact paths so the platform and harness containers see the same files. If agent writes to `/mnt/session/outputs/` but `files.list(scope_id=session.id)` shows no `out:` files, the containers likely have mismatched data dirs — rerun `./deploy/docker.sh up --build`.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OMA_LISTEN_ADDR` | `:8787` | Platform HTTP listen address |
| `OMA_API_KEY` | — | API key for `x-api-key` / Bearer auth |
| `DATABASE_PATH` | `./data/oma.db` | SQLite database path |
| `SANDBOX_WORKDIR` | `./data/sandboxes` | Per-session sandbox root |
| `SKILLS_DATA_DIR` | `./data/skills` | Skill file storage |
| `FILES_DATA_DIR` | `./data/files` | File blob storage |
| `MEMORY_DATA_DIR` | `./data/memory` | Memory large-object storage |
| `SESSION_OUTPUTS_DIR` | `./data/session-outputs` | Session output artifacts |
| `HARNESS_URL` | `http://127.0.0.1:8090` | Harness sidecar base URL |
| `OMA_FAKE_HARNESS` | — | `1` = in-process fake harness (no Python) |
| `HARNESS_HTTP_TIMEOUT_SEC` | `600` | Platform → harness HTTP timeout |
| `OMA_PUBLIC_URL` | `http://127.0.0.1:8787` | Public URL for MCP proxy and integrations |
| `OMA_INTERNAL_SECRET` | — | Shared secret for `/v1/internal/*` and harness key fetch |
| `OMA_OUTBOUND_PROXY_ADDR` | `:8790` | Vault outbound HTTP proxy listen address |
| `OMA_EVAL_WORKER_DISABLED` | — | `1` = disable eval background worker |
| `OMA_MEMORY_RETENTION_DISABLED` | — | `1` = disable memory retention worker |
| `CONSOLE_DIR` | — | Path to built Console `dist/` |
| `AUTH_DISABLED` | `0` | `1` = skip auth + stub `/auth/get-session` (dev only) |
| `AUTH_UPSTREAM_URL` | `http://127.0.0.1:8788` | better-auth sidecar base URL |
| `AUTH_DATABASE_PATH` | `./data/auth.db` | better-auth SQLite database |
| `BETTER_AUTH_SECRET` | — | Cookie signing secret (required in prod) |
| `PUBLIC_BASE_URL` | `http://127.0.0.1:8787` | Public origin for auth cookies |
| `ANTHROPIC_API_KEY` | — | Fallback when no model card matches agent model |

See `.env.example` for smoke-test and OAuth-related variables.

## Design docs

| Doc | Topic |
|-----|-------|
| [docs/design/streaming-turn-and-sse.md](./docs/design/streaming-turn-and-sse.md) | Turn lifecycle and SSE |
| [docs/design/loop-task-termination.md](./docs/design/loop-task-termination.md) | Custom tools, HITL, and interrupt |
| [docs/design/subagent.md](./docs/design/subagent.md) | Sub-agent delegation |
| [docs/design/agent-team.md](./docs/design/agent-team.md) | Agent team coordination |
| [docs/design/session-threads.md](./docs/design/session-threads.md) | Session threads |
| [docs/design/mcp-architecture.md](./docs/design/mcp-architecture.md) | MCP proxy and loader |
| [docs/design/vault-and-credentials.md](./docs/design/vault-and-credentials.md) | Vaults and outbound proxy |
| [docs/design/resource-mounter-and-outcome-evaluator.md](./docs/design/resource-mounter-and-outcome-evaluator.md) | Resource mounter and outcome grading |
| [docs/design/runtime-architecture.md](./docs/design/runtime-architecture.md) | Runtimes and ACP daemon |
| [docs/design/eval-run-background-worker.md](./docs/design/eval-run-background-worker.md) | Eval worker |
| [docs/design/schedule-session-wakeup.md](./docs/design/schedule-session-wakeup.md) | Schedule tools and session wakeup |

## Tech stack

- **Platform:** Go 1.22+, chi, modernc.org/sqlite (pure Go, no CGO)
- **Harness:** Python 3.11+, FastAPI, piPy (`pi_coding_agent`)
- **SDK:** Python 3.11+, anthropic SDK 0.111+ with custom `base_url`, httpx
- **Deploy:** Single static Go binary + Python sidecar; Docker Compose for local/prod-like runs

## Still deferred

Cloudflare Workers / SessionDO, CF Container sandboxes, R2/FUSE memory, Analytics Engine billing, **browser tools** (T16), multi-region D1 sharding, **`/v1/cap-cli/oauth`**, integration install → vault dual-write, and **TypeScript SDK / `oma` CLI** packages remain out of scope or partial. The **Python SDK** (`oma-sdk` v0.1.0) is available locally in `sdk/` but not yet published to PyPI. See [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md) for the full parity matrix and backlog.
