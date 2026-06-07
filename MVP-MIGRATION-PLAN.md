# OMA → oma-building MVP 迁移计划

> Engineering review by `/plan-eng-review` — 2026-06-07  
> 默认决策（用户跳过 D1）：**Node 自托管 MVP**，对齐 `open-managed-agents` README 冒烟测试路径。

## 目标

将 `open-managed-agents` 的**最小可运行闭环**迁入 `oma-building`：

```
POST /v1/agents  →  POST /v1/sessions  →  POST /v1/sessions/:id/events  →  Harness 调 LLM + 沙箱执行工具
```

验收标准（与源仓库 README.zh-CN 一致）：

```bash
AID=$(curl -s -X POST localhost:8787/v1/agents -H 'content-type: application/json' \
  -d '{"name":"hello","model":"claude-sonnet-4-6","tools":[{"type":"agent_toolset_20260401"}]}' | jq -r .id)

SID=$(curl -s -X POST localhost:8787/v1/sessions -H 'content-type: application/json' \
  -d "{\"agent\":\"$AID\"}" | jq -r .id)

curl -s -X POST localhost:8787/v1/sessions/$SID/events -H 'content-type: application/json' \
  -d '{"events":[{"type":"user.message","content":[{"type":"text","text":"Run: uname -a"}]}]}'
```

---

## Step 0 — Scope Challenge

### 1. 已有代码可复用什么？

| 子问题 | 源仓库已有方案 | MVP 建议 |
|--------|----------------|----------|
| Agent CRUD | `packages/agents-store` + `http-routes/buildAgentRoutes` | 直接复制 |
| Session 生命周期 | `packages/sessions-store` + `session-runtime` + `event-log` | 直接复制 |
| Harness（Brain） | `apps/agent/src/harness/*`（main-node 已 in-process 引用） | 复制 harness 子集，**不**搬 SessionDO |
| 沙箱 | `packages/sandbox` → `LocalSubprocessSandbox` | 仅 Node 适配器 |
| API 路由 | `packages/http-routes` | 先只挂 agents + sessions + health |
| 自托管入口 | `apps/main-node`（~1350 行 wiring） | 裁剪后作为 `apps/api` |
| 认证 | `AUTH_DISABLED=1` → `tenant_id=default` | MVP 默认关闭 auth |

### 2. 最小变更集

**In scope（MVP）**

- pnpm workspace 脚手架 + TypeScript + vitest
- ~18 个 package（见下表）
- 1 个 app：`apps/api`（由 `main-node` 裁剪）
- `docker-compose.yml`（单服务或 + oma-vault 可选）
- `.env.example` + `ANTHROPIC_API_KEY`

**NOT in scope（显式 defer）**

| 模块 | 理由 |
|------|------|
| `apps/main` + `apps/agent` Worker（CF SessionDO） | Phase 2；MVP 用 in-process harness |
| `apps/integrations`（Linear/GitHub/Slack） | 非冒烟路径 |
| `apps/console` | MVP 用 curl；Console 可 iframe 同源 later |
| `rl/` 强化学习子系统 | 独立产品线 |
| `packages/dreams-*`, `evals-*` | 高级功能 |
| 多租户 D1 路由（`tenant-db`, `tenant-dbs-store`） | 单 tenant SQLite 够用 |
| `acp-runtime`, bridge daemon | 本地 Claude Code 委派，非核心 |
| `cf-billing`, Analytics Engine | 运维/计费 |
| Postgres 路径 | SQLite 先通，Postgres adapter 已有可 Phase 1.5 启用 |
| Model Cards UI / Vault UI | API 层可后加；MVP 用 env `ANTHROPIC_API_KEY` |

### 3. 复杂度 smell

全量迁移 = **52 packages × 4 apps** → 触发 scope reduction。

MVP 切片 = **~18 packages × 1 app** → 可接受。

### 4. Search / Layer 标注

| 选型 | Layer | 说明 |
|------|-------|------|
| 复用 `http-routes` + store 抽象 | **[Layer 1]** | 已验证的分层，勿重写 REST |
| 复用 `DefaultHarness` + `buildTools` | **[Layer 1]** | Brain 可插拔接口已存在 |
| LocalSubprocess 沙箱 | **[Layer 1]** | docker-compose 默认路径 |
| 新建简化 Agent loop | **[Layer 3]** | ❌ 不推荐 — 丢 event-log/recovery 语义 |
| 整仓 copy 再删 | **[Layer 2]** | 快但债高；仅当时间极紧 |

### 5. Completeness（Boil the Lake）

MVP 应包含：

- Event log 持久化 + crash recovery 测试（`session-runtime` 已有）
- `agent_toolset_20260401` 基础工具（bash/file/web_fetch）
- 单元测试：`agents-store`, `sessions-store`, `event-log`, harness smoke

MVP 可省略完整 E2E Playwright、100% 集成测试覆盖 — 但**必须有**上述 core 的 vitest。

---

## 架构

### 目标拓扑（oma-building MVP）

```
┌─────────────────────────────────────────────────────────┐
│  apps/api (Hono + Node serve)                           │
│    ├── /v1/agents      ← http-routes/buildAgentRoutes   │
│    ├── /v1/sessions    ← http-routes/buildSessionRoutes │
│    ├── /health                                         │
│    └── in-process: DefaultHarness + NodeSessionRouter   │
├─────────────────────────────────────────────────────────┤
│  packages/                                             │
│    shared, api-types, schema, sql-client               │
│    agents-store, sessions-store, environments-store    │
│    event-log, session-runtime, sandbox, markdown       │
│    http-routes (trimmed exports), observability        │
│    kv-store (api keys — optional Phase 1.1)            │
├─────────────────────────────────────────────────────────┤
│  Storage: SQLite (oma.db) + local FS (sandboxes, blobs) │
│  Sandbox: LocalSubprocessSandbox                       │
└─────────────────────────────────────────────────────────┘
```

### 与源架构差异（ intentional ）

| 维度 | open-managed-agents (CF) | oma-building MVP |
|------|--------------------------|------------------|
| Session 状态 | SessionDO + SQLite in DO | SQL event log + in-process router |
| 沙箱 | CF Container per env | LocalSubprocess 单进程 |
| Brain 位置 | agent Worker isolate | main-node 同进程 |
| 多环境 Worker | `SANDBOX_*` binding | 单 sandbox workdir |

**Phase 2 迁移键：** `session-runtime` 的 ports（`SessionRouter`, `SessionLifecycle`）不变；替换 `NodeSessionRouter` → `CfSessionRouter` 即可上 CF。

### Package 迁移清单（18）

```
packages/
  shared/
  api-types/
  schema/
  sql-client/
  agents-store/
  sessions-store/
  environments-store/
  event-log/
  session-runtime/
  sandbox/
  markdown/
  http-routes/          # 初期只 export agents + sessions + health helpers
  observability/
  kv-store/             # 若 MVP 需要 API key；否则 defer
  blob-store/           # session outputs 可选
  files-store/          # 若 tool 需要 promote file；可 Phase 1.1

apps/
  harness/              # 从 apps/agent 抽出 harness/ 为独立 package 或 apps/harness
  api/                  # 从 apps/main-node 裁剪
```

**[P1] (confidence: 9/10)** — 不要把 `apps/agent` 整包复制：其中 SessionDO、CF Sandbox、outbound proxy 与 CF 强耦合。只 export harness 子路径（`default-loop`, `tools`, `provider`）。

**[P1] (confidence: 8/10)** — `http-routes` 依赖链过宽（integrations, dreams, evals）。MVP 应 fork 为 `http-routes-core` 或在 `oma-building` 内新建薄路由层，只 re-export agents/sessions，避免拉入 10+ 无关 store。

### 数据流（ASCII）

```
Client                    apps/api                 SQLite
  │                          │                       │
  │ POST /v1/agents          │                       │
  ├─────────────────────────►│ agents-store.insert   ├──► agents
  │                          │                       │
  │ POST /v1/sessions        │                       │
  ├─────────────────────────►│ sessions-store        ├──► sessions
  │                          │ SqlEventLog.init      ├──► session_events
  │                          │                       │
  │ POST .../events          │                       │
  ├─────────────────────────►│ append user.message   ├──► session_events
  │                          │ DefaultHarness.run    │
  │                          │   ├─ resolveModel     │
  │                          │   ├─ buildTools       │
  │                          │   └─ LocalSubprocess  ├──► bash in workdir
  │                          │ append assistant/tool ├──► session_events
  │◄─────────────────────────┤ SSE or sync response  │
```

### 安全架构（MVP）

- `AUTH_DISABLED=1` 仅本地；生产必须 `BETTER_AUTH_SECRET` + API key
- 沙箱：`SANDBOX_WORKDIR` 隔离；禁止挂载 host `/`
- 密钥：MVP 用 `ANTHROPIC_API_KEY` env；Vault/outbound 代理 Phase 1.1

---

## What already exists（源仓库可复用）

| 能力 | 位置 | MVP 是否复用 |
|------|------|-------------|
| Agent 版本化 CRUD | `packages/agents-store` | ✅ |
| Session + event log | `packages/sessions-store`, `event-log` | ✅ |
| Harness loop | `apps/agent/src/harness/` | ✅（extract） |
| REST 路由定义 | `packages/http-routes/src/agents.ts`, `sessions.ts` | ✅ |
| Node 自托管 wiring 参考 | `apps/main-node/src/index.ts` | ✅（裁剪模板） |
| Docker 部署 | `docker-compose.yml`, `apps/main-node/Dockerfile` | ✅ |
| CF SessionDO 恢复 | `apps/agent/src/session-do.ts` | ❌ Phase 2 |
| Console UI | `apps/console` | ❌ Phase 2 |
| SDK/CLI 发布 | `packages/sdk`, `packages/cli` | ❌ Phase 3 |

---

## 测试计划

### 代码路径 + 用户流覆盖图

```
CODE PATHS                                            USER FLOWS
[+] POST /v1/agents                                   [+] Create agent via API
  ├── [GAP] validation errors                           ├── [GAP] invalid model rejection
  ├── [GAP] duplicate name                              └── [GAP] list/get/archive
  └── [GAP] tool config variants

[+] POST /v1/sessions                                 [+] Start session
  ├── [GAP] missing agent                               ├── [GAP] default environment
  ├── [GAP] agent version binding                       └── [GAP] session list

[+] POST /v1/sessions/:id/events                     [+] Send message → get response
  ├── [GAP] user.message append                         ├── [★★★ TESTED] uname -a smoke (e2e manual)
  ├── [GAP] harness tool call loop                      ├── [GAP] SSE stream subscribe
  ├── [GAP] crash recovery replay                       └── [GAP] error when API key missing
  └── [GAP] concurrent events

[+] DefaultHarness.runLoop                          
  ├── [GAP] model resolution (env vs model card)       
  ├── [GAP] tool execution timeout                     
  └── [GAP] max turns limit                            

[+] LocalSubprocessSandbox                           
  ├── [GAP] command allowlist                          
  └── [GAP] workdir isolation                          

COVERAGE: 1/15 paths tested (7%)  |  MVP target: 80% on in-scope paths before ship
GAPS: 14 (0 E2E automated — add vitest + one docker smoke script)
```

### MVP 必须补的测试

| 测试 | 文件 | 优先级 |
|------|------|--------|
| Agent CRUD roundtrip | `test/unit/agents.test.ts` | P1 |
| Session create + event append | `test/unit/sessions.test.ts` | P1 |
| Event log replay | 复用 `event-log` 现有 tests | P1 |
| Harness mock LLM one turn | `test/unit/harness-smoke.test.ts` | P1 |
| Docker health + smoke script | `scripts/smoke.sh` | P1 |
| Crash recovery | 移植 `apps/main-node/test/crash-recovery.test.ts` | P2 |

### Test plan artifact（供 /qa）

- **Affected routes:** `POST /v1/agents`, `GET /v1/agents/:id`, `POST /v1/sessions`, `POST /v1/sessions/:id/events`, `GET /health`
- **Critical path:** create agent → create session → send "Run: uname -a" → response contains kernel string
- **Edge cases:** missing `ANTHROPIC_API_KEY`, invalid agent id, empty message

---

## 性能（MVP 级别）

| 发现 | 严重度 | 建议 |
|------|--------|------|
| in-process harness 阻塞 HTTP | P2 | MVP 可 sync；Phase 1.1 加 SSE + background turn |
| SQLite 单写 | P2 | 单实例 MVP 可接受；多 replica 需 Postgres |
| 沙箱无 pool | P3 | LocalSubprocess 冷启动可接受 |

---

## 并行化策略

| Step | Modules | Depends on |
|------|---------|------------|
| S1: workspace scaffold | root package.json, tsconfig | — |
| S2: copy core packages | packages/shared … sql-client | S1 |
| S3: copy stores + event-log | agents/sessions/environments-store, event-log | S2 |
| S4: extract harness package | apps/harness from agent/harness | S2 |
| S5: sandbox + session-runtime | sandbox, session-runtime | S2, S4 |
| S6: http-routes-core | trimmed routes | S3 |
| S7: apps/api wiring | apps/api | S3–S6 |
| S8: docker + smoke | docker-compose, scripts | S7 |

**Lanes:**

- **Lane A:** S1 → S2 → S3 → S6（数据层 + API 契约）
- **Lane B:** S4 → S5（运行时 + 沙箱）— 与 A 并行，在 S2 完成后启动
- **Lane C:** S7 → S8 — 依赖 A+B 合并

冲突：`http-routes` 与 `apps/api` 都 touch 路由挂载 — 顺序执行 S6 再 S7。

---

## Implementation Tasks

- [ ] **T1 (P1, human: ~2h / CC: ~20min)** — 初始化 `oma-building` pnpm workspace + tsconfig 基线
  - Surfaced by: Architecture — 空目录需要 monorepo 脚手架
  - Files: `package.json`, `pnpm-workspace.yaml`, `tsconfig.json`
  - Verify: `pnpm install` 成功

- [ ] **T2 (P1, human: ~4h / CC: ~45min)** — 复制 Tier-0 packages（shared, api-types, schema, sql-client）
  - Surfaced by: Architecture — 所有 store 的公共依赖
  - Files: `packages/shared`, `packages/api-types`, `packages/schema`, `packages/sql-client`
  - Verify: `pnpm exec tsc --noEmit` 在 packages 层通过

- [ ] **T3 (P1, human: ~4h / CC: ~45min)** — 复制 store 层 + event-log + environments-store
  - Surfaced by: Architecture — Agent/Session CRUD
  - Files: `packages/agents-store`, `sessions-store`, `environments-store`, `event-log`
  - Verify: 移植对应 `test/unit/*-store*.test.ts`

- [ ] **T4 (P1, human: ~3h / CC: ~30min)** — 抽出 `apps/harness`（从 `apps/agent/src/harness`）
  - Surfaced by: Architecture — 避免 CF SessionDO 耦合
  - Files: `apps/harness/` 或 `packages/harness/`
  - Verify: harness unit tests pass with mocked model

- [ ] **T5 (P1, human: ~3h / CC: ~30min)** — 复制 sandbox + session-runtime
  - Files: `packages/sandbox`, `packages/session-runtime`, `packages/markdown`
  - Verify: `session-runtime` vitest green

- [ ] **T6 (P1, human: ~4h / CC: ~40min)** — 创建 `packages/http-routes-core`（agents + sessions only）
  - Surfaced by: Code quality — 避免 http-routes 全量依赖
  - Files: 从 `http-routes/src/agents.ts`, `sessions.ts` 提取
  - Verify: route handler 单测

- [ ] **T7 (P1, human: ~6h / CC: ~60min)** — 实现 `apps/api`（裁剪 main-node）
  - Files: `apps/api/src/index.ts`, `lib/node-session-router.ts`, `lib/node-session-lifecycle.ts`
  - Verify: `pnpm --filter api dev` + curl 冒烟

- [ ] **T8 (P1, human: ~2h / CC: ~20min)** — docker-compose + `.env.example` + `scripts/smoke.sh`
  - Verify: `docker compose up` + smoke script exit 0

- [ ] **T9 (P2, human: ~3h / CC: ~30min)** — 移植 crash-recovery 测试
  - Files: `apps/api/test/crash-recovery.test.ts`
  - Verify: vitest pass

- [ ] **T10 (P3, human: ~1d / CC: ~2h)** — Phase 2 占位：文档说明 CF 迁移路径
  - Files: `docs/phase2-cloudflare.md`

_No new tasks from Performance — MVP defer._

---

## 命名与仓库策略

**推荐（issue 2A）：** package 名暂保留 `@open-managed-agents/*`，减少 import  churn；`oma-building` README 声明 fork 关系。Phase 1 稳定后再批量 rename → `@oma/*`。

**备选（2B）：** 复制时立即 rename → 一次性 diff 大，但边界清晰。

---

## Failure modes（MVP 必须处理）

| 路径 | 生产失败模式 | 测试? | 错误处理? | 用户可见? |
|------|-------------|-------|-----------|-----------|
| resolveModel | API key 缺失/过期 | GAP | 需 401 envelope | 需明确 error message |
| harness loop | LLM rate limit | GAP | retry/backoff | 需 SSE error event |
| bash tool | 命令 hang | GAP | timeout kill | partial output |
| event append | SQLite locked | GAP | retry | 500 |
| session not found | stale session id | GAP | 404 | ✅ |

**Critical gap:** API key 缺失时 silent hang — 必须在 T7 加启动校验 + 请求级 error envelope。

---

## Completion Summary（Review）

- Step 0: Scope Challenge — **scope reduced** to Node MVP (~18 packages, 1 app)
- Architecture Review: **4** issues (harness extract, http-routes trim, SessionDO defer, naming)
- Code Quality Review: **2** issues (main-node 1350-line wiring → split bootstrap module)
- Test Review: diagram produced, **14 gaps** identified; target 80% on in-scope paths
- Performance Review: **0** blocking; 2 deferred
- NOT in scope: written above
- What already exists: written above
- TODOS.md updates: see Phase 2/3 defer list (no separate TODOS.md yet)
- Failure modes: **1** critical gap (API key / error envelope)
- Outside voice: skipped
- Parallelization: 3 lanes (A/B parallel → C)
- Lake Score: 8/10 — recommend complete event-log + recovery tests, not shortcut sync-only API

---

## GSTACK REVIEW REPORT

| Review | Trigger | Why | Runs | Status | Findings |
|--------|---------|-----|------|--------|----------|
| CEO Review | `/plan-ceo-review` | Scope & strategy | 0 | — | — |
| Eng Review | `/plan-eng-review` | Architecture & tests (required) | 1 | issues_open | 4 arch + 2 quality + 14 test gaps |
| Design Review | `/plan-design-review` | UI/UX gaps | 0 | — | — |
| DX Review | `/plan-devex-review` | Developer experience | 0 | — | — |

- **UNRESOLVED:** MVP 命名策略（保留 @open-managed-agents vs rename @oma）；是否 Phase 1 包含 oma-vault sidecar
- **VERDICT:** ENG REVIEW COMPLETE (plan stage) — ready to implement after naming decision
