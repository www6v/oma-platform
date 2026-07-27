# oma-platform

> 中文文档：[readme-zh.md](./readme-zh.md)

Self-hostable **Open Managed Agents (OMA)** stack: a Go platform runtime plus a Python piPy harness sidecar. The platform owns durability, concurrency, and the HTTP API; the harness owns the LLM loop and tool execution.

## Contents

- [Features](#features)
- [Deploy](#deploy)
- [Architecture](#architecture)
- [API](#api)
- [Python SDK](#python-sdk)
- [Console UI](#console-ui)
- [Configuration](#configuration)
- [Design docs](#design-docs)
- [Tech stack](#tech-stack)
- [Still deferred](#still-deferred)
- [License](#license)

## Features

### Core agent loop

- **Versioned agents** — CRUD, archive, immutable version snapshots (`/v1/agents`).
- **Durable sessions** — Append-only event log in MySQL; sessions pin agent version and environment at creation.
- **Harness turns** — Stateless LLM turns via the piPy sidecar (`POST /internal/turn`).
- **Real-time streaming** — Server-Sent Events (`GET /v1/sessions/:id/events/stream`) with optional replay.
- **Turn interruption** — `user.interrupt` cancels the active harness turn (optionally scoped by `session_thread_id`), clears HITL pending state, and returns the session to idle. Console Stop button supported.
- **Crash recovery** — Orphan `running` sessions reset to `idle` on platform startup.
- **Pluggable sandboxes** — Providers `local` | `litebox` | `boxrun` | `e2b` | `daytona` | `opensandbox` via `SANDBOX_PROVIDER`; per-environment binding; session exec (`POST /v1/sessions/:id/exec`) and file promote ([design](./docs/design/env/sandbox.md), [binding](./docs/design/environment-sandbox-binding.md)).
- **Agent toolset** — OMA `agent_toolset_20260401` maps to piPy builtins plus `web_fetch` and `web_search`.
- **Custom tools + HITL** — Agent-declared custom tools with `requires_action` / `user.custom_tool_result` human-in-the-loop flow ([design doc](./docs/design/loop-task-termination.md)).
- **Session resources** — Mount skills, files, and other resources at session creation or mid-session (`/v1/sessions/:id/resources`).
- **Sub-agents** — `call_agent_*` and `general_subagent` delegation with session threads ([design doc](./docs/design/orch/subagent.md)).
- **Agent teams** — Multi-agent coordination via `team_*` harness tools and `/v1/sessions/:id/teams/*` ([design doc](./docs/design/orch/agent-team.md)).
- **Dynamic workflows** — YAML workflows (NL generation) via the harness `pi_dynamic_workflows` extension; platform proxies `/api/workflows` to the sidecar; steps run as sub-agents on sessions with execution traces visible in Session (Console plugin `console/src/plugins/dynamic-workflows/`; extension [`../piPy-dynamic-workflows/`](../piPy-dynamic-workflows/)).
- **Compaction** — Context summarization before long turns (`harness/oma_adapter/compaction.py`).
- **Schedule tools** — `schedule`, `cancel_schedule`, `list_schedules` with MySQL-backed wakeup worker.
- **MCP tools** — Agent-declared MCP servers via harness loader + `/v1/mcp-proxy`.
- **Resource mounter + outcome evaluator** — Session resource mounting and rubric-based outcome grading ([design doc](./docs/design/env/resource-mounter-and-outcome-evaluator.md)).

### Platform APIs (Console-aligned)

- **Environments** — Named execution contexts with optional sandbox config; default `env-local-default` on boot.
- **Model cards** — Per-tenant credentials; resolved at turn time; internal key endpoints for the harness.
- **Skills** — Builtin catalog + custom skills with zip/file upload (`/v1/skills`); ClawHub import (`/v1/clawhub`).
- **Files** — Upload/download blobs scoped to sessions (`/v1/files`).
- **Vaults & credentials** — Secret storage with OAuth refresh; outbound HTTP proxy injects credentials.
- **Session aux** — Threads (derived from events), pending confirmations, trajectory export, outputs, sandbox exec.
- **Stats & identity** — `/v1/stats`, `/v1/me`, `/v1/api_keys`.
- **Integrations** — Linear, GitHub, and Slack publications, OAuth, install proxy, and webhook dispatch.
- **Eval runs** — CRUD plus background worker (`internal/eval/worker.go`).
- **Dreams** — CRUD plus background dream worker (`internal/dream/worker.go`).
- **Cost report** — Usage and cost aggregation (`/v1/cost_report`).
- **Runtimes** — ACP daemon connect/exchange for local IDE attach ([design doc](./docs/design/runtime-architecture.md)).
- **Memory stores** — Large-object storage with retention worker ([design doc](./docs/design/memory/memory.md)).
- **Managed harnesses** — Optional OpenClaw / Hermes backends via agent `_oma.harness: "managed"`; availability at `GET /v1/config/harnesses` (`OMA_OPENCLAW_*` / `OMA_HERMES_*`).

### Python SDK

- **`oma-sdk` v0.1.0** — Python client in sibling [`../oma-sdk/`](../oma-sdk/) using `anthropic` SDK with custom `base_url` for managed-agents resources plus `httpx` for OMA-only endpoints.
- **Cookbook parity** — Managed Agents Cookbook examples 1–9 under `oma-sdk/example/` with shared helpers in `oma_sdk.cookbook`.
- **E2E tests** — pytest suite in `oma-sdk/tests/` validates the full API surface against a running server.

### Operations

- **Console UI** — SPA in `console/`, same origin as the API (includes Workflows Quickstart/Editor).
- **Auth** — API key (`x-api-key` / `Authorization: Bearer`) or better-auth cookie session.
- **Docker Compose** — Three-service stack (`oma-platform` + `oma-auth` + `oma-harness`) with health checks.
- **Fake harness mode** — `OMA_FAKE_HARNESS=1` for local dev and CI without LLM API keys.
- **Smoke & integration scripts** — `scripts/e2e/smoke-all.sh` (full suite), workflows / team / sandbox / managed-harness smokes under `scripts/workflows/`, `scripts/multi-agent/`, and `scripts/e2e/`.

## Deploy

### Local

Prerequisites: workspace Go toolchain at `../.tools/go` (used by helper scripts), Python 3.11+ with [uv](https://docs.astral.sh/uv/) for the harness, and a reachable MySQL instance (`DATABASE_URL` in `.env`).

```bash
cp .env.example .env
# Edit DATABASE_URL to your MySQL DSN / URL

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

### Docker

```bash
./deploy/docker.sh up
```

Copy `.env.example` to `.env`. Set `DATABASE_URL` to a reachable MySQL instance. For remote Docker hosts set `PUBLIC_BASE_URL` to the browser-facing URL (e.g. `http://124.221.28.203:8787`) and set `BETTER_AUTH_SECRET`. For real model calls set `OMA_FAKE_HARNESS=0` and configure piPy via `~/.pi/agent/settings.json`, `models.json`, and `auth.json` (mounted into the harness container in compose).

Compose mounts a shared `/data` volume for `SESSION_OUTPUTS_DIR`, `FILES_DATA_DIR`, and other artifact paths so the platform and harness containers see the same files. If agent writes to `/mnt/session/outputs/` but `files.list(scope_id=session.id)` shows no `out:` files, the containers likely have mismatched data dirs — rerun `./deploy/docker.sh up --build`.

`./deploy/docker.sh up` can mount `./console/dist` at `/app/console` when present. Build the console first (`./scripts/build-console.sh`), or set `CONSOLE_DIST` in compose.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    Client (Console / curl / SDK)                        │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ HTTP + SSE (+ WS for workflows)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-server (Go)                                           :8787        │
│  agents · sessions · vaults · skills · files · model_cards              │
│  integrations · eval worker · dream worker · runtimes · memory_stores   │
│  teams · session resources · custom tool HITL · clawhub                 │
│  workflows proxy (/api/workflows) · mcp-proxy · outbound-proxy          │
│  internal API · Console SPA · session.Registry + stream.Hub (SSE)       │
├─────────────────────────────────────────────────────────────────────────┤
│  Storage: MySQL (DATABASE_URL) + local filesystem                       │
│    sandboxes/ · skills/ · files/ · memory/ · session-outputs/           │
│  Auth DB: SQLite (AUTH_DATABASE_PATH)                                   │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ POST /internal/turn
                                    │ (workflows proxied to harness)
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-harness (Python / FastAPI)                            :8090        │
│  turn · tools · compaction · web_fetch · web_search · mcp_loader        │
│  call_agent · custom tools · team_* · outcome supervisor                │
│  pi_dynamic_workflows · workflow bootstrap / sub-agent runner           │
│  Tools execute via sandbox provider (workdir or remote)                 │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│  oma-sdk (Python, sibling ../oma-sdk/)                                  │
│  anthropic base_url + httpx OMA-only resources · cookbook helpers       │
└─────────────────────────────────────────────────────────────────────────┘
```

### Component responsibilities

| Layer | Component | Responsibility |
|-------|-----------|----------------|
| **Platform (Go)** | `cmd/oma-server` | Process entrypoint; wires DB, workers, harness client, HTTP server. |
| | `internal/api` | REST routes, auth, integrations, workflows proxy, console stubs. |
| | `internal/store` | MySQL persistence and repositories (`DATABASE_URL`). |
| | `internal/session` | Turn state machine, per-session async queue, interrupt handling. |
| | `internal/stream` | In-memory SSE pub/sub per session. |
| | `internal/sandbox` | Pluggable sandbox providers and environment binding. |
| | `internal/workdir` | Per-session sandbox directories (local provider). |
| | `internal/modelresolve` | Resolve agent model strings to model-card credentials. |
| | `internal/harness` | HTTP client to the Python sidecar, FakeClient, or managed OpenClaw/Hermes. |
| | `internal/outbound` | Vault credential injection for sandbox HTTP. |
| | `internal/eval` | Background eval-run worker. |
| | `internal/dream` | Background dream worker. |
| | `internal/memory` | Memory retention cron worker. |
| | `internal/runtime` | Runtime room registry for ACP daemon. |
| | `internal/integrations/*` | Linear, GitHub, Slack gateway handlers. |
| | `workflows_proxy.go` | Reverse-proxy `/api/workflows*` to harness (REST + WebSocket). |
| **Harness (Python)** | `harness/oma_adapter` | FastAPI adapter over piPy `create_agent_session`. |
| | `turn.py` | Stateless turn: project events → run prompt → emit OMA events. |
| | `tools.py` | Map OMA tool declarations to piPy builtin/extension names. |
| | `custom_tools.py` | Custom tool execution and HITL `requires_action` flow. |
| | `compaction.py` | Pre-turn context compaction. |
| | `call_agent/` | Sub-agent delegation runtime. |
| | `workflow_*.py` | OMA bootstrap + sub-agent runner for `pi_dynamic_workflows`. |
| | `extensions/` | `web_fetch`, `web_search`, `mcp_loader`, `call_agent` piPy extensions. |
| **SDK (Python)** | `../oma-sdk/oma_sdk` | `OMAClient` + resource classes; cookbook streaming helpers. |

### Request flow (one user turn)

1. Client `POST /v1/sessions/:id/events` with a `user.message` event.
2. API validates event types, appends to `session_events`, and enqueues a turn on the session registry.
3. Session machine loads history, ensures the session workdir / sandbox, resolves the model card, and calls `POST /internal/turn` on the harness.
4. Harness projects persisted events into piPy messages, runs one in-memory agent session with `cwd=workdir`, and returns new OMA events.
5. Platform persists harness output, updates session status, and publishes each event to SSE subscribers.
6. Clients poll `GET /v1/sessions/:id/events` or tail `GET /v1/sessions/:id/events/stream`.

Turns are **stateless** on the harness side: every call carries the full event history needed for context. The platform is the source of truth for durability.

### Storage layout

| Path / variable | Purpose |
|-----------------|---------|
| `DATABASE_URL` | Platform MySQL DSN / URL (required) |
| `SANDBOX_WORKDIR` (default `./data/sandboxes`) | Per-session tool execution directories (local provider) |
| `SKILLS_DATA_DIR` (default `./data/skills`) | Skill file blobs |
| `FILES_DATA_DIR` (default `./data/files`) | Uploaded file blobs |
| `MEMORY_DATA_DIR` (default `./data/memory`) | Memory store large objects |
| `SESSION_OUTPUTS_DIR` (default `./data/session-outputs`) | Session output artifacts |
| `AUTH_DATABASE_PATH` (default `./data/auth.db`) | better-auth SQLite database |

## API

Compatible with the [Claude Managed Agents API](https://docs.anthropic.com/en/docs/agents/managed-agents). Same endpoints, same event types, works with existing SDKs.

<details>
<summary><strong>Agents</strong> — Create and manage agent configurations</summary>

```http
POST   /v1/agents                          # Create agent
GET    /v1/agents                          # List agents
GET    /v1/agents/:id                      # Get agent
PATCH  /v1/agents/:id                      # Update agent (new version)
POST   /v1/agents/:id/archive              # Archive agent
GET    /v1/agents/:id/versions             # Version history
```

</details>

<details>
<summary><strong>Environments</strong> — Sandbox execution environments</summary>

```http
POST   /v1/environments                    # Create environment
GET    /v1/environments                    # List environments
GET    /v1/environments/:id                # Get environment
PATCH  /v1/environments/:id                # Update environment
DELETE /v1/environments/:id                # Delete environment
```

</details>

<details>
<summary><strong>Sessions</strong> — Run agent conversations</summary>

```http
POST   /v1/sessions                        # Create session
GET    /v1/sessions                        # List sessions
GET    /v1/sessions/:id                    # Get session
POST   /v1/sessions/:id/events             # Send events (user messages)
GET    /v1/sessions/:id/events             # Get events (paginated)
GET    /v1/sessions/:id/events/stream      # SSE stream
GET    /v1/sessions/:id/threads            # Session threads (sub-agents)
GET    /v1/sessions/:id/pending            # Pending tool confirmations
GET    /v1/sessions/:id/trajectory         # Trajectory export
GET    /v1/sessions/:id/outputs            # Session output files
POST   /v1/sessions/:id/exec               # Sandbox exec
GET    /v1/sessions/:id/resources          # List session resources
POST   /v1/sessions/:id/resources          # Attach resource
DELETE /v1/sessions/:id/resources/:resId   # Remove resource
GET    /v1/sessions/:id/teams/*            # Agent team coordination
POST   /v1/sessions/:id/teams/*            # Agent team coordination
```

</details>

<details>
<summary><strong>Vaults</strong> — Secure credential storage</summary>

```http
POST   /v1/vaults                          # Create vault
GET    /v1/vaults                          # List vaults
POST   /v1/vaults/:id/credentials          # Add credential
GET    /v1/vaults/:id/credentials          # List (secrets stripped)
GET    /v1/oauth/*                         # Vault OAuth flows
POST   /v1/oauth/*                         # Vault OAuth flows
```

</details>

<details>
<summary><strong>Memory Stores</strong> — persistent storage; Claude Managed Agents Memory contract</summary>

When attached to a session, each store is mounted into the sandbox at
`/mnt/memory/<store_name>/`. The agent reads and writes it with the
**standard file tools** (bash/read/write/edit/glob/grep) — there are no
bespoke `memory_*` tools.

```http
POST   /v1/memory_stores                   # Create store
GET    /v1/memory_stores                   # List stores
GET    /v1/memory_stores/:id               # Retrieve store
POST   /v1/memory_stores/:id/archive       # Archive (one-way)
DELETE /v1/memory_stores/:id               # Delete store
```

</details>

<details>
<summary><strong>Files & Skills</strong></summary>

```http
POST   /v1/files                           # Upload file
GET    /v1/files                           # List files
GET    /v1/files/:id/content               # Download file
POST   /v1/skills                          # Create skill
GET    /v1/skills                          # List skills
GET    /v1/clawhub/*                       # ClawHub skill search / import
POST   /v1/clawhub/*                       # ClawHub skill search / import
```

</details>

Also available: `GET /health`, model cards, managed harnesses (`GET /v1/config/harnesses`), MCP proxy, workflows (`/api/workflows/*`), evals, dreams, runtimes, cost report, and integrations. Authenticated routes accept `x-api-key: $OMA_API_KEY`, `Authorization: Bearer $OMA_API_KEY`, or a better-auth cookie session. Set `AUTH_DISABLED=1` only for API-key-free local dev (not production).

## Python SDK

The SDK lives in sibling [`../oma-sdk/`](../oma-sdk/) as `oma-sdk` v0.1.0 (local install only; not yet on PyPI).

```bash
# With platform running on :8787
cd ../oma-sdk
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

See [oma-sdk/SDK-PLAN.md](../oma-sdk/SDK-PLAN.md) and [oma-sdk/example/README.md](../oma-sdk/example/README.md) for resource coverage and cookbook examples.

## Console UI

The OMA Console SPA in `console/` is served on the same port as the API when `CONSOLE_DIR` is set. `./start-console.sh` builds `console/dist/` if missing, starts the better-auth sidecar, and proxies `/auth/*` for email/password sign-in.

**Coverage:** Agents, sessions, environments, model cards, skills, vaults, files, integrations, evals, dreams, runtimes, memory stores, and **dynamic workflows** (`/workflows`, plugin `console/src/plugins/dynamic-workflows/`) are wired to oma-platform APIs. Managed harness dropdown (OpenClaw / Hermes) follows `GET /v1/config/harnesses`. Browser tools, `/v1/cap-cli/oauth` (Vault CLI tab), and some CF-only features remain deferred — see [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md).

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OMA_LISTEN_ADDR` | `:8787` | Platform HTTP listen address |
| `OMA_API_KEY` | — | API key for `x-api-key` / Bearer auth |
| `DATABASE_URL` | — | Platform MySQL URL/DSN (required) |
| `SANDBOX_WORKDIR` | `./data/sandboxes` | Per-session sandbox root (local provider) |
| `SANDBOX_PROVIDER` | `local` | `local` \| `litebox` \| `boxrun` \| `e2b` \| `daytona` \| `opensandbox` |
| `SKILLS_DATA_DIR` | `./data/skills` | Skill file storage |
| `FILES_DATA_DIR` | `./data/files` | File blob storage |
| `MEMORY_DATA_DIR` | `./data/memory` | Memory large-object storage |
| `SESSION_OUTPUTS_DIR` | `./data/session-outputs` | Session output artifacts |
| `HARNESS_URL` | `http://127.0.0.1:8090` | Harness sidecar base URL |
| `OMA_FAKE_HARNESS` | — | `1` = in-process fake harness (no Python) |
| `OMA_OPENCLAW_ENABLED` | — | Enable managed OpenClaw harness |
| `OMA_OPENCLAW_GATEWAY_URL` / `OMA_OPENCLAW_TOKEN` | — | OpenClaw Gateway endpoint |
| `OMA_HERMES_ENABLED` | — | Enable managed Hermes harness |
| `OMA_HERMES_GATEWAY_URL` / `OMA_HERMES_API_KEY` | — | Hermes Agent endpoint |
| `HARNESS_HTTP_TIMEOUT_SEC` | `600` | Platform → harness HTTP timeout |
| `OMA_PUBLIC_URL` | `http://127.0.0.1:8787` | Public URL for MCP proxy and integrations |
| `GATEWAY_ORIGIN` | (falls back to `PUBLIC_BASE_URL`) | Slack/GitHub OAuth `redirect_uri` host (same as open-managed-agents) |
| `OMA_HARNESS_PLATFORM_BASE` | — | Harness → platform callback base |
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

See `.env.example` for sandbox provider credentials (OpenSandbox, LiteBox, E2B, Daytona), smoke-test, and OAuth-related variables.

## Design docs

| Doc | Topic |
|-----|-------|
| [docs/design/session/streaming-turn-and-sse.md](./docs/design/session/streaming-turn-and-sse.md) | Turn lifecycle and SSE |
| [docs/design/loop-task-termination.md](./docs/design/loop-task-termination.md) | Custom tools, HITL, and interrupt |
| [docs/design/orch/subagent.md](./docs/design/orch/subagent.md) | Sub-agent delegation |
| [docs/design/orch/agent-team.md](./docs/design/orch/agent-team.md) | Agent team coordination |
| [docs/design/session/session-threads.md](./docs/design/session/session-threads.md) | Session threads |
| [docs/design/mcp-architecture.md](./docs/design/mcp-architecture.md) | MCP proxy and loader |
| [docs/design/vault/vault-and-credentials.md](./docs/design/vault/vault-and-credentials.md) | Vaults and outbound proxy |
| [docs/design/env/resource-mounter-and-outcome-evaluator.md](./docs/design/env/resource-mounter-and-outcome-evaluator.md) | Resource mounter and outcome grading |
| [docs/design/env/sandbox.md](./docs/design/env/sandbox.md) | Sandbox providers |
| [docs/design/env/opensandbox-environment.md](./docs/design/env/opensandbox-environment.md) | OpenSandbox |
| [docs/design/environment-sandbox-binding.md](./docs/design/environment-sandbox-binding.md) | Environment ↔ sandbox binding |
| [docs/design/memory/memory.md](./docs/design/memory/memory.md) | Memory stores |
| [docs/design/runtime-architecture.md](./docs/design/runtime-architecture.md) | Runtimes and ACP daemon |
| [docs/design/eval-run-background-worker.md](./docs/design/eval-run-background-worker.md) | Eval worker |
| [docs/design/schedule-session-wakeup.md](./docs/design/schedule-session-wakeup.md) | Schedule tools and session wakeup |
| [`../piPy-dynamic-workflows/`](../piPy-dynamic-workflows/) | Dynamic workflows extension |

## Tech stack

- **Platform:** Go 1.24+, chi, go-sql-driver/mysql
- **Harness:** Python 3.11+, FastAPI, piPy (`pi_coding_agent`), `pi_dynamic_workflows`
- **SDK:** Python 3.11+, anthropic SDK 0.111+ with custom `base_url`, httpx (sibling `oma-sdk/`)
- **Deploy:** Single static Go binary + Python sidecar; Docker Compose for local/prod-like runs; MySQL for platform data

## Still deferred

Cloudflare Workers / SessionDO, **CF Container** sandboxes (distinct from the pluggable local/OpenSandbox/E2B/… providers above), R2/FUSE memory, Analytics Engine billing, **browser tools** (T16), multi-region D1 sharding, **`/v1/cap-cli/oauth`**, integration install → vault dual-write, and **TypeScript SDK / `oma` CLI** packages remain out of scope or partial. The **Python SDK** (`oma-sdk` v0.1.0) is available locally in [`../oma-sdk/`](../oma-sdk/) but not yet published to PyPI. See [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md) for the full parity matrix and backlog.

## License

[Apache 2.0](https://www.apache.org/licenses/LICENSE-2.0)
