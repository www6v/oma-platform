# OMA Platform Python SDK — Engineering Review & Implementation Plan

> **Status (2026-06-28):** SDK scaffold shipped under `sdk/`. Managed-agents resources route through `anthropic>=0.111.0` with `base_url`; OMA-only resources use `httpx` wrappers. E2E tests cover agents, sessions, environments, memory_stores, vaults, skills, files, misc, subagents.

---

## NOT In Scope

- Migrating TypeScript packages from open-managed-agents to Go
- Full parity for Anthropic-only resources with no OMA use case today: `deployments`, `deployment_runs`, `user_profiles`, `webhooks`, `beta.messages` (direct LLM `/v1/messages` — harness handles inference)
- Self-hosted environment work queue (`environments.work.*`) — OMA uses harness sidecar, not Anthropic self-hosted workers
- CI/CD pipeline changes
- Frontend / console routes (`/v1/console/*`, integration stubs)
- Internal routes (`/v1/internal/*`) and runtime daemon routes (`/agents/runtime/*`)
- OAuth gateway routes (linear, github, slack gateway webhooks)

---

## What Already Exists

| Component | Location | Notes |
|---|---|---|
| Go HTTP server | `oma-platform/cmd/oma-server/main.go` | Listens `:8787`, chi router, SQLite |
| Core API handlers | `internal/api/*.go` | agents, sessions, environments, memory_stores, vaults, skills, dreams, evals, runtimes, integrations, model_cards, files |
| `/v1/oma/*` aliases | `internal/api/oma_aliases.go` | Mirrors select routes under alternate namespace |
| Python SDK | `sdk/oma_sdk/` | `OMAClient` + resource classes + `oma_sdk/api/*` examples |
| SDK tests | `sdk/tests/` | pytest + pytest-asyncio, live E2E against running server |
| Python harness sidecar | `harness/` | uv project, fastapi, httpx |
| anthropic Python SDK | system pip | **0.111.0** — full `beta.*` managed-agents namespace |

---

## Architecture Decisions (from review)

| # | Decision | Choice | Rationale |
|---|---|---|---|
| D1 | HTTP client for Python SDK | Upgrade anthropic SDK | v0.111.0 has full `beta.agents`, `beta.sessions`, etc. |
| D2 | Wire-compat endpoint | `POST /v1/sessions/{id}/messages` | Implemented in `sessions.go` — convenience wrapper over events |
| D3 | Authentication | `OMA_API_KEY` env var | Matches harness convention |
| D4 | SDK package layout | Resource-class layout | anthropic SDK for managed agents; httpx for OMA-only |
| D5 | Test coverage scope | All oma-platform endpoints | E2E tests validate API surface |

### Key Architectural Insight

```python
anthropic.Anthropic(api_key=OMA_API_KEY, base_url="http://localhost:8787")
```

All `client.beta.{agents,sessions,environments,memory_stores,vaults,skills}` calls hit oma-platform when `base_url` is set. OMA-only resources (`dreams`, `evals`, `files`, …) use `OMAClient`'s shared `httpx.AsyncClient`.

---

## Anthropic SDK ↔ OMA Platform Gap Analysis

**Reference:** anthropic Python SDK **0.111.0** (`client.beta.*`) vs oma-platform routes in `internal/api/router.go`.

**Legend**

| Symbol | Meaning |
|---|---|
| ✅ | Implemented and wire-compatible |
| ⚠️ | Partial — works for common paths but schema/behavior differs |
| ❌ | Not implemented on oma-platform |
| ➕ | OMA-only — no anthropic SDK equivalent |
| 🔌 | SDK method exists but OMA SDK uses httpx instead (by design) |

---

### 1. Resource-Level Summary

| Anthropic SDK resource | OMA route prefix | OMA status | OMA SDK binding | Gap severity |
|---|---|---|---|---|
| `beta.agents` | `/v1/agents` | ✅ Core CRUD + versions | `client.agents` (anthropic) | Low — see method table |
| `beta.sessions` | `/v1/sessions` | ⚠️ Events OK; resources/threads/update gaps | `client.sessions` (anthropic) | **Medium** |
| `beta.environments` | `/v1/environments` | ⚠️ CRUD OK; no delete/work | `client.environments` (anthropic) | Low–Medium |
| `beta.memory_stores` | `/v1/memory_stores` | ✅ Full CRUD + memories + versions | `client.memory_stores` (anthropic) | Low |
| `beta.vaults` | `/v1/vaults` | ✅ Full CRUD + credentials | `client.vaults` (anthropic) | Low |
| `beta.skills` | `/v1/skills` | ⚠️ CRUD OK; download path differs | `client.skills` (anthropic) | Low |
| `beta.files` | `/v1/files` | ✅ Implemented (console mount) | `client.files` (httpx) 🔌 | Medium — SDK path mismatch |
| `beta.models` | `/v1/models` | ⚠️ Different API shape | `client.models` (httpx) 🔌 | Medium — not Anthropic catalog |
| `beta.deployments` | — | ❌ | — | Out of scope |
| `beta.deployment_runs` | — | ❌ | — | Out of scope |
| `beta.user_profiles` | — | ❌ | — | Out of scope |
| `beta.webhooks` | — | ❌ (client-side unwrap only) | — | Out of scope |
| `beta.messages` | — | ❌ (harness `/internal/turn`) | — | Out of scope |
| — | `/v1/dreams` | ➕ | `client.dreams` (httpx) | OMA extension |
| — | `/v1/evals` | ➕ | `client.evals` (httpx) | OMA extension |
| — | `/v1/runtimes` | ➕ | `client.runtimes` (httpx) | OMA extension |
| — | `/v1/integrations` | ➕ | `client.integrations` (httpx) | OMA extension |
| — | `/v1/model_cards` | ➕ | `client.model_cards` (httpx) | OMA extension |
| — | `/v1/cost_report` | ➕ | `client.cost_report` (httpx) | OMA extension |
| — | `/v1/me`, `/v1/api_keys` | ➕ | `client.me`, `client.api_keys` | OMA extension |
| — | `/v1/stats` | ➕ | — | Console-only |
| — | `/v1/tenants` | ➕ | via `client.me` | OMA extension |
| — | `/v1/oauth`, `/v1/clawhub` | ➕ | — | Console / install flows |
| — | `/v1/sessions/{id}/teams/*` | ➕ | `oma_sdk/subagent.py` | OMA multi-agent |
| — | `/v1/sessions/{id}/trajectory` | ➕ | subagent helpers | OMA extension |
| — | `/v1/sessions/{id}/outputs/*` | ➕ | — | Session artifact download |
| — | `POST /v1/sessions/{id}/messages` | ➕ | raw httpx / server handler | OMA convenience (not in SDK) |

**Counts (method-level, managed-agents subset):**

| Category | Count |
|---|---|
| SDK methods with full OMA parity | ~62 |
| SDK methods partial on OMA | ~12 |
| SDK methods missing on OMA (in-scope) | ~18 |
| SDK resources entirely missing (out-of-scope) | 5 resources (~25 methods) |
| OMA-only route groups | 14 |

---

### 2. Detailed Method Comparison — Managed Agents (anthropic SDK → OMA)

#### 2.1 `beta.agents`

| SDK method | HTTP (anthropic 0.111.0) | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/agents?beta=true` | `agents.go` POST `/` | ✅ | |
| `list` | `GET /v1/agents?beta=true` | `agents.go` GET `/` | ✅ | Supports `include_archived`, cursor pagination |
| `retrieve` | `GET /v1/agents/{id}?beta=true&version=N` | GET `/{id}` + GET `/{id}/versions/{version}` | ✅ | Version via query param or dedicated version route |
| `update` | `POST/PATCH /v1/agents/{id}?beta=true` | PATCH/PUT/POST `/{id}` | ✅ | Requires `version` in body |
| `archive` | `POST /v1/agents/{id}/archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `versions.list` | `GET /v1/agents/{id}/versions?beta=true` | GET `/{id}/versions` | ✅ | |
| — | — | `DELETE /v1/agents/{id}` | ➕ | OMA hard-delete; SDK has no `delete`, only `archive` |

#### 2.2 `beta.sessions`

| SDK method | HTTP (anthropic 0.111.0) | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/sessions?beta=true` | `sessions.go` POST `/` | ✅ | |
| `list` | `GET /v1/sessions?beta=true` | GET `/` | ✅ | Filter params partially supported |
| `retrieve` | `GET /v1/sessions/{id}?beta=true` | GET `/{id}` | ✅ | |
| `update` | `POST /v1/sessions/{id}?beta=true` | — | ❌ | **P1 gap** — mid-session agent/tools/metadata patch |
| `delete` | `DELETE /v1/sessions/{id}?beta=true` | DELETE `/{id}` | ✅ | |
| `archive` | `POST /v1/sessions/{id}/archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `events.send` | `POST /v1/sessions/{id}/events?beta=true` | POST `/{id}/events` | ✅ | |
| `events.list` | `GET /v1/sessions/{id}/events?beta=true` | GET `/{id}/events` | ✅ | |
| `events.stream` | `GET /v1/sessions/{id}/events/stream?beta=true` | GET `/{id}/events/stream` | ✅ | SSE |
| `resources.list` | `GET .../resources?beta=true` | — | ❌ | **P2 gap** |
| `resources.add` | `POST .../resources?beta=true` | — | ❌ | **P2 gap** |
| `resources.retrieve` | `GET .../resources/{rid}?beta=true` | — | ❌ | **P2 gap** |
| `resources.update` | `POST .../resources/{rid}?beta=true` | — | ❌ | **P2 gap** |
| `resources.delete` | `DELETE .../resources/{rid}?beta=true` | — | ❌ | **P2 gap** |
| `threads.list` | `GET .../threads?beta=true` | GET `/{id}/threads` | ⚠️ | OMA derives threads from events; no cursor pagination |
| `threads.retrieve` | `GET .../threads/{tid}?beta=true` | — | ❌ | **P2 gap** |
| `threads.archive` | `POST .../threads/{tid}/archive?beta=true` | — | ❌ | **P3 gap** |
| `threads.events.list` | `GET .../threads/{tid}/events?beta=true` | — | ❌ | **P2 gap** — use session-level events + filter |
| `threads.events.stream` | `GET .../threads/{tid}/stream?beta=true` | — | ❌ | **P2 gap** |
| — | — | `POST /{id}/messages` | ➕ | OMA convenience endpoint; **not in anthropic SDK** — wraps `events.send` |

#### 2.3 `beta.environments`

| SDK method | HTTP | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/environments?beta=true` | `environments.go` POST `/` | ✅ | |
| `list` | `GET /v1/environments?beta=true` | GET `/` | ✅ | |
| `retrieve` | `GET /v1/environments/{id}?beta=true` | GET `/{id}` | ✅ | |
| `update` | `POST/PUT /v1/environments/{id}?beta=true` | PUT/POST `/{id}` | ✅ | |
| `archive` | `POST .../archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `delete` | `DELETE /v1/environments/{id}?beta=true` | — | ❌ | **P3 gap** — archive is sufficient for most flows |
| `work.poll` | `GET .../work/poll?beta=true` | — | ❌ | Out of scope (self-hosted workers) |
| `work.list` | `GET .../work?beta=true` | — | ❌ | Out of scope |
| `work.retrieve` | `GET .../work/{id}?beta=true` | — | ❌ | Out of scope |
| `work.ack` | `POST .../work/{id}/ack?beta=true` | — | ❌ | Out of scope |
| `work.heartbeat` | `POST .../work/{id}/heartbeat?beta=true` | — | ❌ | Out of scope |
| `work.stop` | `POST .../work/{id}/stop?beta=true` | — | ❌ | Out of scope |
| `work.stats` | `GET .../work/stats?beta=true` | — | ❌ | Out of scope |
| `work.update` | `POST .../work/{id}?beta=true` | — | ❌ | Out of scope |

#### 2.4 `beta.memory_stores`

| SDK method | HTTP | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/memory_stores?beta=true` | `memory_stores.go` | ✅ | |
| `list` | `GET /v1/memory_stores?beta=true` | GET `/` | ✅ | |
| `retrieve` | `GET .../{id}?beta=true` | GET `/{id}` | ✅ | |
| `update` | `POST .../{id}?beta=true` | PUT/POST `/{id}` | ✅ | |
| `archive` | `POST .../archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `delete` | `DELETE .../{id}?beta=true` | DELETE `/{id}` | ✅ | |
| `memories.create` | `POST .../memories?beta=true` | POST `/{id}/memories` | ✅ | |
| `memories.list` | `GET .../memories?beta=true` | GET `/{id}/memories` | ✅ | |
| `memories.retrieve` | `GET .../memories/{mid}?beta=true` | GET `/{id}/memories/{mid}` | ✅ | |
| `memories.update` | `POST/PATCH .../memories/{mid}?beta=true` | PATCH/POST `/{id}/memories/{mid}` | ✅ | Precondition support partial |
| `memories.delete` | `DELETE .../memories/{mid}?beta=true` | DELETE `/{id}/memories/{mid}` | ✅ | |
| `memory_versions.list` | `GET .../memory_versions?beta=true` | GET `/{id}/memory_versions` | ✅ | |
| `memory_versions.retrieve` | `GET .../memory_versions/{vid}?beta=true` | GET `/{id}/memory_versions/{vid}` | ✅ | |
| `memory_versions.redact` | `POST .../memory_versions/{vid}/redact?beta=true` | POST `/{id}/memory_versions/{vid}/redact` | ✅ | |

#### 2.5 `beta.vaults`

| SDK method | HTTP | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/vaults?beta=true` | `vaults.go` | ✅ | |
| `list` | `GET /v1/vaults?beta=true` | GET `/` | ✅ | |
| `retrieve` | `GET .../{id}?beta=true` | GET `/{id}` | ✅ | |
| `update` | `POST .../{id}?beta=true` | PUT/POST `/{id}` | ✅ | |
| `archive` | `POST .../archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `delete` | `DELETE .../{id}?beta=true` | DELETE `/{id}` | ✅ | |
| `credentials.create` | `POST .../credentials?beta=true` | POST `/{id}/credentials` | ✅ | |
| `credentials.list` | `GET .../credentials?beta=true` | GET `/{id}/credentials` | ✅ | |
| `credentials.retrieve` | `GET .../credentials/{cid}?beta=true` | GET `/{id}/credentials/{cid}` | ✅ | |
| `credentials.update` | `POST .../credentials/{cid}?beta=true` | POST `/{id}/credentials/{cid}` | ✅ | |
| `credentials.archive` | `POST .../credentials/{cid}/archive?beta=true` | POST `/{id}/credentials/{cid}/archive` | ✅ | |
| `credentials.delete` | `DELETE .../credentials/{cid}?beta=true` | DELETE `/{id}/credentials/{cid}` | ✅ | |
| `credentials.mcp_oauth_validate` | `POST .../mcp_oauth_validate?beta=true` | POST `/{id}/credentials/{cid}/mcp_oauth_validate` | ✅ | |

#### 2.6 `beta.skills`

| SDK method | HTTP | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/skills?beta=true` (multipart) | POST `/` + POST `/upload` | ⚠️ | OMA also accepts JSON `files[]`; multipart via `/upload` |
| `list` | `GET /v1/skills?beta=true` | GET `/` | ✅ | |
| `retrieve` | `GET /v1/skills/{id}?beta=true` | GET `/{id}` | ✅ | Includes built-in skills |
| `delete` | `DELETE /v1/skills/{id}?beta=true` | DELETE `/{id}` | ✅ | Built-ins forbidden |
| `versions.create` | `POST .../versions?beta=true` | POST `/{id}/versions` + `/versions/upload` | ⚠️ | Same multipart vs JSON split |
| `versions.list` | `GET .../versions?beta=true` | GET `/{id}/versions` | ✅ | |
| `versions.retrieve` | `GET .../versions/{v}?beta=true` | GET `/{id}/versions/{v}` | ⚠️ | OMA returns inline `files[]` JSON, not Anthropic metadata shape |
| `versions.delete` | `DELETE .../versions/{v}?beta=true` | DELETE `/{id}/versions/{v}` | ✅ | |
| `versions.download` | `GET .../versions/{v}/content?beta=true` | — | ❌ | **P3 gap** — use GET `/{id}/versions/{v}` JSON instead |

---

### 3. Files & Models — SDK exists, OMA shape differs

These resources exist in **both** anthropic SDK and oma-platform, but OMA SDK deliberately uses **httpx** because routes or response shapes diverge.

#### 3.1 `beta.files` vs OMA `/v1/files`

| SDK method | Anthropic HTTP | OMA route | Status | Notes |
|---|---|---|---|---|
| `upload` | `POST /v1/files?beta=true` (multipart) | `POST /v1/files` | ✅ | OMA also accepts base64 JSON body |
| `list` | `GET /v1/files?beta=true` | `GET /v1/files` | ✅ | Query params differ (`scope_id` vs OMA filters) |
| `retrieve_metadata` | `GET /v1/files/{id}?beta=true` | `GET /v1/files/{id}` | ✅ | |
| `download` | `GET /v1/files/{id}/content?beta=true` | `GET /v1/files/{id}/content` | ✅ | |
| `delete` | `DELETE /v1/files/{id}?beta=true` | `DELETE /v1/files/{id}` | ✅ | |

**SDK binding:** `OMAClient.files` → `oma_sdk/resources/files.py` (httpx), not `client.beta.files`.

**Why httpx:** OMA accepts dual upload modes (multipart + base64 JSON) and ties into session outputs; tests live in `test_files.py`.

#### 3.2 `beta.models` vs OMA `/v1/models`

| SDK method | Anthropic HTTP | OMA route | Status | Notes |
|---|---|---|---|---|
| `list` | `GET /v1/models?beta=true` | `GET /v1/models/list` (stub) + `POST /v1/models/list` | ⚠️ | OMA POST proxies provider API keys to fetch catalog |
| `retrieve` | `GET /v1/models/{id}?beta=true` | — | ❌ | OMA has no per-model retrieve |

**SDK binding:** `OMAClient.models` → httpx wrapper with OMA-specific POST `/list` semantics.

---

### 4. Anthropic SDK Resources Not on OMA (out of scope)

| SDK resource | Key methods | OMA route | Action |
|---|---|---|---|
| `beta.deployments` | create, list, retrieve, update, archive, pause, unpause, run | — | Defer — scheduled agent runs not in OMA |
| `beta.deployment_runs` | list, retrieve | — | Defer |
| `beta.user_profiles` | create, list, retrieve, update, create_enrollment_url | — | Defer — multi-tenant billing identity |
| `beta.webhooks` | unwrap (local crypto) | — | N/A — client-side only |
| `beta.messages` | create, stream, count_tokens, batches.*, tool_runner | harness `/internal/turn` | Inference via harness, not platform API |

---

### 5. OMA-Only Routes (no anthropic SDK equivalent)

| Route group | Key endpoints | SDK access | Test file |
|---|---|---|---|
| `/v1/dreams` | POST, GET, GET/{id}, POST/{id}/cancel, POST/{id}/archive | `client.dreams` | via misc / examples |
| `/v1/evals/runs` | POST /runs, GET /runs, GET/DELETE /runs/{id} | `client.evals` | `test_misc.py` |
| `/v1/runtimes` | POST /connect-runtime, GET, DELETE/{id} | `client.runtimes` | `test_misc.py` |
| `/v1/integrations/{provider}/*` | publications, installations, dispatch-rules | `client.integrations` | `test_misc.py` |
| `/v1/model_cards` | CRUD | `client.model_cards` | `test_misc.py` |
| `/v1/cost_report` | GET / | `client.cost_report` | `test_misc.py` |
| `/v1/me` | GET /, GET /tenants, POST /cli-tokens | `client.me` | `test_misc.py` |
| `/v1/api_keys` | POST, GET, DELETE/{id} | `client.api_keys` | `test_misc.py` |
| `/v1/stats` | GET / | — | console |
| `/v1/tenants` | POST / | via me | — |
| `/v1/oauth/*` | authorize, callback, refresh | — | vault OAuth |
| `/v1/clawhub/*` | search, install | — | skill marketplace |
| `/v1/sessions/{id}/teams/*` | teams, messages, tasks, shutdown | `oma_sdk/subagent.py` | `test_subagents.py` |
| `/v1/sessions/{id}/trajectory` | GET | subagent helpers | `test_subagents.py` |
| `/v1/sessions/{id}/pending` | GET | — | harness polling |
| `/v1/sessions/{id}/outputs/*` | list, download | — | session artifacts |
| `/v1/oma/*` | aliases for api_keys, me, evals, … | same httpx paths | — |

---

### 6. Gap Priority & Recommended Actions

| Priority | Gap | Impact | Recommendation |
|---|---|---|---|
| **P1** | `sessions.update` missing | Cannot patch session title/agent/tools mid-flight via SDK | Add `POST /v1/sessions/{id}` handler in `sessions.go` |
| **P2** | `sessions.resources.*` missing | File mount / MCP resource attach flows fail | Add resource CRUD routes or document workaround via session create `resources[]` |
| **P2** | `sessions.threads.retrieve/events.*` missing | Sub-agent thread isolation incomplete vs Anthropic API | Extend `session_aux.go` or document session-level event filtering |
| **P3** | `environments.delete` missing | Minor — archive covers lifecycle | Add DELETE or document archive-only |
| **P3** | `skills.versions.download` missing | SDK download returns 404 | Add `GET .../versions/{v}/content` or patch SDK tests to use JSON retrieve |
| **P3** | `beta.models.retrieve` missing | Low — list covers console needs | Add stub or keep httpx-only |
| — | Wire `client.beta.files` | Optional DX improvement | Could alias to httpx `FilesResource` behind `OMAClient.files` |
| — | Schema drift guard | Future breakage | Quarterly smoke test against live Anthropic API |

---

### 7. SDK Client Binding Matrix

| User-facing accessor | Transport | Underlying API | Notes |
|---|---|---|---|
| `client.agents` | anthropic SDK | `beta.agents` | Direct pass-through |
| `client.sessions` | anthropic SDK | `beta.sessions` | Gaps: update, resources, thread sub-routes |
| `client.environments` | anthropic SDK | `beta.environments` | Gap: delete, work.* |
| `client.memory_stores` | anthropic SDK | `beta.memory_stores` | Full parity |
| `client.vaults` | anthropic SDK | `beta.vaults` | Full parity |
| `client.skills` | anthropic SDK | `beta.skills` | Gap: versions.download |
| `client.files` | httpx | `/v1/files` | Dual upload modes |
| `client.models` | httpx | `/v1/models/list` | OMA-specific catalog proxy |
| `client.dreams` | httpx | `/v1/dreams` | Requires `anthropic-beta` dreaming header |
| `client.evals` | httpx | `/v1/evals` | |
| `client.runtimes` | httpx | `/v1/runtimes` | |
| `client.integrations` | httpx | `/v1/integrations` | |
| `client.model_cards` | httpx | `/v1/model_cards` | |
| `client.cost_report` | httpx | `/v1/cost_report` | |
| `client.me` | httpx | `/v1/me` | |
| `client.api_keys` | httpx | `/v1/api_keys` | |

---

## Implementation Status

### Done

- [x] **T1** `POST /v1/sessions/{id}/messages` — `sessions.go:236`
- [x] **T2** `sdk/pyproject.toml` — uv project, anthropic>=0.111.0
- [x] **T3** `sdk/oma_sdk/__init__.py` — `OMAClient`
- [x] **T4** `sdk/oma_sdk/resources/*` — OMA-only resource classes
- [x] **T5** `sdk/tests/conftest.py` — fixtures
- [x] **T6–T11** E2E tests — agents, sessions, environments, memory_stores, vaults, skills
- [x] **T12–T14** E2E tests — files, misc, subagents
- [x] **Examples** — `oma_sdk/api/*`, `example/data_analyst_agent.py`

### Remaining (from gap analysis)

- [ ] **T15 (P1)** `sessions.update` — `POST /v1/sessions/{id}`
- [ ] **T16 (P2)** `sessions.resources.*` — full resource CRUD
- [ ] **T17 (P2)** `sessions.threads.*` — retrieve, archive, per-thread events/stream
- [ ] **T18 (P3)** `skills.versions.download` — binary content endpoint
- [ ] **T19 (P3)** `environments.delete`
- [ ] **T20** Optional — expose `client.beta.files` as thin wrapper over httpx `FilesResource`

---

## Test Coverage Diagram

```
oma-platform APIs
├── anthropic SDK managed agents (6 resources) ─── test_agents … test_skills ✅
│   ├── agents + versions          (~7 methods, 1 OMA-only DELETE)
│   ├── sessions + events          (~9 methods; gaps: update, resources, threads)
│   ├── environments               (~5 methods; gap: delete)
│   ├── memory_stores + memories   (~14 methods) ✅ full
│   ├── vaults + credentials       (~13 methods) ✅ full
│   └── skills + versions          (~9 methods; gap: download)
├── SDK resources with shape mismatch (2) ─────── test_files, test_misc ✅
│   ├── files (httpx)
│   └── models (httpx)
└── OMA-only (9+) ─────────────────────────────── test_misc, test_subagents ✅
    ├── dreams, evals, runtimes, integrations
    ├── model_cards, cost_report, me, api_keys
    └── teams / trajectory / subagents
```

Test framework: `pytest` + `pytest-asyncio` (asyncio_mode=auto); live E2E against `:8787`.

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | issues_open | 6 P1–P3 gaps documented; SDK shipped |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

- **UNRESOLVED:** 4 open platform gaps (sessions.update, sessions.resources, sessions.threads, skills.download)
- **VERDICT:** Eng review complete — gap matrix updated 2026-06-28. Implement T15–T17 before claiming full Anthropic wire-compat.

### Section Summary

| Section | Result |
|---|---|
| Step 0: Scope | Accepted — gap analysis scoped to anthropic 0.111.0 vs current OMA routes |
| Architecture | 6 managed-agent resources via SDK; 9+ OMA-only via httpx — sound split |
| Code Quality | SDK implemented; examples in `oma_sdk/api/` |
| Tests | 9 test modules; live E2E pattern established |
| Performance | SSE via anthropic SDK; shared httpx pool in OMAClient |
| NOT in scope | deployments, user_profiles, webhooks, messages, environments.work |
| What already exists | Updated — SDK no longer greenfield |
| Failure modes | sessions.update/resources gaps → silent SDK 404 for advanced flows |
| Outside voice | Skipped this run |
| Parallelization | Sequential — gap fixes touch `sessions.go` primarily |

**Architecture score: 8/10** — Wire-compat design validated; remaining gaps are concentrated in sessions sub-resources.

**Recommended guard:** Quarterly smoke test creating one resource of each type against live Anthropic API to detect schema drift.
