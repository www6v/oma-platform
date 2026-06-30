# OMA Platform Python SDK — Engineering Review & Implementation Plan

> **Status (2026-06-30):** SDK implemented in this directory. Managed-agents resources route through `anthropic>=0.111.0` with `base_url`; OMA-only resources use `httpx` wrappers. E2E tests cover agents, sessions, environments, memory_stores, vaults, skills, files, misc, subagents. **Cookbook parity (data analyst):** P0 closed — session create `resources[]`, turn mount, outputs→Files API, Go integration test, and `example/example1/data_analyst_agent.py` parity probe (see [gap analysis](../docs/sdk/data-analyst-cookbook-gap-analysis.md)). **Cookbook parity (iterate):** parity probe added at `example/example2/iterate_fix_failing_tests.py` (from `CMA_iterate_fix_failing_tests.ipynb`); gaps below.

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
| Go HTTP server | `../cmd/oma-server/main.go` | Listens `:8787`, chi router, SQLite |
| Core API handlers | `../internal/api/*.go` | agents, sessions, environments, memory_stores, vaults, skills, dreams, evals, runtimes, integrations, model_cards, files |
| `/v1/oma/*` aliases | `../internal/api/oma_aliases.go` | Mirrors select routes under alternate namespace |
| Python SDK | `oma_sdk/` | `OMAClient` + resource classes + `oma_sdk/api/*` examples |
| SDK tests | `tests/` | pytest + pytest-asyncio, live E2E against running server |
| Python harness sidecar | `../harness/` | uv project, fastapi, httpx |
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

**Reference:** anthropic Python SDK **0.111.0** (`client.beta.*`) vs oma-platform routes in `../internal/api/router.go`.

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
| `beta.sessions` | `/v1/sessions` | ✅ Create `resources[]` + events + update + post-create resources CRUD + threads sub-routes | `client.sessions` (anthropic) | Low |
| `beta.environments` | `/v1/environments` | ✅ CRUD + archive + delete; no work.* | `client.environments` (anthropic) | Low |
| `beta.memory_stores` | `/v1/memory_stores` | ✅ Full CRUD + memories + versions | `client.memory_stores` (anthropic) | Low |
| `beta.vaults` | `/v1/vaults` | ✅ Full CRUD + credentials | `client.vaults` (anthropic) | Low |
| `beta.skills` | `/v1/skills` | ✅ CRUD + versions.download zip | `client.skills` (anthropic) | Low |
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
| `create` | `POST /v1/sessions?beta=true` | `sessions.go` POST `/` | ✅ | Accepts `resources[]`; scoped file copies persisted + returned |
| `list` | `GET /v1/sessions?beta=true` | GET `/` | ✅ | Filter params partially supported |
| `retrieve` | `GET /v1/sessions/{id}?beta=true` | GET `/{id}` | ✅ | |
| `update` | `POST /v1/sessions/{id}?beta=true` | POST `/{id}` | ✅ | title + metadata merge |
| `delete` | `DELETE /v1/sessions/{id}?beta=true` | DELETE `/{id}` | ✅ | |
| `archive` | `POST /v1/sessions/{id}/archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `events.send` | `POST /v1/sessions/{id}/events?beta=true` | POST `/{id}/events` | ✅ | |
| `events.list` | `GET /v1/sessions/{id}/events?beta=true` | GET `/{id}/events` | ✅ | |
| `events.stream` | `GET /v1/sessions/{id}/events/stream?beta=true` | GET `/{id}/events/stream` | ✅ | SSE |
| `resources.list` | `GET .../resources?beta=true` | GET `/{id}/resources` | ✅ | |
| `resources.add` | `POST .../resources?beta=true` | POST `/{id}/resources` | ✅ | file scoped copy on add |
| `resources.retrieve` | `GET .../resources/{rid}?beta=true` | GET `/{id}/resources/{rid}` | ✅ | |
| `resources.update` | `POST .../resources/{rid}?beta=true` | POST `/{id}/resources/{rid}` | ✅ | |
| `resources.delete` | `DELETE .../resources/{rid}?beta=true` | DELETE `/{id}/resources/{rid}` | ✅ | |
| `threads.list` | `GET .../threads?beta=true` | GET `/{id}/threads` | ✅ | derived from events |
| `threads.retrieve` | `GET .../threads/{tid}?beta=true` | GET `/{id}/threads/{tid}` | ✅ | |
| `threads.archive` | `POST .../threads/{tid}/archive?beta=true` | POST `/{id}/threads/{tid}/archive` | ✅ | persists `session.thread_archived` event |
| `threads.events.list` | `GET .../threads/{tid}/events?beta=true` | GET `/{id}/threads/{tid}/events` | ✅ | filter by `session_thread_id` |
| `threads.events.stream` | `GET .../threads/{tid}/stream?beta=true` | GET `/{id}/threads/{tid}/stream` | ✅ | SSE filtered by thread |
| — | — | `POST /{id}/messages` | ➕ | OMA convenience endpoint; **not in anthropic SDK** — wraps `events.send` |

#### 2.3 `beta.environments`

| SDK method | HTTP | OMA handler | Status | Notes |
|---|---|---|---|---|
| `create` | `POST /v1/environments?beta=true` | `environments.go` POST `/` | ✅ | |
| `list` | `GET /v1/environments?beta=true` | GET `/` | ✅ | |
| `retrieve` | `GET /v1/environments/{id}?beta=true` | GET `/{id}` | ✅ | |
| `update` | `POST/PUT /v1/environments/{id}?beta=true` | PUT/POST `/{id}` | ✅ | |
| `archive` | `POST .../archive?beta=true` | POST `/{id}/archive` | ✅ | |
| `delete` | `DELETE /v1/environments/{id}?beta=true` | DELETE `/{id}` | ✅ | blocked when non-archived sessions reference env |
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
| `versions.download` | `GET .../versions/{v}/content?beta=true` | GET `/{id}/versions/{v}/content` | ✅ | zip archive (`application/zip`) |

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

**Why httpx:** OMA accepts dual upload modes (multipart + base64 JSON), ties into session outputs via `scope_id`, and supports session-scoped file copies from `sessions.create(resources=[])`. Tests: `tests/test_files.py`; cookbook probe: `example/example1/data_analyst_agent.py`.

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
| `/v1/evals/runs` | POST /runs, GET /runs, GET/DELETE /runs/{id} | `client.evals` | `tests/test_misc.py` |
| `/v1/runtimes` | POST /connect-runtime, GET, DELETE/{id} | `client.runtimes` | `tests/test_misc.py` |
| `/v1/integrations/{provider}/*` | publications, installations, dispatch-rules | `client.integrations` | `tests/test_misc.py` |
| `/v1/model_cards` | CRUD | `client.model_cards` | `tests/test_misc.py` |
| `/v1/cost_report` | GET / | `client.cost_report` | `tests/test_misc.py` |
| `/v1/me` | GET /, GET /tenants, POST /cli-tokens | `client.me` | `tests/test_misc.py` |
| `/v1/api_keys` | POST, GET, DELETE/{id} | `client.api_keys` | `tests/test_misc.py` |
| `/v1/stats` | GET / | — | console |
| `/v1/tenants` | POST / | via me | — |
| `/v1/oauth/*` | authorize, callback, refresh | — | vault OAuth |
| `/v1/clawhub/*` | search, install | — | skill marketplace |
| `/v1/sessions/{id}/teams/*` | teams, messages, tasks, shutdown | `oma_sdk/subagent.py` | `tests/test_subagents.py` |
| `/v1/sessions/{id}/trajectory` | GET | subagent helpers | `tests/test_subagents.py` |
| `/v1/sessions/{id}/pending` | GET | — | harness polling |
| `/v1/sessions/{id}/outputs/*` | list, download | — | Prefer `files.list(scope_id=session.id)` after turn sync |
| `/v1/oma/*` | aliases for api_keys, me, evals, … | same httpx paths | — |

---

### 6. Gap Priority & Recommended Actions

| Priority | Gap | Impact | Recommendation |
|---|---|---|---|
| **P1** | `sessions.update` missing | Cannot patch session title/agent/tools mid-flight via SDK | Add `POST /v1/sessions/{id}` handler in `sessions.go` |
| **P1** | Environment `packages.pip` not installed locally | Cookbook step 1 succeeds but harness may lack pandas/plotly | Document local venv requirement or install packages in harness |
| **P2** | `sessions.resources.*` post-create CRUD missing | Cannot add/remove mounts after session create | Add resource CRUD routes; **create-time `resources[]` works** (2026-06) |
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
| `client.sessions` | anthropic SDK | `beta.sessions` | Gaps: update, post-create resources CRUD, thread sub-routes; create `resources[]` ✅ |
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
- [x] **T2** `pyproject.toml` — uv project, anthropic>=0.111.0
- [x] **T3** `oma_sdk/__init__.py` — `OMAClient`
- [x] **T4** `oma_sdk/resources/*` — OMA-only resource classes
- [x] **T5** `tests/conftest.py` — fixtures
- [x] **T6–T11** E2E tests — agents, sessions, environments, memory_stores, vaults, skills
- [x] **T12–T14** E2E tests — files, misc, subagents
- [x] **Examples** — `oma_sdk/api/*`, `example/example1/data_analyst_agent.py` (cookbook parity probe)

### Cookbook parity — data analyst (2026-06)

Reference: Anthropic `managed_agents/data_analyst_agent.ipynb` vs `example/example1/data_analyst_agent.py`.
Full gap matrix: [`docs/sdk/data-analyst-cookbook-gap-analysis.md`](../docs/sdk/data-analyst-cookbook-gap-analysis.md).

| ID | Priority | Task | Status | Verify |
|---|---|---|---|---|
| **C1** | P0 | Session create accepts `resources[]` | ✅ | `session_resources_api_test.go` |
| **C2** | P0 | Turn resolves session + env resources | ✅ | harness mount tests |
| **C3** | P0 | Outputs path + post-turn sync → Files API | ✅ | `workdir/sync_test.go`, `session_outputs_api_test.go` |
| **C4** | P0 | Go integration test (cookbook §3–6) | ✅ | CI `TestDataAnalystCookbook` |
| **C5** | P0 | Example script cookbook 1:1 (no default disk fallback) | ✅ | `python example/example1/data_analyst_agent.py` |
| **C6** | P1 | `DataAnalystExamples` helper + pytest E2E | ⏸️ deferred | — |
| **C7** | P1 | SDK-PLAN gap status update | ✅ | this doc |

**Still open for full cookbook parity (non-blocking for C1–C5):**

- Local harness does not install `environment.config.packages.pip` (use host venv or document)
- `client.files` via httpx, not `client.beta.files` (T20)
- Turn timeout not API-configurable (env var only)
- Post-create `sessions.resources.*` CRUD (S2)

### Remaining (Anthropic wire-compat gaps)

- [x] **T15 (P1)** `sessions.update` — `POST /v1/sessions/{id}` (+ metadata merge, migration `020_session_metadata.sql`)
- [x] **T16 (P2)** `sessions.resources.*` — full resource CRUD (`session_resources_handlers.go`)
- [x] **T17 (P2)** `sessions.threads.*` — retrieve, archive, per-thread events/stream
- [x] **T18 (P3)** `skills.versions.download` — `GET /{id}/versions/{v}/content` zip archive
- [x] **T19 (P3)** `environments.delete` — `DELETE /v1/environments/{id}` with active-session guard
- [ ] **T20** Optional — expose `client.beta.files` as thin wrapper over httpx `FilesResource`

### Cookbook parity — iterate fix failing tests (2026-06-30)

Reference: Anthropic `managed_agents/CMA_iterate_fix_failing_tests.ipynb` vs `example/example2/iterate_fix_failing_tests.py`.

| ID | Priority | Gap | Cookbook expects | OMA / SDK today | Recommendation |
|---|---|---|---|---|---|
| **IF1** | P1 | Stream-then-send ordering | `with client.beta.sessions.events.stream(...) as stream:` then `events.send` inside open SSE | `oma_sdk.cookbook.stream_until_end_turn` opens httpx SSE first, sends after connect delay, `replay=True` | ✅ SDK helper shipped; sync anthropic context manager still N/A |
| **IF2** | P1 | Archive race after idle | `wait_for_idle_status()` polls `sessions.retrieve().status == "idle"` before `archive()` | `oma_sdk.cookbook.wait_for_idle_status` + called from both examples before archive | ✅ |
| **IF3** | P1 | `end_turn` vs bare idle | Exit on `session.status_idle` **and** `stop_reason.type == "end_turn"` | `stream_until_end_turn` checks both; `data_analyst_agent.py` updated | ✅ |
| **IF4** | P2 | Shared cookbook helpers | `from utilities import stream_until_end_turn, wait_for_idle_status` | `from oma_sdk import stream_until_end_turn, wait_for_idle_status` | ✅ |
| **MT1** | P2 | Multi-turn same session | Cell 15: second `user.message` + stream after first `end_turn` | Go `TestIterateCookbookMultiTurn` + pytest `test_iterate_cookbook.py` | ✅ |
| **F1** | P2 | Files upload SDK path | `client.beta.files.upload(file=(name, bytes, mime))` | `client.files.upload` via httpx async (T20) | Optional `client.beta.files` alias |
| **S1** | P0 | Session create `resources[]` | Mount calc.py + test_calc.py at create | ✅ closed 2026-06 | Parity probe asserts `len(session.resources) >= 2` |
| **O1** | P0 | Outputs → Files API | `/mnt/session/outputs/calc.py` listable via `files.list(scope_id=session.id)` | Same class as data-analyst report.html | Reuse outputs sync path; probe raises if calc.py missing |
| **M1** | P3 | Agent model shape | `model=MODEL` string | OMA examples use `model={"id": MODEL}` | Document; verify anthropic SDK accepts both against OMA |

**Iterate-specific vs data-analyst overlap:** S1 and O1 share the data-analyst P0 fixes. New gaps from this notebook are **IF1–IF4** (streaming semantics) and **MT1** (multi-turn).

**Example mapping (notebook → script):**

| Notebook | Script function / block |
|---|---|
| Cell 3 agent | `client.agents.create` + `ITERATE_SYSTEM_PROMPT` |
| Cell 5 environment | `client.environments.create` (`limited` networking) |
| Cell 7 upload | `upload_fixture()` → `client.files.upload` |
| Cell 9 session | `client.sessions.create(resources=[...])` |
| Cell 11 stream+send | `oma_sdk.cookbook.stream_until_end_turn(..., send_events=[...])` |
| Cell 15 verify | second `events.send` + `stream_until_end_turn` |
| Cell 17 archive | `wait_for_idle_status` + archive session/env/agent |

SDK module: `oma_sdk/cookbook.py` — `stream_until_end_turn`, `wait_for_idle_status`, `StreamConfig`, event parsers. Tests: `tests/test_cookbook.py`, `tests/test_iterate_cookbook.py` (MT1). Go: `TestIterateCookbookMultiTurn` in `internal/api/` and `test/integration/`.

Fixtures: `sdk/example/example2/iterate/calc.py`, `test_calc.py` (from cookbook `example_data/iterate/`).

---

## Test Coverage Diagram

```
oma-platform APIs
├── anthropic SDK managed agents (6 resources) ─── test_agents … test_skills ✅
│   ├── agents + versions          (~7 methods, 1 OMA-only DELETE)
│   ├── sessions + events          (~9 methods; create resources ✅; gaps: update, post-create resources CRUD, threads)
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

Cookbook parity (Go server):
├── data analyst critical path ─────────────────── test/integration/data_analyst_cookbook_test.go ✅
│   └── CI: `.github/workflows/ci.yml` → `TestDataAnalystCookbook`
└── iterate fix failing tests ──────────────────── example/example2/iterate_fix_failing_tests.py [GAP probe]
    ├── [★★★ TESTED] stream_until_end_turn + end_turn (oma_sdk.cookbook)
    ├── [★★★ TESTED] MT1 multi-turn — Go TestIterateCookbookMultiTurn + pytest
    └── [★★  TESTED] session resources + outputs via shared S1/O1 path
```

Test framework: `pytest` + `pytest-asyncio` (asyncio_mode=auto); live E2E against `:8787`.

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 3 | issues_open | Iterate cookbook probe: IF1–IF4, MT1 gaps logged |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

- **UNRESOLVED:** optional T20 (`client.beta.files` alias); cookbook local pip install still manual
- **VERDICT:** IF1–IF4 + MT1 closed; iterate cookbook parity probe complete except live LLM run

### Section Summary

| Section | Result |
|---|---|
| Step 0: Scope | Accepted — gap analysis scoped to anthropic 0.111.0 vs current OMA routes; cookbook P0 tracked separately |
| Architecture | 6 managed-agent resources via SDK; 9+ OMA-only via httpx — sound split; session create resources + outputs sync landed |
| Code Quality | SDK implemented; examples in `oma_sdk/api/`; `example/example1/data_analyst_agent.py` is parity probe (workaround copy in `example/example1/v1/`) |
| Tests | 9 pytest modules + Go `TestDataAnalystCookbook`; live E2E pattern established |
| Performance | SSE via anthropic SDK or `client.events.stream`; shared httpx pool in OMAClient |
| NOT in scope | deployments, user_profiles, webhooks, messages, environments.work, cloud runtime |
| What already exists | Updated — SDK no longer greenfield; cookbook critical path covered in CI |
| Failure modes | Resource caps (100/8 memory_store) enforced; thread archive blocks further events via `session.thread_archived` |
| Outside voice | Skipped this run |
| Parallelization | Sequential — gap fixes touch `sessions.go` primarily |

**Architecture score: 8/10** — Wire-compat design validated; remaining gaps are concentrated in sessions sub-resources (post-create) and local package install.

**Recommended guard:** Quarterly smoke test creating one resource of each type against live Anthropic API to detect schema drift.
