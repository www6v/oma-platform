# open-managed-agents → oma-platform 迁移计划

> Engineering review by `/plan-eng-review` — 2026-06-11  
> 目标仓库：`oma-platform`（Go 平台 + Python piPy harness 侧车）  
> 参考源：`../open-managed-agents`（Cloudflare Workers meta-harness）  
> 已确认范围：**P0 + P1 + P2**（不含 CF SessionDO / Container 重写）

## 文档说明

本文档记录 `open-managed-agents` 与 `oma-platform` 的**功能对齐矩阵**与分阶段迁移 backlog。

早期版本（2026-06-07）假设 TypeScript `main-node` 复制路径；当前实现已改为 **Go `oma-server` + Python `harness/` 侧车**，矩阵以实际代码为准。验收脚本：`scripts/console-integration.sh`。

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
│    integrations | eval CRUD | runtimes | memory_stores   │
│    CONSOLE_DIR → Console SPA 同源                        │
├─────────────────────────────────────────────────────────┤
│  harness (Python piPy sidecar)               :8090      │
│    POST /internal/turn — 无状态 LLM 回合                  │
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
| 集成 | 独立 `apps/integrations` Worker | 同进程 `internal/api/integrations.go` |
| 对象存储 | R2 | 本地 `fileblob` / skill files |
| 多租户 | D1 分片 + KV | 单库 `tenant_id` 列 |

**Phase 3（defer）：** SessionDO、CF Container、R2 FUSE memory、Analytics Engine 计费、lane 部署。

---

## 功能对齐矩阵

图例：**✅ 已对齐** | **🟡 部分** | **❌ 未迁移** | **⏭ CF 专有 defer**

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
| web_fetch | `harness/tools.ts` | — | ❌ | P0-1 |
| web_search | Workers AI | — | ❌ | P2-7 或 defer |
| MCP 工具 | `mcp-spawner.ts`, `/v1/mcp-proxy` | Agent 可声明 `mcp_servers` | 🟡 | 无 proxy；turn 未挂载 MCP（P0-2） |
| Vault 凭据注入 | outbound proxy | Vault CRUD + `mcp_oauth_validate` | 🟡 | 沙箱 HTTP 不经 vault 代理（P0-3） |
| Model 解析 | model card + provider | `internal/modelresolve/` | ✅ | |
| POST /v1/models/list | `apps/main/routes/models.ts` | `console_stubs.go` 静态列表 | 🟡 | P0-4 |
| Environment | `environments-store` | `environments.go` | ✅ | 无 per-env 容器镜像 |
| 沙箱隔离 | CF Container | `internal/workdir/` | 🟡 | 目录级，非容器 |
| Model card internal key | `/v1/internal/.../key` | turn payload + internal resolve | ✅ | harness 解析用（P0-5） |

### P1 — Console 完整可用 + 集成执行

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| Console 同源 SPA | main `assets` binding | `internal/console/` + `CONSOLE_DIR` | ✅ | 挂载 `open-managed-agents/apps/console/dist` |
| Auth (API key + cookie) | better-auth | `internal/auth/` | ✅ | `AUTH_UPSTREAM_URL` 或 `AUTH_DISABLED=1` |
| /v1/me, api_keys, tenants | main routes | `me.go`, `tenant.go` | ✅ | |
| /v1/stats | `routes/stats.ts` | `stats.go` | ✅ | |
| Skills CRUD + zip upload | `routes/skills.ts` | `skills.go`, `skillzip/` | ✅ | |
| Files 上传/下载 | R2 + `files-store` | `files.go`, `fileblob/` | ✅ | 本地 blob，非 R2 |
| Model Cards CRUD | `model-cards-store` | `model_cards.go` | ✅ | |
| Vaults + credentials | `vaults-store` | `vaults.go`, `vaultoauth/` | ✅ | OAuth refresh 已有 |
| Session aux | threads/pending/trajectory/outputs | `session_aux.go`, `trajectory.go` | 🟡 | `threads` 仍为 stub（P1-7） |
| Integrations Linear/GH/Slack | `apps/integrations` | `integrations.go`, `oauth.go`, `linear/` | 🟡 | Linear **webhook + OAuth 已接**（P1-1/2）；GH/Slack 仍无执行（P1-3） |
| Eval runs | cron `tickEvalRuns` | `eval_runs.go` | 🟡 | CRUD + `pending`；**无后台 worker**（P1-4） |
| Runtimes + ACP daemon | RuntimeRoom DO | `runtimes.go`, `runtime_daemon.go` | 🟡 | connect/exchange 有；**无 WS attach**（P1-6） |
| Memory stores | R2 + FUSE + queue | `memory_stores.go` | 🟡 | SQLite 内联 content；无 retention cron（P1-5） |
| OAuth (/v1/oauth/*) | `routes/oauth.ts` | — | ❌ | 通用 OAuth defer；Linear 用 `/linear/oauth/pub/{id}` |
| Internal API (/v1/internal/*) | `routes/internal.ts` | `internal.go` | 🟡 | model_cards key/resolve + Linear mock bind；其余 P2-8 |

### P2 — 平台 parity

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| call_agent / 子 Agent | `harness/tools.ts` | — | ❌ | P2-1 |
| Compaction 上下文压缩 | `harness/compaction.ts` | — | ❌ | P2-2 |
| Resource mounter | `runtime/resource-mounter.ts` | — | ❌ | memory/files/github（P2-3） |
| Outcome evaluator | `harness/outcome-evaluator.ts` | — | ❌ | eval 依赖（P2-4） |
| Dreams | `/v1/dreams`, `dreams-store` | — | ❌ | P2-5 |
| Cost report | `/v1/cost_report`, `cf-billing` | — | ❌ | 简化 token 聚合（P2-6） |
| browser tools | `harness/browser-tools.ts` | — | ❌ | P2-7 |
| /v1/oma/* 路由别名 | main index | — | ❌ | 低优先级兼容 |
| Rate limiting | CF RL namespaces | — | ❌ | 可用 Go middleware |
| Multi-tenant D1 分片 | `tenant-db` | 单 SQLite `tenant_id` | 🟡 | 够用至多 replica |
| SDK / CLI | `packages/sdk`, `packages/cli` | — | ❌ | Phase 3 |
| RL 子系统 | `rl/` | — | ⏭ | 独立产品线 |

### ⏭ CF 专有（Phase 3+，不在 P0–P2）

| 功能域 | 说明 |
|--------|------|
| SessionDO | Durable Object 强一致 session |
| CF Container 多环境 Worker | `SANDBOX_sandbox_<env>` binding |
| R2 Event → Queue → memory 索引 | FUSE 写 memory 审计 |
| Analytics Engine / cf-billing | 用量与计费 |
| Email OTP | `SEND_EMAIL` binding |
| lane 部署 | PR 级并行 Worker |

---

## 迁移进度摘要

| 类别 | 数量 | 说明 |
|------|------|------|
| ✅ 已对齐 | ~22 域 | 含 Agent/Session/SSE、Console 挂载、Skills/Files/Vault 等 |
| 🟡 部分 | ~12 域 | 集成、eval、runtime、memory、models/list、MCP/vault |
| ❌ 待迁 (P0–P2) | ~15 域 | 见下方 Implementation Tasks |
| ⏭ CF defer | ~8 域 | SessionDO、Container、RL 等 |

---

## 分阶段迁移路线

### Phase P0 — Harness 与凭据（优先）

| ID | 任务 | 源参考 | oma 落点 | 验收 |
|----|------|--------|----------|------|
| P0-1 | web_fetch 工具 | `harness/tools.ts` | `harness/oma_adapter/tools.py`, `turn.py` | Agent 可抓取 URL 并写入 event |
| P0-2 | MCP 客户端 + 可选 proxy | `mcp-spawner.ts`, `/v1/mcp-proxy` | harness + `internal/api/mcp_proxy.go` | 声明的 mcp_server 可调用 |
| P0-3 | Vault outbound HTTP 代理 | agent outbound handler | `internal/outbound/` | 凭据不出沙箱明文 |
| P0-4 | 真实 POST /v1/models/list | `routes/models.ts` | 替换 `handleModelsListStub` | 用 model card key 拉 provider 列表 |
| P0-5 | model_cards internal key | internal routes | harness turn 或 Go internal 端点 | 侧车解析真实 API key |

### Phase P1 — 集成执行 + Eval + Runtime

| ID | 任务 | 源参考 | oma 落点 | 验收 |
|----|------|--------|----------|------|
| P1-1 | Linear webhook → session | `integrations/routes/linear` | webhook handlers + dispatch | issue → `user.message` |
| P1-2 | OAuth 回调 | `/v1/oauth`, linear pub oauth | `internal/api/oauth.go` | publication install 完成 |
| P1-3 | GitHub/Slack webhook 最小集 | integrations worker | 同进程或 sidecar | 至少一种 provider E2E |
| P1-4 | Eval run worker | `tickEvalRuns` | `internal/eval/worker.go` | pending→running→completed |
| P1-5 | Memory 大对象 + retention | R2 + queue | 本地 FS 或 S3；cron | 大 memory 不撑 SQLite |
| P1-6 | Runtime WebSocket attach | `/agents/runtime/_attach` | `runtime_daemon.go` | ACP 本地 IDE smoke |
| P1-7 | Session threads 真实数据 | SessionDO | 从 event log 派生 | Console 非 stub |

### Phase P2 — 高级 Agent + 平台 API

| ID | 任务 | 源参考 | oma 落点 |
|----|------|--------|----------|
| P2-1 | call_agent 委派 | `harness/tools.ts` | harness + session API |
| P2-2 | Compaction | `compaction.ts` | turn 前事件摘要 |
| P2-3 | Resource mounter | `resource-mounter.ts` | turn 前挂载 memory/files |
| P2-4 | Outcome evaluator | `outcome-evaluator.ts` | eval worker |
| P2-5 | Dreams API | `dreams-store` | 新 store + routes |
| P2-6 | Cost report | `cf-billing` | session token 聚合 |
| P2-7 | browser / web_search | browser-tools | Playwright sidecar 或 defer |
| P2-8 | /v1/internal/* | `internal.ts` | 供 integrations 拆分 |

---

## 目标数据流（P1 完成后）

```
Client / Console
       │
       ▼
┌──────────────────────────────────────┐
│  oma-server (Go)                      │
│  agents · sessions · vaults           │
│  integrations · eval-worker · oauth   │
│  outbound-proxy · stream.Hub → SSE    │
└──────────────┬───────────────────────┘
               │ POST /internal/turn
               ▼
┌──────────────────────────────────────┐
│  harness (Python piPy)                │
│  bash/file + web_fetch + MCP          │
│  (+ call_agent / compaction @ P2)     │
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
| Console 契约集成测试 | `scripts/console-integration.sh`, `p1_console_test.go` |
| DB migrations (001–011) | `internal/store/migrations/` |
| Linear gateway (OAuth/webhook) | `internal/api/oauth.go`, `internal/integrations/linear/` |
| Linear webhook smoke | `scripts/smoke-linear-webhook.sh` |
| Fake harness CI | `OMA_FAKE_HARNESS=1`, `internal/harness/fake.go` |
| Trajectory 导出 | `internal/api/trajectory.go` |
| Tool confirmation / pending | `internal/api/pending.go` |
| Crash recovery 测试 | `crash_recovery_test.go`, `sessions_recovery_test.go` |
| Docker Compose | `docker-compose.yml`（platform + harness） |

---

## NOT in scope（P0–P2 明确不做）

- CF SessionDO / Durable Objects 重写  
- 每 Environment 独立 Container Worker  
- `rl/` 强化学习训练  
- 完整 cf-billing + Analytics Engine  
- `@openma/sdk` / `oma` CLI 发布（Phase 3）  
- 整仓 TypeScript `main-node` 复制（已 supersede 为 Go 实现）

---

## Implementation Tasks

- [ ] **T1 (P0)** — harness `web_fetch` — `harness/oma_adapter/tools.py`, `turn.py` — Verify: turn 返回 fetch 结果 event
- [ ] **T2 (P0)** — MCP 挂载 + 可选 `/v1/mcp-proxy` — harness + `internal/api/mcp_proxy.go`
- [ ] **T3 (P0)** — Vault outbound HTTP 代理 — `internal/outbound/`
- [ ] **T4 (P0)** — 真实 `POST /v1/models/list` — 替换 `internal/api/console_stubs.go`
- [x] **T5 (P0)** — model card internal key 供 harness — internal 端点或 turn payload
- [x] **T6 (P1)** — Linear webhook + OAuth — `integrations.go` + `oauth.go` + `011_linear_gateway.sql` — Verify: `go test ./internal/api/...` + `scripts/smoke-linear-webhook.sh`（需重启 oma-server）
- [x] **T7 (P1)** — GitHub/Slack webhook 最小 E2E — integrations 扩展
- [x] **T8 (P1)** — Eval run background worker — `internal/eval/worker.go` — Verify: run pending→completed
- [ ] **T9 (P1)** — Memory blob + retention — store + cron
- [ ] **T10 (P1)** — Runtime WebSocket attach — `runtime_daemon.go`
- [ ] **T11 (P1)** — Session threads 从 event log 派生 — `session_aux.go`
- [ ] **T12 (P2)** — call_agent + compaction — harness
- [ ] **T13 (P2)** — resource mounter + outcome evaluator — harness + eval worker
- [ ] **T14 (P2)** — Dreams + cost_report API — 新 store + routes
- [ ] **T15 (P2)** — `/v1/internal/*` — 供未来 integrations 拆分

---

## 测试计划

### 已有覆盖

- Go API 单测/集成：`internal/api/*_test.go`
- Console wire：`scripts/console-integration.sh`
- Harness 合约：`harness/tests/test_oma_contract.py`, `test_turn.py`

### P0 完成后新增

| 测试 | 位置 | 优先级 |
|------|------|--------|
| web_fetch smoke | `harness/tests/test_web_fetch.py` | P0 |
| MCP tool smoke | `harness/tests/test_mcp.py` | P0 |
| models/list 集成 | `internal/api/models_test.go` | P0 |
| Agent tools E2E | `scripts/smoke-agent-tools.sh` | P0 |

### P1 完成后新增

| 测试 | 位置 | 优先级 |
|------|------|--------|
| Linear webhook E2E | `scripts/smoke-linear-webhook.sh` | P1 |
| Eval worker 状态机 | `internal/eval/worker_test.go` | P1 |
| Runtime attach | `internal/api/runtime_attach_test.go` | P1 |

### 仍缺的 harness / 集成路径

```
[+] POST /v1/sessions/:id/events
  ├── [★★★] uname -a smoke (manual / fake harness)
  ├── [GAP] web_fetch / MCP tool loop
  ├── [GAP] vault-injected HTTP
  └── [GAP] webhook → user.message

[+] Eval runs
  ├── [★★] CRUD (console-integration)
  └── [GAP] worker pending→completed

[+] Integrations
  ├── [★★] publication + dispatch CRUD
  └── [GAP] OAuth callback + webhook dispatch
```

---

## Failure modes（待处理）

| 路径 | 失败模式 | 测试 | 处理 | 用户可见 |
|------|----------|------|------|----------|
| resolveModel | API key 缺失 | 部分 | 需 turn 级 error envelope | 需明确 message |
| harness loop | LLM rate limit | GAP | retry/backoff | SSE error event |
| bash tool | 命令 hang | 部分 | `HARNESS_HTTP_TIMEOUT_SEC` | partial output |
| integration webhook | 签名失败 | GAP | 401 + 日志 | 静默丢事件 |
| eval worker | 进程 crash | GAP | 标记 failed + 重试 | Console 显示 failed |

---

## 并行化策略

| Lane | 内容 | 依赖 |
|------|------|------|
| A | P0 harness 工具（web_fetch, MCP） | — |
| B | P0 平台（outbound, models/list, model key） | — |
| C | P1 integrations + oauth | A 可选 |
| D | P1 eval worker + runtime attach | A |
| E | P2 call_agent / compaction / dreams | A, D |

Lane A/B 可并行；C/D 在 P0 核心工具就绪后启动。

---

## GSTACK REVIEW REPORT

| Review | Trigger | Runs | Status | Findings |
|--------|---------|------|--------|----------|
| Eng Review | `/plan-eng-review` | 2 | plan updated | P0+P1+P2 矩阵；~22✅ ~12🟡 ~15❌ |
| CEO Review | — | 0 | — | — |
| Design Review | — | 0 | — | — |

- **SCOPE:** P0 + P1 + P2；不含 CF SessionDO
- **VERDICT:** 文档与代码对齐完成 — 按 T1–T15 实施

---

## 变更历史

| 日期 | 变更 |
|------|------|
| 2026-06-07 | 初版：TypeScript main-node MVP 计划 |
| 2026-06-11 | 重写：Go+Python 现状、P0/P1/P2 对齐矩阵、Implementation Tasks T1–T15 |
