# OMA Platform Python SDK — Engineering Review & Implementation Plan

## NOT In Scope

- Migrating TypeScript packages from open-managed-agents to Go (no TS→Go rewrite)
- Implementing missing Anthropic Managed Agents resources in oma-platform: `deployments`, `user_profiles`, `webhooks` (these are in `anthropic.beta` but not in oma-platform; tracked as future gaps)
- CI/CD pipeline changes
- Frontend / console routes (`/v1/console/*`, stubs)
- Internal routes (`/v1/internal/*`) and runtime daemon routes
- OAuth gateway routes (linear, github, slack gateway webhooks)

---

## What Already Exists

| Component | Location | Notes |
|---|---|---|
| Go HTTP server | `oma-platform/cmd/oma-server/main.go` | Listens `:8787`, chi router, SQLite |
| Session events | `internal/api/sessions.go:173,274,327` | `POST /{id}/events`, `GET /{id}/events`, `GET /{id}/events/stream` |
| All core API handlers | `internal/api/*.go` | agents, sessions, environments, memory_stores, vaults, skills, dreams, evals, runtimes, integrations, model_cards |
| `/v1/oma/*` aliases | `internal/api/oma_aliases.go` | Mirrors routes under alternate namespace |
| Python harness sidecar | `harness/` | uv project, fastapi, httpx, pytest, pytest-asyncio |
| SDK directory | `sdk/` | **Empty** — greenfield |
| open-managed-agents TS SDK | `open-managed-agents/packages/sdk/` | **Deprecated** — says to use `@anthropic-ai/sdk` with custom baseURL |
| anthropic Python SDK | system pip | Upgraded to **0.111.0** (has full `beta.agents` namespace) |

---

## Architecture Decisions (from review)

| # | Decision | Choice | Rationale |
|---|---|---|---|
| D1 | HTTP client for Python SDK | Upgrade anthropic SDK | v0.111.0 has full `beta.agents`, `beta.sessions`, etc. |
| D2 | Missing /messages endpoint | Add to oma-platform | Wire-compat with Anthropic Managed Agents spec |
| D3 | Authentication | `OMA_API_KEY` env var | Matches harness convention |
| D4 | SDK package layout | Resource-class layout | Mirrors anthropic SDK structure |
| D5 | Test coverage scope | All oma-platform endpoints | User requirement: validate ALL APIs |

### Key Architectural Insight

`anthropic.Anthropic(api_key=OMA_API_KEY, base_url='http://localhost:8787')` routes all `beta.*` calls through oma-platform. The anthropic SDK's `beta.sessions.events` maps directly to oma-platform's existing routes:

```
anthropic.beta.sessions.events.send()  →  POST /v1/sessions/{id}/events
anthropic.beta.sessions.events.list()  →  GET  /v1/sessions/{id}/events
anthropic.beta.sessions.events.stream()→  GET  /v1/sessions/{id}/events/stream
```

---

## API Coverage Map

### Fully Covered by anthropic SDK 0.111.0

| anthropic.beta.X | oma-platform route | Status |
|---|---|---|
| agents | `/v1/agents` | ✅ exists |
| agents.versions | `/v1/agents/{id}/versions` | ✅ exists |
| sessions | `/v1/sessions` | ✅ exists |
| sessions.events | `/v1/sessions/{id}/events[/stream]` | ✅ exists |
| sessions.threads | `/v1/sessions/{id}/threads` (session_threads.go) | ✅ exists |
| environments | `/v1/environments` | ✅ exists |
| memory_stores | `/v1/memory_stores` | ✅ exists |
| memory_stores.memories | `/v1/memory_stores/{id}/memories` | ✅ exists |
| memory_stores.memory_versions | `/v1/memory_stores/{id}/memory_versions` | ✅ exists |
| vaults | `/v1/vaults` | ✅ exists |
| vaults.credentials | `/v1/vaults/{id}/credentials` | ✅ exists |
| skills | `/v1/skills` | ✅ exists |
| skills.versions | `/v1/skills/{id}/versions` | ✅ exists |

### oma-platform Only (no anthropic SDK equivalent)

| oma-platform route | Handler | Test strategy |
|---|---|---|
| `/v1/dreams` | dreams.go | raw httpx via OMAClient |
| `/v1/evals` | eval_runs.go | raw httpx via OMAClient |
| `/v1/runtimes` | runtimes.go | raw httpx via OMAClient |
| `/v1/integrations` | integrations.go | raw httpx via OMAClient |
| `/v1/model_cards` | model_cards.go | raw httpx via OMAClient |
| `/v1/cost_report` | cost_report.go | raw httpx via OMAClient |
| `/v1/me` | me.go | raw httpx via OMAClient |
| `/v1/api_keys` | me.go:172 | raw httpx via OMAClient |
| `/v1/files` | files.go | raw httpx via OMAClient |
| `/v1/models` | models_list.go | raw httpx via OMAClient |

### anthropic SDK Has But oma-platform Missing

| anthropic.beta.X | Gap action |
|---|---|
| deployments | Out of scope — not implemented |
| user_profiles | Out of scope — not implemented |
| sessions (POST `/{id}/messages`) | **D2: Add to sessions.go** |

---

## Implementation Tasks

### Phase 1: Go — Add Missing Endpoint

**T1** `internal/api/sessions.go` — Add `POST /{id}/messages` handler
- Accept `{"content": ..., "role": "user"}` body
- Write event via same path as `POST /{id}/events`
- Stream response using SSE from `GET /{id}/events/stream`
- Wire-compatible with `anthropic.beta.sessions.resume()` / messages pattern

### Phase 2: Python SDK Scaffolding

**T2** `sdk/pyproject.toml` — uv project
```toml
[project]
name = "oma-sdk"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
  "anthropic>=0.111.0",
  "httpx>=0.28.0",
]

[dependency-groups]
dev = ["pytest>=8.0", "pytest-asyncio>=0.24", "respx>=0.21"]

[build-system]
requires = ["hatchling"]
build-backend = "hatchling.build"

[tool.hatch.build.targets.wheel]
packages = ["oma_sdk"]

[tool.pytest.ini_options]
asyncio_mode = "auto"
testpaths = ["tests"]
```

**T3** `sdk/oma_sdk/__init__.py` — OMAClient class
```python
import os, anthropic, httpx

class OMAClient:
    def __init__(self, base_url: str = "http://localhost:8787"):
        api_key = os.environ["OMA_API_KEY"]
        self.anthropic = anthropic.Anthropic(api_key=api_key, base_url=base_url)
        self.http = httpx.AsyncClient(
            base_url=base_url,
            headers={"x-api-key": api_key},
        )
    # Expose beta resources as direct attributes
    @property
    def agents(self): return self.anthropic.beta.agents
    @property
    def sessions(self): return self.anthropic.beta.sessions
    @property
    def environments(self): return self.anthropic.beta.environments
    @property
    def memory_stores(self): return self.anthropic.beta.memory_stores
    @property
    def vaults(self): return self.anthropic.beta.vaults
    @property
    def skills(self): return self.anthropic.beta.skills
```

**T4** `sdk/oma_sdk/resources/` — oma-platform-only resource classes (dreams, evals, runtimes, integrations, model_cards, cost_report, me, api_keys, files, models)

Each resource wraps `self.http` with typed methods. Example:
```python
class DreamsResource:
    def __init__(self, http): self.http = http
    async def create(self, **body): ...
    async def list(self, agent_id: str): ...
    async def get(self, id: str): ...
    async def cancel(self, id: str): ...
    async def archive(self, id: str): ...
```

### Phase 3: Test Suite

**T5** `sdk/tests/conftest.py` — pytest fixtures
- `@pytest.fixture` `client` → `OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))`
- Mark tests `live` when they need a running server

**T6** `sdk/tests/test_agents.py` — agents CRUD + versions
**T7** `sdk/tests/test_sessions.py` — sessions CRUD + events send/list/stream + messages
**T8** `sdk/tests/test_environments.py` — environments CRUD
**T9** `sdk/tests/test_memory_stores.py` — memory stores CRUD + memories + memory_versions
**T10** `sdk/tests/test_vaults.py` — vaults CRUD + credentials
**T11** `sdk/tests/test_skills.py` — skills CRUD + versions + upload
**T12** `sdk/tests/test_dreams.py` — dreams CRUD + cancel + archive
**T13** `sdk/tests/test_evals.py` — eval runs CRUD
**T14** `sdk/tests/test_misc.py` — me, api_keys, files, models, cost_report, model_cards

---

## GSTACK REVIEW REPORT

### Section 1 — Architecture

**Findings:**

| Finding | Severity | Resolution |
|---|---|---|
| anthropic SDK 0.72.0 had no `beta.agents` | BLOCKER | Upgraded to 0.111.0 ✅ |
| oma-platform missing `POST /sessions/{id}/messages` | HIGH | Add in sessions.go (T1) |
| oma-platform routes not covered by anthropic SDK (dreams, evals, etc.) | MEDIUM | OMAClient.http for oma-specific resources (T4) |

**Architecture score: 7/10** — Good base (wire-compat design is correct), one endpoint gap to fill, oma-platform-only resources need thin httpx wrappers.

### Section 2 — Code Quality

**Findings:**

- SDK is greenfield (empty `sdk/`) — no legacy debt
- Harness pattern (`uv`, `pyproject.toml`, `pytest-asyncio`) is well-established and should be replicated
- OMAClient design: anthropic SDK handles auth/retry/serialization for managed agents resources; oma-specific resources use raw httpx — clean separation, no DRY violations

**Code quality target:** Resource classes should match anthropic SDK naming conventions exactly (snake_case, same method names: `create`, `list`, `retrieve`, `update`, `archive`, `delete`).

### Section 3 — Tests

**Coverage diagram:**

```
oma-platform APIs (22 resource groups)
├── anthropic SDK managed agents (13) ────── T6-T11 ✅
│   ├── agents + versions
│   ├── sessions + events + threads + messages
│   ├── environments
│   ├── memory_stores + memories + memory_versions
│   ├── vaults + credentials
│   └── skills + versions
└── oma-platform only (9) ─────────────────── T12-T14 ✅
    ├── dreams
    ├── evals
    ├── runtimes
    ├── integrations
    ├── model_cards
    ├── cost_report
    ├── me
    ├── api_keys
    └── files / models
```

Test framework: `pytest` + `pytest-asyncio` (asyncio_mode=auto) + `respx` for mocked tests; `live` marker for integration tests against real server.

### Section 4 — Performance

- SSE streaming: `anthropic.beta.sessions.events.stream()` handles SSE natively — no custom implementation needed
- Connection pooling: httpx.AsyncClient with shared instance in OMAClient
- No N+1 concerns in test suite (each test creates/deletes its own resources)

### Outside Voice

The wire-compatibility design (use Anthropic SDK + `base_url`) is the right call — it future-proofs oma-platform against Anthropic API schema changes by inheriting the SDK's request/response models automatically. The main risk is schema drift: when Anthropic adds required fields to managed agents resources, oma-platform handlers will need to accept them even if unused.

**Recommended guard:** Add a smoke test that creates one of each resource type against the live Anthropic API to detect schema drift quarterly.
