# open-managed-agents → oma-platform 迁移计划

> Engineering review by `/plan-eng-review` — 2026-06-13（矩阵同步代码实况）  
> 目标仓库：`oma-platform`（Go 平台 + Python piPy harness 侧车）  
> 参考源：`../open-managed-agents`（Cloudflare Workers meta-harness）  
> 已确认范围：**P0 + P1 + P2 主体已完成**；剩余为外围能力与生产硬化（T17+）

## 文档说明

本文档记录 `open-managed-agents` 与 `oma-platform` 的**功能对齐矩阵**与分阶段迁移 backlog。

早期版本（2026-06-07）假设 TypeScript `main-node` 复制路径；当前实现已改为 **Go `oma-server` + Python `harness/` 侧车**，矩阵以实际代码为准。验收脚本：`scripts/console-integration.sh`；Console QA 报告：`.gstack/qa-reports/qa-report-console-2026-06-13.md`（100/100）。

---

## 目标

严格对齐 OMA 协议与 Console 契约，使自托管栈达到与 `open-managed-agents` 相当的日常可用能力：

```
POST /v1/agents  →  POST /v1/sessions  →  POST /v1/sessions/:id/events
  →  Harness (piPy) 调 LLM + 沙箱工具  →  SSE 流式事件
```

冒烟路径（与源仓库 README 一致）：

```bash
AID=$(curl -s -X POST localhost:8787/v1/agents -H 'content-type: application/json' \
  -H "Authorization: Bearer $OMA_API_KEY" \
  -d '{"name":"hello","model":{"id":"claude-sonnet-4-6","speed":"standard"},"tools":[{"type":"agent_toolset_20260401"}]}' | jq -r .id)

SID=$(curl -s -X POST localhost:8787/v1/sessions -H 'content-type: application/json' \
  -H "Authorization: Bearer $OMA_API_KEY" \
  -d "{\"agent\":\"$AID\"}" | jq -r .id)

curl -s -X POST localhost:8787/v1/sessions/$SID/events -H 'content-type: application/json' \
  -H "Authorization: Bearer $OMA_API_KEY" \
  -d '{"events":[{"type":"user.message","content":[{"type":"text","text":"Run: uname -a"}]}]}'
```

Console 全量 wire 验收：`scripts/console-integration.sh`

---

## 架构对齐

### 当前拓扑（oma-platform）

```
┌─────────────────────────────────────────────────────────┐
│  oma-server (Go chi)                         :8787      │
│    /v1/agents | sessions | vaults | skills | …         │
│    session.Registry + stream.Hub (SSE)                   │
│    mcp-proxy · outbound-proxy · integrations             │
│    eval-worker · dream-worker · internal API             │
│    CONSOLE_DIR → Console SPA 同源                        │
├─────────────────────────────────────────────────────────┤
│  harness (Python piPy sidecar)               :8090      │
│    POST /internal/turn — 无状态 LLM 回合                  │
│    web_fetch · MCP · call_agent · compaction             │
├─────────────────────────────────────────────────────────┤
│  Storage: SQLite (oma.db) + 本地 FS                      │
│    sandboxes/ | skills/ | files/ | session-outputs/      │
└─────────────────────────────────────────────────────────┘
```

### 与 open-managed-agents 差异（有意保留）

| 维度 | open-managed-agents (CF) | oma-platform |
|------|--------------------------|--------------|
| API 入口 | `apps/main` (Hono Worker) | `cmd/oma-server` (Go) |
| Session 状态 | SessionDO + DO 内 SQLite | SQLite event log + in-process Registry |
| Brain | `apps/agent` DefaultHarness | `harness/oma_adapter` (piPy) |
| 沙箱 | CF Container per environment | `SANDBOX_WORKDIR/<session_id>/` |
| 集成 | 独立 `apps/integrations` Worker | 同进程 gateway + `integrations.go` |
| 对象存储 | R2 | 本地 `fileblob` / skill files |
| 多租户 | D1 分片 + KV | 单库 `tenant_id` 列 |

**Phase 3（defer）：** SessionDO、CF Container、R2 FUSE memory、Analytics Engine 计费、lane 部署、**browser 工具（T16）**。

---

## 功能对齐矩阵

图例：**✅ 已对齐** | **🟡 部分** | **❌ 未迁移** | **⏭ defer**

### P0 — 核心 Agent 闭环

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| Agent CRUD + 版本 | `packages/agents-store`, `buildAgentRoutes` | `internal/api/agents.go`, `store/agents.go` | ✅ | AMA wire 已测 |
| Session + event log | SessionDO, `event-log` | `sessions.go`, `store/events.go` | ✅ | |
| SSE 流 | SessionDO broadcast | `internal/stream/hub.go` | ✅ | |
| user.interrupt | SessionDO | `internal/api/pending.go` | ✅ | |
| Crash recovery | `runtime/recovery.ts` | `SessionRepo.RecoverRunning()` | ✅ | 启动时重置 orphan `running` |
| Harness turn | `harness/default-loop.ts` | `harness/oma_adapter/turn.py` | ✅ | HTTP 侧车，无状态 |
| agent_toolset 基础工具 | bash/read/write/edit/glob/grep | `harness/oma_adapter/tools.py` | ✅ | glob 映射为 piPy `find` |
| web_fetch | `harness/tools.ts` | `web_fetch/`, `extensions/web_fetch.py` | ✅ | T1 已完成；`test_web_fetch.py` |
| web_search | `harness/tools.ts` DEFAULT_TOOLS | — | ❌ | **T17 待做** |
| schedule / cancel_schedule / list_schedules | `harness/tools.ts` | — | ❌ | T18 可选；源仓默认 tool |
| MCP 工具 | `mcp-spawner.ts`, `/v1/mcp-proxy` | `mcp_loader` + `mcp_proxy.go` + `mcpproxy/` | ✅ | T2 已完成；`test_mcp.py` |
| Vault 凭据注入 | outbound proxy | `internal/outbound/` + harness 注入 | ✅ | T3 已完成；MCP + HTTP 双路径 |
| Model 解析 | model card + provider | `internal/modelresolve/` | ✅ | |
| POST /v1/models/list | `routes/models.ts` | `internal/api/models_list.go` | ✅ | T4 已完成；真实 provider 拉取 |
| GET /v1/models/list | 静态 catalog 探测 | `handleModelsListCatalogStub` | 🟡 | 探测用 stub，可接受 |
| Environment | `environments-store` | `environments.go` | ✅ | 无 per-env 容器镜像 |
| 沙箱隔离 | CF Container | `internal/workdir/` | 🟡 | 目录级，非容器 |
| Model card internal key | `/v1/internal/.../key` | `internal.go` + turn payload | ✅ | T5 |

### P1 — Console 完整可用 + 集成执行

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| Console 同源 SPA | main `assets` binding | `internal/console/` + `CONSOLE_DIR` | ✅ | 挂载 `open-managed-agents/apps/console/dist` |
| Auth (API key + cookie) | better-auth | `internal/auth/` | ✅ | `AUTH_UPSTREAM_URL` 或 `AUTH_DISABLED=1` |
| /v1/me, api_keys, tenants | main routes | `me.go`（tenants 在 `/v1/me/tenants`） | ✅ | |
| /v1/stats | `routes/stats.ts` | `stats.go` | ✅ | |
| Skills CRUD + zip upload | `routes/skills.ts` | `skills.go`, `skillzip/` | ✅ | |
| Files 上传/下载 | R2 + `files-store` | `files.go`, `fileblob/` | ✅ | 本地 blob，非 R2 |
| Model Cards CRUD | `model-cards-store` | `model_cards.go` | ✅ | |
| Vaults + credentials | `vaults-store` | `vaults.go`, `vaultoauth/` | ✅ | OAuth refresh 已有 |
| Session aux | threads/pending/trajectory/outputs | `session_threads.go`, `trajectory.go` | ✅ | T11；`subagent_e2e_test.go` |
| Integrations Linear/GH/Slack | `apps/integrations` | gateway + `linear/`, `github/`, `slack/` | ✅ | T6–T7 |
| Eval runs + worker | cron `tickEvalRuns` | `eval_runs.go`, `internal/eval/worker.go` | ✅ | T8 |
| Runtimes + ACP daemon | RuntimeRoom DO | `runtimes.go`, `runtime_daemon.go` | ✅ | T10 connect/exchange/attach |
| Memory stores + retention | R2 + FUSE + queue | `memory_stores.go`, retention cron | ✅ | T9 |
| OAuth (/v1/oauth/*) 通用 | `routes/oauth.ts` | 仅 Linear 专用 OAuth | ❌ | T21 defer；Linear 用 gateway 路径 |
| /v1/cap-cli/oauth | `routes/cap-cli-oauth.ts` | — | ❌ | T21 defer |
| Internal API (/v1/internal/*) | `routes/internal.ts` | `internal.go` + sessions/vaults | ✅ | T15 |
| /v1/integrations 聚合路由 | `routes/integrations.ts` | 分散 gateway 路由 | 🟡 | 能力有，路径形态不完全一致 |

### P2 — 平台 parity

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| call_agent / 子 Agent | `harness/tools.ts` | harness `call_agent/` | ✅ | T12 |
| Compaction 上下文压缩 | `harness/compaction.ts` | `compaction.py` | ✅ | T12 |
| Resource mounter | `runtime/resource-mounter.ts` | `resource_mounter.py` | ✅ | T13；`smoke-resource-live-e2e.sh` |
| Outcome evaluator | `harness/outcome-evaluator.ts` | `outcome_evaluator.py` + eval worker | ✅ | T13 |
| Dreams | `/v1/dreams`, `dreams-store` | `dreams.go`, `dream/worker.go` | ✅ | T14 |
| Cost report | `/v1/cost_report`, `cf-billing` | `cost_report.go`, `internal/usage/` | ✅ | T14 |
| browser tools | `harness/browser-tools.ts` OPT_IN | — | ⏭ | **T16 明确 defer** |
| clawhub | `routes/clawhub.ts` | — | ❌ | T21 Phase 3 |
| /v1/oma/* 路由别名 | main index | — | ❌ | T19 低优先级兼容 |
| Rate limiting | CF RL namespaces | — | ❌ | T20 Go middleware |
| Multi-tenant D1 分片 | `tenant-db` | 单 SQLite `tenant_id` | 🟡 | 够用至多 replica |
| SDK / CLI | `packages/sdk`, `packages/cli` | — | ❌ | T22 Phase 3 |
| RL 子系统 | `rl/` | — | ⏭ | 独立产品线 |

### ⏭ CF 专有 + 产品 defer（不在当前 sprint）

| 功能域 | 说明 |
|--------|------|
| SessionDO | Durable Object 强一致 session |
| CF Container 多环境 Worker | `SANDBOX_sandbox_<env>` binding |
| R2 Event → Queue → memory 索引 | FUSE 写 memory 审计 |
| Analytics Engine / cf-billing | 完整用量与计费 |
| Email OTP | `SEND_EMAIL` binding |
| lane 部署 | PR 级并行 Worker |
| browser 工具 (T16) | Playwright sidecar；源仓亦为 opt-in |

---

## 迁移进度摘要

| 类别 | 数量 | 说明 |
|------|------|------|
| ✅ 已对齐 | ~35 域 | Agent 闭环、web_fetch/MCP/outbound、Console、集成、eval、runtime、memory、P2 高级能力 |
| 🟡 部分 | ~5 域 | GET models/list stub、目录沙箱、集成路由形态、单库多租户 |
| ❌ 待迁 | ~7 域 | web_search（T17）、schedule 工具（T18）、oma 别名、限流、oauth 通用、clawhub、SDK |
| ⏭ defer | ~9 域 | SessionDO、Container、RL、browser（T16）、完整计费栈等 |

**VERDICT：** 自托管栈已达 open-managed-agents **日常可用 parity**；下一步 T17（web_search）+ 可选 T18–T22。

---

## 分阶段迁移路线

### Phase P0 — Harness 与凭据（已完成）

| ID | 任务 | 源参考 | oma 落点 | 状态 |
|----|------|--------|----------|------|
| P0-1 | web_fetch 工具 | `harness/tools.ts` | `web_fetch/`, `extensions/web_fetch.py` | ✅ T1 |
| P0-2 | MCP 客户端 + proxy | `mcp-spawner.ts`, `/v1/mcp-proxy` | `mcp/` + `mcp_proxy.go` | ✅ T2 |
| P0-3 | Vault outbound HTTP 代理 | agent outbound handler | `internal/outbound/` | ✅ T3 |
| P0-4 | 真实 POST /v1/models/list | `routes/models.ts` | `models_list.go` | ✅ T4 |
| P0-5 | model_cards internal key | internal routes | `internal.go` + turn payload | ✅ T5 |

### Phase P1 — 集成执行 + Eval + Runtime（已完成）

| ID | 任务 | 源参考 | oma 落点 | 状态 |
|----|------|--------|----------|------|
| P1-1 | Linear webhook → session | integrations/linear | gateway webhook | ✅ T6 |
| P1-2 | OAuth 回调 | linear pub oauth | `oauth.go` + gateway | ✅ T6 |
| P1-3 | GitHub/Slack webhook | integrations worker | `github/`, `slack/` | ✅ T7 |
| P1-4 | Eval run worker | `tickEvalRuns` | `internal/eval/worker.go` | ✅ T8 |
| P1-5 | Memory 大对象 + retention | R2 + queue | blob + retention cron | ✅ T9 |
| P1-6 | Runtime WebSocket attach | `/agents/runtime/_attach` | `runtime_daemon.go` | ✅ T10 |
| P1-7 | Session threads | SessionDO | `session_threads.go` | ✅ T11 |

### Phase P2 — 高级 Agent + 平台 API（主体已完成）

| ID | 任务 | 源参考 | oma 落点 | 状态 |
|----|------|--------|----------|------|
| P2-1 | call_agent 委派 | `harness/tools.ts` | `call_agent/` | ✅ T12 |
| P2-2 | Compaction | `compaction.ts` | `compaction.py` | ✅ T12 |
| P2-3 | Resource mounter | `resource-mounter.ts` | `resource_mounter.py` | ✅ T13 |
| P2-4 | Outcome evaluator | `outcome-evaluator.ts` | `outcome_evaluator.py` | ✅ T13 |
| P2-5 | Dreams API | `dreams-store` | `dreams.go` + worker | ✅ T14 |
| P2-6 | Cost report | `cf-billing` | `cost_report.go` | ✅ T14 |
| P2-7 | web_search | `harness/tools.ts` | harness tool + provider | ❌ **T17** |
| P2-8 | /v1/internal/* | `internal.ts` | `internal.go` | ✅ T15 |
| P2-9 | browser tools | `browser-tools.ts` | — | ⏭ **T16 defer** |

### Phase P3 — 剩余外围（当前 backlog）

| ID | 任务 | 源参考 | oma 落点 | 决策 |
|----|------|--------|----------|------|
| T16 | browser_* 工具 | `browser-tools.ts` | Playwright sidecar | **⏭ 明确 defer** |
| T17 | web_search | `harness/tools.ts` | harness + provider 选型 | **要做** |
| T18 | schedule 三件套 | `harness/tools.ts` | harness cron/queue | 可选 |
| T19 | `/v1/oma/*` 别名 | main index | `router.go` 重挂载 | P3 |
| T20 | Rate limiting | CF RL | Go middleware | P3 |
| T21 | 通用 oauth + clawhub | `oauth.ts`, `clawhub.ts` | 新 routes | P3 |
| T22 | SDK / CLI | `packages/sdk`, `cli` | 独立发布 | Phase 3 |

---

## 目标数据流（当前实况）

```
Client / Console
       │
       ▼
┌──────────────────────────────────────┐
│  oma-server (Go)                      │
│  agents · sessions · vaults           │
│  integrations · eval-worker · dreams  │
│  mcp-proxy · outbound-proxy           │
│  stream.Hub → SSE                     │
└──────────────┬───────────────────────┘
               │ POST /internal/turn
               ▼
┌──────────────────────────────────────┐
│  harness (Python piPy)                │
│  bash/file + web_fetch + MCP          │
│  call_agent · compaction              │
│  (+ web_search @ T17)                 │
└──────────────┬───────────────────────┘
               │ tools in workdir
               ▼
       SANDBOX_WORKDIR/<session_id>/
               │
               ▼
         SQLite session_events
```

---

## What already exists（oma-platform，勿重写）

| 能力 | 位置 |
|------|------|
| AMA Agent/Session wire | `internal/api/agentwire.go`, `sessionwire.go`, `*_ama_test.go` |
| Console 契约集成测试 | `scripts/console-integration.sh`, `p1_console_test.go`, QA 100/100 |
| web_fetch + MCP + outbound | `harness/oma_adapter/web_fetch/`, `mcp/`, `internal/outbound/` |
| models/list POST | `internal/api/models_list.go`, `internal/modelslist/` |
| DB migrations (001–014+) | `internal/store/migrations/` |
| Linear/GitHub/Slack gateway | `internal/integrations/*`, `internal/api/oauth.go` |
| Eval + dream workers | `internal/eval/worker.go`, `internal/dream/worker.go` |
| Session threads + subagent E2E | `session_threads.go`, `subagent_e2e_test.go` |
| Resource mounter + outcome eval | `resource_mounter.py`, `outcome_evaluator.py` |
| Internal API T15 | `internal.go`, `smoke-t15-e2e.sh` |
| Fake harness CI | `OMA_FAKE_HARNESS=1`, `internal/harness/fake.go` |
| Docker Compose | `docker-compose.yml`（platform + harness） |

---

## NOT in scope（明确不做 / defer）

- CF SessionDO / Durable Objects 重写  
- 每 Environment 独立 Container Worker  
- `rl/` 强化学习训练  
- 完整 cf-billing + Analytics Engine  
- **browser 工具（T16 明确 defer）** — 源仓亦为 opt-in；`web_fetch` 覆盖只读场景  
- `@openma/sdk` / `oma` CLI 发布（T22 Phase 3）  
- 整仓 TypeScript `main-node` 复制（已 supersede 为 Go 实现）

---

## Implementation Tasks

### 已完成（T1–T15）

- [x] **T1 (P0)** — harness `web_fetch` — `web_fetch/`, `extensions/web_fetch.py` — Verify: `harness/tests/test_web_fetch.py`
- [x] **T2 (P0)** — MCP 挂载 + `/v1/mcp-proxy` — `mcp/` + `internal/api/mcp_proxy.go` — Verify: `harness/tests/test_mcp.py`
- [x] **T3 (P0)** — Vault outbound HTTP 代理 — `internal/outbound/` — Verify: `internal/outbound/*_test.go`
- [x] **T4 (P0)** — 真实 `POST /v1/models/list` — `internal/api/models_list.go`
- [x] **T5 (P0)** — model card internal key — `internal.go` + turn payload
- [x] **T6 (P1)** — Linear webhook + OAuth — Verify: `scripts/smoke-linear-webhook.sh`
- [x] **T7 (P1)** — GitHub/Slack webhook 最小 E2E
- [x] **T8 (P1)** — Eval run background worker — `internal/eval/worker.go`
- [x] **T9 (P1)** — Memory blob + retention
- [x] **T10 (P1)** — Runtime WebSocket attach — `runtime_daemon.go`
- [x] **T11 (P1)** — Session threads 从 event log 派生 — `session_threads.go`
- [x] **T12 (P2)** — call_agent + compaction — harness
- [x] **T13 (P2)** — resource mounter + outcome evaluator — Verify: `./scripts/smoke-t13-e2e.sh`
- [x] **T14 (P2)** — Dreams + cost_report — Verify: `./scripts/smoke-t14-e2e.sh`
- [x] **T15 (P2)** — `/v1/internal/*` — Verify: `./scripts/smoke-t15-e2e.sh`

### 当前 backlog（T16–T22）

- [⏭] **T16 (P2)** — `browser_*` 工具 — Playwright sidecar — **明确 defer**（源仓 opt-in；`web_fetch` 已覆盖只读）
- [ ] **T17 (P2)** — `web_search` — harness tool + provider 选型 — Verify: `harness/tests/test_web_search.py` + agent turn smoke
- [ ] **T18 (P2)** — `schedule` / `cancel_schedule` / `list_schedules` — 若需与源默认 toolset 一致
- [ ] **T19 (P3)** — `/v1/oma/*` 路由别名 — `router.go`
- [ ] **T20 (P3)** — Rate limiting middleware
- [ ] **T21 (P3)** — 通用 `/v1/oauth` + clawhub
- [ ] **T22 (P3)** — SDK/CLI 发布

---

## 测试计划

### 已有覆盖

- Go API 单测/集成：`internal/api/*_test.go`
- Console wire + QA 100/100：`scripts/console-integration.sh`, `.gstack/qa-reports/qa-report-console-2026-06-13.md`
- Harness：`test_oma_contract.py`, `test_turn.py`, `test_web_fetch.py`, `test_mcp.py`
- Smoke：T13–T15、`smoke-resource-live-e2e.sh`、`subagent_e2e_test.go`

### T17 完成后新增

| 测试 | 位置 | 优先级 |
|------|------|--------|
| web_search unit | `harness/tests/test_web_search.py` | P2 |
| web_search turn smoke | `scripts/smoke-web-search-e2e.sh`（新建） | P2 |
| Agent 默认 toolset parity | 对照源 `DEFAULT_TOOLS` | P2 |

### 仍缺的 harness 路径（T17 后）

```
[+] POST /v1/sessions/:id/events
  ├── [★★★] uname -a / fake harness
  ├── [★★★] web_fetch + MCP tool loop
  ├── [GAP] web_search（T17）
  └── [GAP] schedule 三件套（T18 可选）

[+] browser_*（T16 defer — 不纳入当前 sprint）
```

---

## Failure modes（生产硬化 backlog）

| 路径 | 失败模式 | 测试 | 处理 | 用户可见 |
|------|----------|------|------|----------|
| resolveModel | API key 缺失 | 部分 | turn 级 error envelope | 需明确 message |
| harness loop | LLM rate limit | GAP | retry/backoff | SSE error event |
| bash tool | 命令 hang | 部分 | `HARNESS_HTTP_TIMEOUT_SEC` | partial output |
| MCP setup | upstream hang | 部分 | 15s timeout（对齐源仓） | 透明 error event |
| integration webhook | 签名失败 | 部分 | 401 + 日志 | 静默丢事件 |
| eval worker | 进程 crash | 部分 | 标记 failed + 重试 | Console 显示 failed |

---

## 并行化策略（剩余工作）

| Lane | 内容 | 依赖 |
|------|------|------|
| A | **T17 web_search**（harness + provider） | — |
| B | T18 schedule 工具（可选） | — |
| C | T19–T21 平台边缘（别名、限流、oauth） | 独立 |
| D | T22 SDK/CLI | 产品发布节奏 |

Lane A 为当前最高优先级；T16 browser 已 defer，不占用 lane。

---

## GSTACK REVIEW REPORT

| Review | Trigger | Runs | Status | Findings |
|--------|---------|------|--------|----------|
| Eng Review | `/plan-eng-review` | 3 | **CLEARED** | ~35✅ ~5🟡 ~7❌；T1–T15 完成 |
| Console QA | `/qa` | 1 | 100/100 | 2026-06-13 |
| CEO Review | — | 0 | — | — |
| Design Review | — | 0 | — | — |

- **SCOPE:** P0–P2 主体完成；T16 browser **defer**；T17 web_search **要做**
- **VERDICT:** 核心迁移完成 — 按 T17 实施 web_search，其余 T18–T22 按优先级排期

---

## 变更历史

| 日期 | 变更 |
|------|------|
| 2026-06-07 | 初版：TypeScript main-node MVP 计划 |
| 2026-06-11 | 重写：Go+Python 现状、P0/P1/P2 对齐矩阵、Implementation Tasks T1–T15 |
| 2026-06-13 | 矩阵同步代码实况：web_fetch/MCP/outbound/threads 等标 ✅；T1–T4 完成；新增 T16–T22；T16 defer、T17 要做；Console QA 100/100 |
