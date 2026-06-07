# OMA Building MVP Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up `oma-building` as a self-hostable MVP that implements the core meta-harness loop from `open-managed-agents`: versioned agents, durable sessions with append-only event logs, LLM harness turns, and local subprocess sandbox tools — without Cloudflare Workers dependencies.

**Architecture:** Split into a **Go platform runtime** (HTTP API, SQLite persistence, session state machine, SSE fan-out, **workdir provisioning only**) and a **Python harness sidecar** wrapping **piPy `create_agent_session`** (not a greenfield loop). Go owns durability and concurrency; piPy owns the LLM loop **and tool execution** (`read`/`bash`/`write` in `$SANDBOX_WORKDIR/<session_id>/`). Each turn is **stateless**: adapter projects OMA `SessionEvent[]` → piPy messages, runs one prompt, returns OMA-shaped events for Go to persist + SSE. Turn orchestration: `POST /internal/turn`. API surface mirrors OMA `/v1/agents` and `/v1/sessions` subset for SDK compatibility later.

**Tech Stack:** Go 1.22+, chi router, modernc.org/sqlite, Python 3.11+, FastAPI (thin adapter), piPy (`pi_coding_agent` via path dep to `piPy-env/piPy`), pytest, go test, docker compose

---

## 0. Language choice (read first)

### Recommendation: **Go (platform) + Python (harness)**

| Layer | Language | Why |
|-------|----------|-----|
| REST API + SSE | **Go** | Single static binary, goroutines map cleanly to many concurrent SSE tails; production deploy is `COPY binary` not a venv |
| Session state machine + event log | **Go** | Port of `packages/session-runtime` is straightforward; SQLite via `modernc.org/sqlite` needs no CGO |
| Session workdirs | **Go** | Create/isolate `$SANDBOX_WORKDIR/<session_id>/`; no tool exec in Go (eng-review D4) |
| Harness (LLM + tools loop) | **Python + piPy** | `create_agent_session(in_memory=True, cwd=workdir)`; adapter maps OMA events ↔ piPy messages |
| Future CLI | **Go** | Same repo, `cmd/oma` |

### piPy harness evaluation (requested)

**What piPy is:** Python port of pi-mono — three layers (`pi_ai` → `pi_agent` → `pi_coding_agent`). Local asyncio agent runtime with built-in coding tools, JSONL sessions, RPC mode, skills/extensions.

**Verdict for oma-building MVP (eng-review 2026-06-07):** **Use `pi_coding_agent.create_agent_session` in a FastAPI sidecar; stateless turns; piPy owns tools in session workdir.**

| Integration option | Fit | Notes |
|--------------------|-----|-------|
| **A) FastAPI + `create_agent_session(in_memory=True)`** (accepted) | **Best** | OMA event log is sole durable history; built-in read/bash/write in workdir |
| B) `pipy --mode rpc` subprocess | Rejected | Per-turn process + history sync complexity |
| C) Dual JSONL + OMA SQLite | Rejected | Sync nightmare (eng-review D5) |
| D) Greenfield Anthropic loop | Fallback | Only if piPy adapter blocks >3 days |

**OMA adapter responsibilities (~300–500 LOC):**

1. `project_oma_events()` → piPy message list at turn start
2. `create_agent_session(cwd=workdir, tools=["read","bash","write"], in_memory=True)`
3. `emit_oma_events()` from piPy `AgentEvent` stream → `SessionEvent[]` batch
4. Contract tests against `open-managed-agents/test/` golden JSON (eng-review D7)

**piPy path dependency:**

```toml
[tool.uv.sources]
pi-coding-agent = { path = "../../../piPy-env/piPy/packages/pi_coding_agent" }
pi-ai = { path = "../../../piPy-env/piPy/packages/pi_ai" }
pi-agent = { path = "../../../piPy-env/piPy/packages/pi_agent" }
```

### Why piPy for harness (eng-review 2026-06-07)

| Factor | Custom FastAPI loop (old plan) | piPy SDK adapter (accepted) |
|--------|-------------------------------|----------------------------|
| Loop + tools | Port `default-loop.ts` manually | `create_agent_session` + built-in read/bash/write |
| Providers | anthropic SDK only | piPy registry (anthropic + openai + google + faux for tests) |
| RPC option | N/A | Available but **rejected for MVP** — HTTP sidecar + SDK is simpler with docker-compose |
| State | N/A | **Stateless turns** — OMA SQLite event log is sole durable history |
| Sandbox | Go `internal/sandbox` | **piPy owns cwd** — Go only creates `$SANDBOX_WORKDIR/<session_id>/` |

piPy path dependency: `pyproject.toml` path → `../../../piPy-env/piPy/packages/pi_coding_agent` (or git submodule pin for CI).

### Why not pure Python monolith?

FastAPI-only platform makes SSE + turn mutexes harder to reason about under load. OMA's `main-node` hit seq races under concurrent `broadcast()` — Go explicit per-session turn locks match that lesson.

### Why not pure Rust?

Best safety/perf for sandbox, but 2–3× MVP calendar time; Anthropic ecosystem is thinner than Python. Revisit for sandbox hardening in Phase 2.

### Why not stay on TypeScript?

`open-managed-agents/apps/main-node` is the closest reference implementation. Forking it would be fastest *if* the goal were another Node deploy. `oma-building` is explicitly a greenfield runtime in Go/Python/Rust — use TS **only as spec reference**, not copied code.

### MVP shortcut (optional Week-1 spike)

If you need a demo in &lt;5 days: **Python monolith** (`platform/` + `harness/` in one FastAPI app), then extract Go platform in Week 2. This plan assumes the split from day one to avoid a second migration.

---

## 1. MVP scope

### In scope (P0)

| Capability | OMA reference | oma-building deliverable |
|------------|---------------|--------------------------|
| Health | `GET /health` | Same |
| API key auth | `x-api-key` middleware | Single key from `OMA_API_KEY` env |
| Agent CRUD | `POST/GET /v1/agents`, archive | Create, get, list, soft-archive; version bump on update |
| Session lifecycle | `POST/GET /v1/sessions` | Create (bind agent version), get, list |
| Event log | `POST /v1/sessions/:id/events`, `GET .../events` | Append `user.message`; paginate JSON |
| SSE stream | `GET /v1/sessions/:id/events/stream` | Tail new events; optional `?replay=1` |
| Harness turn | SessionDO `runHarnessTurn` | On `user.message`, run loop until idle |
| Tools | `bash`, `read`, `write` | Local subprocess in session workdir |
| Crash hint | `onWake` / orphan `running` status | Mark stale `running` → `interrupted` at boot |

### Explicitly out of scope (P1+)

- Console UI, better-auth, multi-tenant
- Vault / outbound credential proxy (`oma-vault`)
- Memory stores, files, environments, model cards
- Integrations (Linear / GitHub / Slack)
- Compaction, browser tools, ACP proxy harness
- Evals, dreams, RL, Cloudflare bindings
- Full Anthropic API parity (threads, resources, compaction events)

### Success criteria (smoke test)

```bash
# After docker compose up
AID=$(curl -s -X POST localhost:8787/v1/agents \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d '{"name":"hello","model":"claude-sonnet-4-20250514","system_prompt":"You are helpful."}' \
  | jq -r .id)

SID=$(curl -s -X POST localhost:8787/v1/sessions \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d "{\"agent\":\"$AID\"}" | jq -r .id)

curl -s -X POST localhost:8787/v1/sessions/$SID/events \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d '{"events":[{"type":"user.message","content":[{"type":"text","text":"Run: uname -a"}]}]}'

curl -s "localhost:8787/v1/sessions/$SID/events?order=asc" -H "x-api-key: $OMA_API_KEY" \
  | jq '.data[] | select(.type=="agent.message")'
```

---

## 2. Target repository layout

```
oma-building/
├── cmd/
│   └── oma-server/
│       └── main.go                 # entrypoint
├── internal/
│   ├── api/                        # chi handlers, auth middleware
│   ├── store/                      # SQLite repos (agents, sessions, events)
│   ├── session/                    # SessionRegistry + state machine
│   ├── workdir/                    # per-session directory create/clean (no tool exec)
│   └── stream/                     # SSE hub per session
├── migrations/
│   └── 001_core.sql
├── harness/                        # Python sidecar (piPy adapter)
│   ├── pyproject.toml              # path-dep on piPy pi_agent + pi_ai
│   ├── oma_adapter/
│   │   ├── main.py                 # FastAPI: POST /internal/turn
│   │   ├── project.py              # OMA SessionEvent[] → piPy messages
│   │   ├── emit.py                 # AgentEvent stream → OMA events
│   │   ├── turn.py                 # create_agent_session + prompt
│   │   └── types.py                # DTOs (mirror OMA shared types)
│   └── tests/
│       ├── test_project.py
│       ├── test_turn.py
│       └── test_oma_contract.py    # golden fixtures from open-managed-agents/test/
├── docker-compose.yml
├── Dockerfile.platform
├── Dockerfile.harness
├── go.mod
└── README.md
```

---

## 3. Data model (minimal subset)

Mirror OMA IDs: `agent_*`, `sess_*`, `evt_*` prefixes via nanoid-style generator.

### Tables (SQLite)

```sql
-- migrations/001_core.sql
CREATE TABLE agents (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  config TEXT NOT NULL,          -- JSON AgentConfig
  version INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE TABLE agent_versions (
  agent_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  snapshot TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (agent_id, version)
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  agent_id TEXT NOT NULL,
  agent_version INTEGER NOT NULL,
  agent_snapshot TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'idle',  -- idle|running|interrupted|archived
  turn_id TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER
);

CREATE TABLE session_events (
  session_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,           -- JSON SessionEvent
  created_at INTEGER NOT NULL,
  PRIMARY KEY (session_id, seq)
);

CREATE INDEX idx_session_events_session_seq ON session_events(session_id, seq);
```

### Event types (MVP)

```json
{"type":"user.message","id":"evt_...","content":[{"type":"text","text":"..."}]}
{"type":"agent.message","id":"evt_...","content":[{"type":"text","text":"..."}]}
{"type":"agent.tool_use","id":"evt_...","name":"bash","input":{...}}
{"type":"agent.tool_result","tool_use_id":"...","content":[{"type":"text","text":"..."}]}
{"type":"session.lifecycle","phase":"turn_start|turn_end","turn_id":"..."}
```

---

## 4. Implementation tasks

### Task 1: Go module scaffold + health endpoint

**Files:**
- Create: `oma-building/go.mod`
- Create: `oma-building/cmd/oma-server/main.go`
- Create: `oma-building/internal/api/router.go`
- Create: `oma-building/internal/api/health.go`
- Test: `oma-building/internal/api/health_test.go`

- [ ] **Step 1: Write the failing test**

```go
// internal/api/health_test.go
package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
)

func TestHealthOK(t *testing.T) {
	handler := api.NewRouter(api.Deps{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd oma-building && go test ./internal/api/... -v`
Expected: FAIL — package or `NewRouter` not defined

- [ ] **Step 3: Write minimal implementation**

```go
// internal/api/health.go
package api

import "net/http"

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
```

Wire `GET /health` in `router.go` via `chi`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd oma-building && go test ./internal/api/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod cmd/ internal/api/
git commit -m "feat(platform): scaffold Go server with health endpoint"
```

---

### Task 2: SQLite migrations + agent store

**Files:**
- Create: `oma-building/migrations/001_core.sql`
- Create: `oma-building/internal/store/db.go`
- Create: `oma-building/internal/store/agents.go`
- Test: `oma-building/internal/store/agents_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCreateAgentIncrementsVersion(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db)
	ctx := context.Background()
	a, err := repo.Create(ctx, store.CreateAgentInput{
		Name: "demo",
		Model: "claude-sonnet-4-20250514",
		SystemPrompt: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != 1 {
		t.Fatalf("version=%d", a.Version)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/store/... -v`

- [ ] **Step 3: Implement `AgentRepo`**

- Apply `migrations/001_core.sql` on open
- `Create` inserts `agents` + `agent_versions` row
- `Get`, `List`, `Archive`, `Update` (bumps version)

AgentConfig JSON shape (minimal):

```go
type AgentConfig struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Version      int    `json:"version"`
}
```

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(store): SQLite agent repository with versioning"
```

---

### Task 3: REST `/v1/agents` routes

**Files:**
- Create: `oma-building/internal/api/agents.go`
- Modify: `oma-building/internal/api/router.go`
- Test: `oma-building/internal/api/agents_test.go`

- [ ] **Step 1: Failing HTTP test for `POST /v1/agents`**

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement handlers**

```http
POST   /v1/agents
GET    /v1/agents
GET    /v1/agents/:id
PATCH  /v1/agents/:id
POST   /v1/agents/:id/archive
```

Match OMA response field names (`id`, `name`, `model`, `version`, `created_at`).

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 4: Session + event log store

**Files:**
- Create: `oma-building/internal/store/sessions.go`
- Create: `oma-building/internal/store/events.go`
- Test: `oma-building/internal/store/events_test.go`

- [ ] **Step 1: Test append assigns monotonic `seq`**

```go
func TestAppendEventSeqMonotonic(t *testing.T) {
	// create session, append 3 events, seq must be 1,2,3
}
```

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement**

- `CreateSession` copies agent snapshot JSON into `sessions.agent_snapshot`
- `AppendEvents` in a transaction: `SELECT MAX(seq)+1` per session
- `ListEvents(sessionID, afterSeq, limit, order)`

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 5: REST `/v1/sessions` + events JSON API

**Files:**
- Create: `oma-building/internal/api/sessions.go`
- Test: `oma-building/internal/api/sessions_test.go`

- [ ] **Step 1: Test `POST /v1/sessions` + `POST .../events`**

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement**

```http
POST /v1/sessions
GET  /v1/sessions
GET  /v1/sessions/:id
POST /v1/sessions/:id/events
GET  /v1/sessions/:id/events?limit=100&order=asc&after_seq=0
```

`POST .../events` body: `{"events":[...]}` — for MVP only accept `user.message`; enqueue turn async.

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 6: SSE event stream hub

**Files:**
- Create: `oma-building/internal/stream/hub.go`
- Modify: `oma-building/internal/api/sessions.go` (add stream route)
- Test: `oma-building/internal/stream/hub_test.go`

- [ ] **Step 1: Test subscriber receives broadcast**

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement per-session fan-out**

- `GET /v1/sessions/:id/events/stream`
- `Content-Type: text/event-stream`
- Format: `id: <seq>\ndata: <json>\n\n`
- On connect with `?replay=1`, send historical events then tail

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 7: Session workdir provisioning (no Go tool exec)

**Files:**
- Create: `oma-building/internal/workdir/manager.go`
- Test: `oma-building/internal/workdir/manager_test.go`

- [ ] **Step 1: Test creates isolated directory per session**

```go
func TestEnsureWorkdirCreatesSessionDir(t *testing.T) {
	m := workdir.NewManager(t.TempDir())
	p, err := m.Ensure(context.Background(), "sess_test")
	// assert p is under base and exists
}
```

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement**

- Base: `$SANDBOX_WORKDIR`
- `Ensure(sessionID)` → `filepath.Join(base, sessionID)` with `0755`
- Optional `Remove(sessionID)` on archive (P1)

Tool execution lives in piPy harness (eng-review D4). Reference for path guard semantics only: `open-managed-agents/packages/sandbox/src/adapters/local-subprocess.ts`

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 8: Session state machine + harness dispatch

**Files:**
- Create: `oma-building/internal/session/machine.go`
- Create: `oma-building/internal/session/registry.go`
- Create: `oma-building/internal/harness/client.go` (HTTP client to Python)
- Test: `oma-building/internal/session/machine_test.go`

- [ ] **Step 1: Test turn marks session running then idle**

Port logic from `packages/session-runtime/src/machine.ts`:

1. `beginTurn` → `status=running`, mint `turn_id`
2. Call harness client with events + agent snapshot + **workdir path**
3. Harness returns new events → persist + SSE broadcast
4. `endTurn` → `status=idle`

- [ ] **Step 2: Run — FAIL**

- [ ] **Step 3: Implement**

`HarnessClient` interface:

```go
type HarnessClient interface {
	RunTurn(ctx context.Context, req HarnessTurnRequest) (HarnessTurnResponse, error)
}
```

For tests, use `FakeHarness` that emits one `agent.message`.

- [ ] **Step 4: Run — PASS**

- [ ] **Step 5: Commit**

---

### Task 9: Python harness sidecar (piPy SDK adapter)

**Files:**
- Create: `oma-building/harness/pyproject.toml` (path deps to `piPy-env/piPy`)
- Create: `oma-building/harness/oma_adapter/main.py`
- Create: `oma-building/harness/oma_adapter/project.py`
- Create: `oma-building/harness/oma_adapter/emit.py`
- Create: `oma-building/harness/oma_adapter/turn.py`
- Test: `oma-building/harness/tests/test_project.py`
- Test: `oma-building/harness/tests/test_turn.py`
- Test: `oma-building/harness/tests/test_oma_contract.py`

- [ ] **Step 1: Write failing pytest for projection + contract**

```python
# tests/test_oma_contract.py — load fixtures from ../../open-managed-agents/test/unit/
```

- [ ] **Step 2: Run — FAIL**

Run: `cd harness && uv sync && uv run pytest tests/ -v`

- [ ] **Step 3: Implement stateless turn**

```python
# POST /internal/turn
# body: { session_id, agent, events, workdir }
# returns: { events: [...] }
```

1. `project_oma_events(events)` → seed piPy session (in_memory)
2. `create_agent_session(cwd=workdir, model=agent.model, system_prompt=..., tools=["read","bash","write"], in_memory=True)`
3. `await session.prompt(latest_user_text)` — piPy runs tools locally in workdir
4. `emit_oma_events(subscription_buffer)` → return batch

Use `pi_ai.providers.faux` in unit tests; real Anthropic via `models.json` / env in compose.

Reference event shapes: `open-managed-agents/apps/agent/src/harness/default-loop.ts`. Loop engine: `piPy/packages/pi_coding_agent/src/pi_coding_agent/sdk.py`.

- [ ] **Step 4: Run pytest — PASS**

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(harness): piPy SDK adapter with OMA event mapping"
```

**Fallback:** If piPy adapter blocks >3 days, swap `turn.py` to direct Anthropic SDK — platform `HarnessClient` interface unchanged.

---

### Task 10: Wire platform ↔ harness in docker compose

**Files:**
- Create: `oma-building/docker-compose.yml`
- Create: `oma-building/Dockerfile.platform`
- Create: `oma-building/Dockerfile.harness`
- Create: `oma-building/.env.example`

- [ ] **Step 1: Compose services**

```yaml
services:
  oma-platform:
    build: { dockerfile: Dockerfile.platform }
    ports: ["8787:8787"]
    environment:
      DATABASE_PATH: /data/oma.db
      SANDBOX_WORKDIR: /data/sandboxes
      HARNESS_URL: http://oma-harness:8090
      OMA_API_KEY: ${OMA_API_KEY}
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
    volumes: ["./data:/data"]
  oma-harness:
    build: { dockerfile: Dockerfile.harness }
    environment:
      ANTHROPIC_API_KEY: ${ANTHROPIC_API_KEY}
```

- [ ] **Step 2: Run smoke test end-to-end**

Run: `docker compose up --build -d && <smoke script from §1>`

- [ ] **Step 3: Document in README.md**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat: docker compose for platform+harness MVP"
```

---

### Task 11: API key middleware + orphan recovery

**Files:**
- Create: `oma-building/internal/api/auth.go`
- Modify: `oma-building/cmd/oma-server/main.go` (boot recovery)
- Test: `oma-building/internal/api/auth_test.go`

- [ ] **Step 1: Test 401 without key, 200 with key**

- [ ] **Step 2: On boot, `UPDATE sessions SET status='interrupted' WHERE status='running'`**

Reference: `session-runtime/src/recovery.ts`

- [ ] **Step 3: Run tests — PASS**

- [ ] **Step 4: Commit**

---

## 5. Mapping from open-managed-agents (reference only)

| OMA module | MVP action |
|------------|------------|
| `apps/main-node` | Behavioral reference for self-host wiring |
| `packages/http-routes/sessions` | Route shapes + validation rules |
| `packages/session-runtime/machine.ts` | Port turn lifecycle |
| `apps/agent/harness/default-loop.ts` | Event shapes only; loop via piPy SDK |
| `apps/agent/harness/tools.ts` | Use piPy built-in read/bash/write in workdir |
| `packages/sandbox/adapters/local-subprocess.ts` | Path guard reference only; exec in piPy |
| `packages/event-log` | Port seq semantics to `session_events` |
| `apps/console` | **Not ported** — use curl / future UI |
| `apps/integrations` | **Not ported** |
| `packages/vaults-store` | **Not ported** |

---

## 6. Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Event schema drift from OMA | Copy JSON fixtures from `open-managed-agents/test/`; add contract test |
| Harness/platform split latency | Localhost HTTP; batch events per turn response |
| piPy ↔ OMA event mapping bugs | Golden-file tests in `test_adapter.py`; start with text-only messages |
| piPy version drift (0.0.1) | Path dep + pin commit after MVP; fallback to raw Anthropic loop |
| SQLite write races | Single-writer mutex per session in Go (lesson from `NodeHarnessRuntime`) |
| Sandbox escape | MVP: path prefix guard only; document "dev mode"; Phase 2: bubblewrap/firejail |
| No Console | Acceptable for MVP; optional thin static debug page in P1 |

---

## 7. Architecture diagram (MVP)

```
Client (curl / SDK)
    │  x-api-key
    ▼
┌─────────────────────────────────────┐
│  Go Platform (:8787)                │
│  chi router                         │
│  ├─ /v1/agents, /v1/sessions       │
│  ├─ event log (SQLite)              │
│  ├─ SSE hub                         │
│  ├─ session state machine           │
│  └─ workdir manager (mkdir only)    │
└───────┬─────────────────────────────┘
        │ POST /internal/turn { workdir, events, agent }
        ▼
┌─────────────────────────────────────┐
│  Python Harness (:8090)             │
│  FastAPI + piPy SDK                 │
│  ├─ project_oma_events()            │
│  ├─ create_agent_session(cwd=...)   │
│  └─ read/bash/write in workdir      │
└─────────────────────────────────────┘
        │
        ▼
   pi_ai → Anthropic API
```

OMA reference path for comparison: `apps/main-node` runs harness in-process (TypeScript). oma-building **splits** what OMA combines, but uses piPy instead of `default-loop.ts`.

## 8. Migration path comparison

| Path | Time to smoke test | Fits oma-building goals |
|------|-------------------|-------------------------|
| **A) This plan (Go + piPy adapter)** | ~2–3 weeks | Yes — greenfield, no CF, reusable harness |
| B) `docker compose up` on open-managed-agents | Hours | No — does not build oma-building |
| C) Fork `apps/main-node` (TypeScript) | ~1 week | Partial — same language as OMA, not Go/Python |
| D) Python monolith (no Go) | ~1 week | Partial — SSE/sandbox pain later |

## 9. Phase 2 backlog (post-MVP)

1. **SDK compatibility test** — run `@openma/sdk` create agent/session against oma-building
2. **Compaction** — enable piPy `compact` / `set_auto_compaction` behind OMA `agent.thread_context_compacted` events
3. **Vault sidecar** — Go MITM proxy or embed `oma-vault` pattern
4. **Memory stores** — R2/local-fs + `/mnt/memory` mount
5. **Extract hot paths to Rust** — sandbox executor crate via CGO if needed
6. **Console** — reuse OMA console pointing at oma-building API

---

## Self-review

**Spec coverage:** All P0 rows in §1 have Tasks 1–11. Smoke test in §1 is validated in Task 10.

**Placeholder scan:** No TBD steps; each task has concrete files and test snippets.

**Type consistency:** `AgentConfig`, `session_events.seq`, `HarnessTurnRequest` used consistently across Tasks 2–9.

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Codex Review | `/codex review` | Independent 2nd opinion | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | issues_open | 7 decisions; scope C accepted; piPy SDK path |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience gaps | 0 | — | — |

**UNRESOLVED:** 0 (all eng-review decisions recorded in §0 and Tasks 7–9)

**VERDICT:** Eng Review complete with accepted deltas — ready to implement after Task 1.

### Eng-review decisions (2026-06-07)

| ID | Decision |
|----|----------|
| D1 | Skip /office-hours; review existing plan |
| D2 | **C** Go platform + piPy adapter (not main-node fork, not greenfield loop) |
| D3 | **A** FastAPI sidecar + piPy SDK (not RPC subprocess) |
| D4 | **A** piPy owns tools; Go **workdir only** (Task 7 revised) |
| D5 | **A** Stateless turns; OMA SQLite sole history |
| D6 | **A** path dep to local `piPy-env/piPy` |
| D7 | **A** Contract tests from OMA test fixtures |

### NOT in scope (explicit deferrals)

- Fork/deploy `open-managed-agents/apps/main-node` as oma-building (fastest smoke test, wrong repo goal)
- Go `internal/sandbox` tool execution (superseded by piPy cwd tools)
- piPy RPC subprocess mode
- Dual piPy JSONL + OMA event log persistence
- Compaction, vault, integrations, console (unchanged from §1)

### What already exists (reuse, don't rebuild)

| Asset | Use |
|-------|-----|
| `open-managed-agents/apps/main-node` | Behavioral reference for self-host wiring |
| `open-managed-agents/packages/session-runtime` | Port `machine.ts` turn lifecycle to Go |
| `open-managed-agents/packages/http-routes` | Route/validation shapes |
| `open-managed-agents/test/unit/*.test.ts` | Golden event fixtures for contract tests |
| `piPy-env/piPy` | Harness engine (`create_agent_session`, tools, providers, faux for tests) |

### Failure modes (MVP)

| Scenario | Test? | Handling? | User-visible? |
|----------|-------|-----------|---------------|
| Harness timeout mid-turn | GAP → add in Task 8 | Mark `interrupted` on boot | SSE stalls then recovery |
| SQLite seq race (concurrent append) | Task 4 | Per-session mutex in Go | Duplicate/missing events |
| piPy projection drops tool_use id | Task 9 contract | Map `tool_use_id` in emit | Broken tool_result pairing |
| Workdir escape via `../` in tool path | GAP | piPy tools use cwd-relative guard | Silent wrong file read |
| ANTHROPIC_API_KEY missing | Task 10 compose | 502 from harness | Error in event log (add P1) |

### Implementation Tasks (eng-review synthesis)

- [ ] **T1 (P1, human: ~2h / CC: ~15min)** — Align plan Tasks 7–9 with piPy SDK + workdir-only Go
  - Surfaced by: Architecture — sandbox ownership conflict in original plan
  - Files: this plan, `internal/workdir/`, `harness/oma_adapter/`
  - Verify: plan diff review

- [ ] **T2 (P1, human: ~4h / CC: ~30min)** — `test_oma_contract.py` against OMA fixtures
  - Surfaced by: Test review — schema drift risk §6
  - Files: `harness/tests/test_oma_contract.py`
  - Verify: `uv run pytest tests/test_oma_contract.py`

- [ ] **T3 (P2, human: ~1d / CC: ~1h)** — Per-session turn mutex + harness timeout
  - Surfaced by: Architecture — OMA main-node seq race lesson
  - Files: `internal/session/registry.go`
  - Verify: concurrent append integration test

_No new tasks from Performance review (MVP load acceptable on localhost)._
