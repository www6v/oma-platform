# open-managed-agents → oma-platform 迁移计划

> Engineering review — 2026-07-07（矩阵 + 代码实况三次审查）  
> 目标仓库：`oma-platform`（Go 平台 + Python piPy harness 侧车 + Python SDK）  
> 参考源：`../open-managed-agents`（Cloudflare Workers meta-harness）  
> 已确认范围：**P0 + P1 + P2 主体已完成**；T1–T21（除 T16 browser defer）✅；**Python SDK `oma-sdk` v0.1.0** ✅（`sdk/`）；Managed Agents Cookbook example1–9 + Go CI 探针 ✅；剩余 **cap-cli OAuth**、**TS SDK / `oma` CLI 发布**、install **vault 双写** 与生产硬化

## 文档说明

本文档记录 `open-managed-agents` 与 `oma-platform` 的**功能对齐矩阵**与分阶段迁移 backlog。

早期版本（2026-06-07）假设 TypeScript `main-node` 复制路径；当前实现已改为 **Go `oma-server` + Python `harness/` 侧车 + `sdk/oma_sdk`**，矩阵以实际代码为准。验收脚本：`scripts/e2e/console-integration.sh`、`scripts/e2e/smoke-all.sh`；Console QA 最新：`scripts/e2e/.gstack/qa-reports/qa-report-console-2026-06-20.json`（healthScore 100，15/15 路由）。Cookbook parity 路线图：`docs/sdk-migrate/managed-agents-cookbook-roadmap.md`；设计文档索引：`docs/design/`。

---

## 对齐审查摘要（2026-07-07）

相对 `open-managed-agents` 的功能域统计（P0+P1+P2 + 2026-07 新增域，不含 CF 专有 defer 项）：

| 统计口径 | 数值 |
|---------|------|
| ✅ 已对齐 | **44 / 52** |
| 🟡 部分对齐 | **7 / 52** |
| ❌ 未迁移 | **1 / 52**（`/v1/cap-cli/oauth`） |
| ⏭ 明确 defer | **1**（`browser_*` 工具；另 `rl/` 独立产品线不计入矩阵） |
| **严格完成率（仅 ✅）** | **~85%** |
| **含部分项按 50% 计** | **~91%** |
| **核心自托管日常可用 parity** | **~92%**（排除 browser、cap-cli、TS SDK/CLI 发布、CF 基础设施） |

**P0 核心 Agent 闭环 ~93%** · **P1 Console + 集成 ~88%** · **P2 平台 parity ~88%**（不含 defer）

**VERDICT：** 核心迁移与 **Managed Agents Cookbook 主线（example1–9）** 已完成；日常 Agent 对话、HITL custom tool、Sub-agent / Agent Team、Console 管理路径可用。Integration install proxy **Phase 1–2** 已落地。下一 ROI：**vault 双写**（安装 token 持久化）、**`/v1/cap-cli/oauth`**（Console Vault CLI tab 依赖）、**TS SDK / `oma` CLI 发布**。

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

Console 全量 wire 验收：`scripts/e2e/console-integration.sh`

---

## 架构对齐

### 当前拓扑（oma-platform）

```
┌─────────────────────────────────────────────────────────┐
│  oma-server (Go chi)                         :8787      │
│    /v1/agents | sessions | vaults | skills | …         │
│    session.Registry + stream.Hub (SSE)                   │
│    teams · session resources · custom tool HITL          │
│    mcp-proxy · outbound-proxy · integrations             │
│    eval-worker · dream-worker · internal API             │
│    CONSOLE_DIR → Console SPA 同源                        │
├─────────────────────────────────────────────────────────┤
│  harness (Python piPy sidecar)               :8090      │
│    POST /internal/turn — 无状态 LLM 回合                  │
│    web_fetch · web_search · MCP · call_agent             │
│    custom tools · team_* · compaction                    │
├─────────────────────────────────────────────────────────┤
│  sdk/oma_sdk (Python SDK) — 可选客户端                    │
│    anthropic base_url + httpx OMA-only 资源               │
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

### P0 — 核心 Agent 闭环（~93%）

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| Agent CRUD + 版本 | `packages/agents-store`, `buildAgentRoutes` | `internal/api/agents.go`, `store/agents.go` | ✅ | AMA wire 已测 |
| Session + event log | SessionDO, `event-log` | `sessions.go`, `store/events.go` | ✅ | |
| SSE 流 | SessionDO broadcast | `internal/stream/hub.go` | ✅ | |
| user.interrupt | SessionDO | `internal/api/pending.go`, `internal/session/machine.go` | ✅ | 2026-07：支持 `session_thread_id` 范围 cancel；interrupt 强制 `end_turn` 并清空 HITL pending |
| Crash recovery | `runtime/recovery.ts` | `SessionRepo.RecoverRunning()` | ✅ | 启动时重置 orphan `running` |
| Harness turn | `harness/default-loop.ts` | `harness/oma_adapter/turn.py` | ✅ | HTTP 侧车，无状态 |
| agent_toolset 基础工具 | bash/read/write/edit/glob/grep | `harness/oma_adapter/tools.py` | ✅ | glob 映射为 piPy `find` |
| Custom tools + HITL | `agent.custom_tool_use`, `requires_action` | `custom_tools.py`, `pending_custom_tools.go`, `custom_tool_promote.go` | ✅ | GT1–GT5；Console HITL 面板（`HitlActionPanel.tsx`）；见 `docs/design/loop-task-termination.md` |
| Session resources CRUD | `sessions.resources.*` | `session_resources_handlers.go`, migration `019` | ✅ | 创建时 + mid-session add/delete；explore / orchestrate cookbook 已测 |
| web_fetch | `harness/tools.ts` | `web_fetch/`, `extensions/web_fetch.py` | ✅ | T1 已完成；`test_web_fetch.py` |
| web_search | `harness/tools.ts` DEFAULT_TOOLS | `extensions/web_search.py` + DDG/Tavily | ✅ | T17 完成 |
| schedule / cancel_schedule / list_schedules | `harness/tools.ts` | harness + SQLite worker | ✅ | T18；`OMA_DEFAULT_TOOLS` 已与源仓对齐 |
| MCP 工具 | `mcp-spawner.ts`, `/v1/mcp-proxy` | `mcp_loader` + `mcp_proxy.go` + `mcpproxy/` | ✅ | T2 已完成；`test_mcp.py` |
| Vault 凭据注入 | outbound proxy | `internal/outbound/` + harness 注入 | ✅ | T3 已完成；MCP + HTTP 双路径 |
| Model 解析 | model card + provider | `internal/modelresolve/` | ✅ | |
| POST /v1/models/list | `routes/models.ts` | `internal/api/models_list.go` | ✅ | T4 已完成；真实 provider 拉取 |
| GET /v1/models/list | 静态 catalog 探测 | `handleModelsListCatalogStub` | 🟡 | 探测用 stub；Console 模型探测与源仓略不一致 |
| Environment | `environments-store` | `environments.go` | ✅ | 无 per-env 容器镜像 |
| 沙箱隔离 | CF Container | `internal/workdir/` | 🟡 | **生产安全短板**：目录级 `SANDBOX_WORKDIR/<session_id>/`，非容器；多租户 blast radius 大于源仓 |
| Model card internal key | `/v1/internal/.../key` | `internal.go` + turn payload | ✅ | T5 |

### P1 — Console 完整可用 + 集成执行（~88%）

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| Console 同源 SPA | main `assets` binding | `internal/console/` + `CONSOLE_DIR` | ✅ | 挂载 `console/dist` |
| Auth (API key + cookie) | better-auth | `internal/auth/` | ✅ | `AUTH_UPSTREAM_URL` 或 `AUTH_DISABLED=1` |
| /v1/me, api_keys, tenants | main routes | `me.go`（tenants 在 `/v1/me/tenants`） | ✅ | |
| /v1/stats | `routes/stats.ts` | `stats.go` | ✅ | |
| Skills CRUD + zip upload | `routes/skills.ts` | `skills.go`, `skillzip/`, `store/skills.go` | ✅ | 2026-06-16：`DELETE /v1/skills/:id/versions/:version` 已实现；`TestSkillsVersionDeleteLifecycle` |
| Files 上传/下载 | R2 + `files-store` | `files.go`, `fileblob/` | ✅ | 本地 blob，非 R2 |
| Model Cards CRUD | `model-cards-store` | `model_cards.go` | ✅ | |
| Vaults + credentials | `vaults-store` | `vaults.go`, `vaultoauth/` | ✅ | OAuth refresh 已有 |
| Session aux | threads/pending/trajectory/outputs | `session_threads.go`, `trajectory.go` | ✅ | T11；`subagent_e2e_test.go` |
| Integrations webhook（Linear/GH/Slack） | `apps/integrations` | gateway + `linear/`, `github/`, `slack/` | ✅ | T6–T7；webhook 冒烟可过 |
| Integrations install proxy | 独立 Worker install proxy | `installbridge/` + `install_gateway.go` | ✅ Phase 1–2 | **Phase 1**（2026-06-16）：`start-a1` / `credentials` / `handoff-link` / `form-token`。**Phase 2**（2026-06-16）：`GET /github|slack/oauth/pub/{pubId}/callback`、`/github/manifest/start|callback`、`/github-setup|slack-setup/{token}`、`POST /github|slack/publications/credentials`；auth 豁免；`install_gateway_test.go` |
| Integrations user-scoped auth | service binding + user ctx | `integrations.go` `userID(r)` 校验 | 🟡 | 旧版无 `user_id` 的 API key 无法调用 `/v1/integrations/*` |
| Eval runs + worker | cron `tickEvalRuns` | `eval_runs.go`, `internal/eval/worker.go` | ✅ | T8 |
| Runtimes + ACP daemon | RuntimeRoom DO | `runtimes.go`, `runtime_daemon.go` | ✅ | T10 connect/exchange/attach |
| Memory stores + retention | R2 + FUSE + queue | `memory_stores.go`, retention cron | ✅ | T9 |
| OAuth (/v1/oauth/*) 通用 | `routes/oauth.ts` | `oauth_v1.go`, `oauthflow/` | ✅ | T21；`/v1/cap-cli/oauth` 仍 defer |
| /v1/cap-cli/oauth | `routes/cap-cli-oauth.ts` | — | ❌ | Console Vault CLI tab 已调用 `/v1/cap-cli/oauth/initiate|poll`；Go 侧路由未实现 |
| Internal API (/v1/internal/*) | `routes/internal.ts` | `internal.go` + sessions/vaults | ✅ | T15 |
| /v1/integrations 聚合路由 | `routes/integrations.ts` | 分散 gateway 路由 | 🟡 | 能力有，路径形态不完全一致 |

### P2 — 平台 parity

| 功能域 | 源参考 | oma-platform 实现 | 状态 | 缺口 / 备注 |
|--------|--------|-------------------|------|-------------|
| call_agent / 子 Agent | `harness/tools.ts` | harness `call_agent/` | ✅ | T12；见 `docs/design/subagent.md` |
| Agent Team | `team_create`, mailbox | `teams`/`team_members`/`agent_messages` + harness tools | ✅ | migrations `017`–`018`；`/v1/sessions/:id/teams/*`；见 `docs/design/agent-team.md` |
| Compaction 上下文压缩 | `harness/compaction.ts` | `compaction.py` | ✅ | T12 |
| Resource mounter | `runtime/resource-mounter.ts` | `resource_mounter.py` | ✅ | T13；`smoke-resource-live-e2e.sh` |
| Outcome evaluator | `harness/outcome-evaluator.ts` | `outcome_evaluator.py` + `outcome_supervisor.go` | ✅ | T13 + OG1–OG3；example4 cookbook |
| Dreams | `/v1/dreams`, `dreams-store` | `dreams.go`, `dream/worker.go` | ✅ | T14 |
| Cost report | `/v1/cost_report`, `cf-billing` | `cost_report.go`, `internal/usage/` | ✅ | T14 |
| browser tools | `harness/browser-tools.ts` OPT_IN | — | ⏭ | **T16 明确 defer** |
| clawhub | `routes/clawhub.ts` | `clawhub.go` | ✅ | T21 |
| /v1/oma/* 路由别名 | main index | `oma_aliases.go` + `router.go` | ✅ | T19 + T21 oauth/clawhub |
| Rate limiting | CF RL namespaces | `internal/ratelimit/` | ✅ | T20 Go middleware |
| Multi-tenant D1 分片 | `tenant-db` | 单 SQLite `tenant_id` | 🟡 | 够用至多 replica |
| Python SDK (`oma-sdk`) | `packages/sdk` (Python subset) | `sdk/oma_sdk/` v0.1.0 | 🟡 | T22a ✅：anthropic `base_url` + httpx OMA-only 资源；example1–9 + pytest E2E；**未 PyPI 发布** |
| TS SDK / `oma` CLI | `packages/sdk`, `packages/cli` | — | 🟡 | T22b defer：外部自动化可用 Python SDK 或 curl |
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
| ✅ 已对齐 | **44 / 52** 域 | Agent 闭环、HITL custom tool、Session resources、Agent Team、工具集、Console、Skills、eval、runtime、memory、P2 高级能力、Cookbook example1–9 |
| 🟡 部分 | **7 / 52** 域 | GET models/list stub、目录沙箱、Integration legacy API key / 路由形态、Python SDK（未发布）、TS SDK/CLI |
| ❌ 待迁 | **1 / 52** 域 | `/v1/cap-cli/oauth` |
| ⏭ defer | **1** 域 + CF 专有 + `rl/` | `browser_*`（T16）；SessionDO、Container、完整计费栈等 |

**VERDICT：** 自托管栈 **~91% parity**；T1–T21（除 T16）+ **T22a Python SDK** + **Cookbook 主线** 已完成。最高 ROI：**cap-cli OAuth**、**vault 双写**；发布 backlog：**T22b TS SDK / `oma` CLI**。

---

## 严重短板与断链（2026-07-07 审查）

按「用户 / Console 能否走通」排序：

| # | 短板 | 影响 | 优先级 |
|---|------|------|--------|
| 1 | **`/v1/cap-cli/oauth` 未实现** | Console Vault「CLI」tab 调用 initiate/poll 会 404；无法 Device Auth Grant 写入 `cap_cli` credential | **P1'** |
| 2 | **Integration install → vault 双写** | Console wizard 可完成 GitHub manifest + Slack OAuth 安装；installation token 尚未自动写入 vault | **P1' backlog** |
| 3 | **目录级沙箱 vs CF Container** | 多租户 / 不可信 Agent 场景 blast radius 大于源仓 | 架构已知差异 |
| 4 | **Legacy API key 无 user_id** | `/v1/integrations/*` 403；Console 集成页纯 API key 模式可能失败 | **P2'** |
| 5 | ~~Skills 版本 DELETE 501~~ | ~~Console 删 skill 版本失败~~ | ✅ 2026-06-16 已修复 |
| 6 | **生产硬化 GAP** | LLM rate limit 无 retry、model 错误 envelope 模糊、webhook 签名失败静默丢事件 | **P1'** |
| 7 | **TS SDK / `oma` CLI 发布** | Python SDK 已可用；TypeScript 包与独立 CLI 仍缺 | T22b defer |
| 8 | **`beta.webhooks` / session.status_idled** | operate / slack Part B cookbook 刻意 out of scope | P2 defer |

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
| P2-7 | web_search | `harness/tools.ts` | harness tool + provider | ✅ T17 |
| P2-8 | /v1/internal/* | `internal.ts` | `internal.go` | ✅ T15 |
| P2-9 | browser tools | `browser-tools.ts` | — | ⏭ **T16 defer** |

### Phase P3 — 剩余外围（当前 backlog）

| ID | 任务 | 源参考 | oma 落点 | 决策 |
|----|------|--------|----------|------|
| P0' Phase 1 | Integration install proxy (in-process) | `node-install-bridge.ts` | `installbridge/` + migration `016` | ✅ 2026-06-16 |
| P0' Phase 2 | OAuth callbacks + manifest + handoff pages | provider oauth routes | `install_gateway.go` | ✅ 2026-06-16 |
| P1' | `/v1/cap-cli/oauth` Device Auth Grant | `routes/cap-cli-oauth.ts` | 新路由 + vault credential 写入 | **下一 ROI** |
| P1' | Install → vault 双写 | publication credentials | `installbridge/` + vault repo | **下一 ROI** |
| P1' | LLM rate limit retry + model 错误 envelope | harness + turn | harness / API error path | 生产稳定性 |
| T16 | browser_* 工具 | `browser-tools.ts` | Playwright sidecar | **⏭ 明确 defer** |
| T17 | web_search | `harness/tools.ts` | harness + provider 选型 | ✅ 完成 |
| T18 | schedule 三件套 | `harness/tools.ts` | harness cron/queue | ✅ 完成 |
| T19 | `/v1/oma/*` 别名 | main index | `oma_aliases.go` | ✅ |
| T20 | Rate limiting | CF RL | Go middleware | ✅ |
| T21 | 通用 oauth + clawhub | `oauth.ts`, `clawhub.ts` | `oauth_v1.go`, `clawhub.go`, `oauthflow/` | ✅ |
| T22a | Python SDK (`oma-sdk`) | `packages/sdk` Python subset | `sdk/oma_sdk/` + example1–9 | ✅ 2026-07 |
| T22b | TS SDK / `oma` CLI 发布 | `packages/sdk`, `packages/cli` | 独立发布 | Phase 3 defer |
| CK1 | Cookbook parity 主线 | managed_agents/*.ipynb | example1–9 + Go `Test*Cookbook*` | ✅ 2026-07 |
| CK2 | prompt_versioning / operate / slack bot | 剩余 cookbook | workflow 探针 | P2 defer（webhooks 前置） |

---

## 目标数据流（当前实况）

```
Client / Console / Python SDK (oma-sdk)
       │
       ▼
┌──────────────────────────────────────┐
│  oma-server (Go)                      │
│  agents · sessions · vaults           │
│  integrations · eval-worker · dreams  │
│  teams · session resources            │
│  mcp-proxy · outbound-proxy           │
│  stream.Hub → SSE                     │
└──────────────┬───────────────────────┘
               │ POST /internal/turn
               ▼
┌──────────────────────────────────────┐
│  harness (Python piPy)                │
│  bash/file + web_fetch + web_search   │
│  MCP + call_agent + custom tools      │
│  team_create / spawn_teammate         │
│  compaction + outcome supervisor      │
└──────────────┬───────────────────────┘
               │ tools in workdir
               ▼
       SANDBOX_WORKDIR/<session_id>/
               │
               ▼
         SQLite session_events
               │
               ▼
    requires_action → user.custom_tool_result (HITL)
```

---

## What already exists（oma-platform，勿重写）

| 能力 | 位置 |
|------|------|
| AMA Agent/Session wire | `internal/api/agentwire.go`, `sessionwire.go`, `*_ama_test.go` |
| Console 契约集成测试 | `scripts/e2e/console-integration.sh`, `p1_console_test.go`, QA 100/100 (2026-06-20) |
| Skills CRUD + zip + version DELETE | `skills.go`, `skillzip/`, `store/skills.go` — `TestSkillsVersionDeleteLifecycle` |
| Custom tools + HITL | `harness/oma_adapter/custom_tools.py`, `internal/harness/pending_custom_tools.go`, Console `hitl.ts` |
| Session resources CRUD | `session_resources_handlers.go`, migration `019_session_resources.sql` |
| Agent Team | `internal/store/teams.go`, `session_aux.go` teams routes, migrations `017`–`018` |
| web_fetch + MCP + outbound | `harness/oma_adapter/web_fetch/`, `mcp/`, `internal/outbound/` |
| models/list POST | `internal/api/models_list.go`, `internal/modelslist/` |
| DB migrations (001–020) | `internal/store/migrations/` |
| Linear/GitHub/Slack gateway + install proxy | `internal/integrations/*`, `installbridge/`, `install_gateway.go` |
| Eval + dream workers | `internal/eval/worker.go`, `internal/dream/worker.go` |
| Session threads + subagent E2E | `session_threads.go`, `subagent_e2e_test.go` |
| Resource mounter + outcome eval/supervisor | `resource_mounter.py`, `outcome_evaluator.py`, `outcome_supervisor.go` |
| Internal API | `internal.go`, `smoke-internal-api-e2e.sh` |
| Python SDK + Cookbook examples | `sdk/oma_sdk/`, `sdk/example/example1`–`example9`, `sdk/SDK-PLAN.md` |
| Cookbook Go CI probes | `.github/workflows/ci.yml` — `Test*Cookbook*`, `TestSreCookbook`, `TestSkillHarness` |
| Fake harness CI | `OMA_FAKE_HARNESS=1`, `internal/harness/fake.go` |
| Docker Compose | `deploy/docker-compose.yml`（platform + harness + auth） |
| 设计文档 | `docs/design/`（subagent, agent-team, loop-task-termination, oauth, mcp, …） |

---

## NOT in scope（明确不做 / defer）

- CF SessionDO / Durable Objects 重写  
- 每 Environment 独立 Container Worker  
- `rl/` 强化学习训练  
- 完整 cf-billing + Analytics Engine  
- **`beta.webhooks` / `session.status_idled`** — SDK-PLAN 与 operate/slack Part B cookbook 刻意 out of scope  
- **browser 工具（T16 明确 defer）** — 源仓亦为 opt-in；`web_fetch` 覆盖只读场景  
- **TypeScript `@openma/sdk` / 独立 `oma` CLI 发布（T22b）** — Python SDK 已覆盖主要 API  
- 整仓 TypeScript `main-node` 复制（已 supersede 为 Go 实现）

---

## Implementation Tasks

### 已完成（T1–T15）

- [x] **T1 (P0)** — harness `web_fetch` — `web_fetch/`, `extensions/web_fetch.py` — Verify: `harness/tests/test_web_fetch.py`
- [x] **T2 (P0)** — MCP 挂载 + `/v1/mcp-proxy` — `mcp/` + `internal/api/mcp_proxy.go` — Verify: `harness/tests/test_mcp.py`
- [x] **T3 (P0)** — Vault outbound HTTP 代理 — `internal/outbound/` — Verify: `internal/outbound/*_test.go`
- [x] **T4 (P0)** — 真实 `POST /v1/models/list` — `internal/api/models_list.go`
- [x] **T5 (P0)** — model card internal key — `internal.go` + turn payload
- [x] **T6 (P1)** — Linear webhook + OAuth — Verify: `scripts/e2e/smoke-linear-webhook.sh`
- [x] **T7 (P1)** — GitHub/Slack webhook 最小 E2E
- [x] **T8 (P1)** — Eval run background worker — `internal/eval/worker.go`
- [x] **T9 (P1)** — Memory blob + retention
- [x] **T10 (P1)** — Runtime WebSocket attach — `runtime_daemon.go`
- [x] **T11 (P1)** — Session threads 从 event log 派生 — `session_threads.go`
- [x] **T12 (P2)** — call_agent + compaction — harness
- [x] **T13 (P2)** — resource mounter + outcome evaluator — Verify: `./scripts/e2e/smoke-resource-outcome-e2e.sh`
- [x] **T14 (P2)** — Dreams + cost_report — Verify: `./scripts/e2e/smoke-dreams-e2e.sh`
- [x] **T15 (P2)** — `/v1/internal/*` — Verify: `./scripts/e2e/smoke-internal-api-e2e.sh`

### 当前 backlog（T16–T22 + P1'）

- [⏭] **T16 (P2)** — `browser_*` 工具 — Playwright sidecar — **明确 defer**（源仓 opt-in；`web_fetch` 已覆盖只读）
- [x] **T17 (P2)** — `web_search` — Verify: `harness/tests/test_web_search.py`, `scripts/e2e/smoke-web-search-e2e.sh`
- [x] **T18 (P2)** — `schedule` / `cancel_schedule` / `list_schedules` — Verify: `harness/tests/test_schedule.py`, `internal/store/wakeups_test.go`, `scripts/e2e/smoke-schedule-e2e.sh`
- [x] **T19 (P3)** — `/v1/oma/*` 路由别名 — `oma_aliases.go` + `oma_aliases_test.go`
- [x] **T20 (P3)** — Rate limiting middleware — `internal/ratelimit/` — Verify: `go test ./internal/ratelimit/...`
- [x] **T21 (P3)** — 通用 `/v1/oauth` + clawhub — Verify: `go test ./internal/oauthflow/... ./internal/api/ -run 'OAuth|Clawhub'`
- [x] **T22a (P3)** — Python SDK `oma-sdk` v0.1.0 — `sdk/oma_sdk/` + example1–9 — Verify: `sdk/tests/test.sh`, CI `Test*Cookbook*`
- [ ] **T22b (P3)** — TypeScript SDK / `oma` CLI 独立发布
- [x] **CK1** — Managed Agents Cookbook 主线（example1–9）— Verify: `.github/workflows/ci.yml` cookbook test job
- [ ] **P1'** — `/v1/cap-cli/oauth` — Console Vault CLI tab 依赖
- [ ] **P1'** — Integration install → vault 双写
- [ ] **P1'** — LLM rate limit retry + model error envelope

---

## 测试计划

### 已有覆盖

- Go API 单测/集成：`internal/api/*_test.go`
- Console wire + QA 100/100：`scripts/e2e/console-integration.sh`（最新 2026-06-20）
- Harness：`test_oma_contract.py`, `test_turn.py`, `test_web_fetch.py`, `test_mcp.py`, `test_schedule.py`, `test_web_search.py`
- Smoke：`scripts/e2e/smoke-all.sh`；T13–T15、`smoke-resource-live-e2e.sh`、`subagent_e2e_test.go`
- **Cookbook CI**（`.github/workflows/ci.yml`）：`TestDataAnalystCookbook`, `TestIterateCookbookMultiTurn`, `TestGateCookbook*`, `TestOutcomeGraderCookbook`, `TestCoordinateCookbook*`, `TestRememberCookbook*`, `TestExploreCookbook*`, `TestOrchestrateCookbook*`, `TestSkillHarness`, `TestSreCookbook`
- **Python SDK E2E**：`sdk/tests/test_*.py`（agents, sessions, vaults, skills, subagents, cookbook helpers）
- **Session interrupt**：`internal/session/machine_interrupt_test.go` — scoped cancel + HITL pending 清理

### Harness 路径覆盖（2026-07）

```
[+] POST /v1/sessions/:id/events
  ├── [★★★] uname -a / fake harness
  ├── [★★★] web_fetch + MCP tool loop
  ├── web_search ✅（T17）
  ├── schedule 三件套 ✅（T18）
  ├── custom tool HITL ✅（GT1–GT5）
  ├── call_agent / subagent ✅（T12）
  ├── agent team mailbox ✅（017–018）
  └── outcome supervisor loop ✅（OG1–OG3）

[+] browser_*（T16 defer — 不纳入当前 sprint）
[+] cap-cli oauth（P1' — Console Vault CLI tab 404）
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
| cap-cli oauth | 路由缺失 | GAP | 实现 initiate/poll | Console Vault CLI 404 |
| user.interrupt | HITL pending 残留 | ✅ | 强制 `end_turn` + 清空 pending | Console Stop 按钮（2026-07） |

---

## 并行化策略（剩余工作）

| Lane | 内容 | 依赖 |
|------|------|------|
| A | **P1' cap-cli OAuth** — unblock Console Vault CLI tab | 独立 |
| B | **P1' install → vault 双写** | install proxy Phase 2 ✅ |
| C | **P1' 生产硬化** — LLM retry、error envelope、webhook 日志 | 独立 |
| D | **T22b TS SDK / CLI 发布** | Python SDK ✅ |
| E | **CK2 剩余 cookbook** — prompt_versioning、operate（需 webhooks 决策） | webhooks 产品决策 |

T16 browser 已 defer；Cookbook 主线 CK1 已关闭。

---

## GSTACK REVIEW REPORT

| Review | Trigger | Runs | Status | Findings |
|--------|---------|------|--------|----------|
| Eng Review | `/plan-eng-review` | 5 | **CLEARED** | 2026-07-07：44✅ 7🟡 1❌ 1⏭；~91% parity |
| Console QA | `/qa` | 3 | 100/100 | 最新 2026-06-20（15/15 路由） |
| Cookbook CI | CI workflow | 1 | ✅ | example1–9 + SRE + skill harness |
| CEO Review | — | 0 | — | — |
| Design Review | — | 0 | — | — |

- **SCOPE:** P0–P2 主体 + T22a Python SDK + Cookbook CK1 **✅**；Install proxy Phase 1–2 **✅**；T16 browser **defer**
- **VERDICT:** ~91% parity；下一 ROI：**cap-cli OAuth** + **vault 双写**；发布 backlog：**T22b TS SDK/CLI**

---

## 变更历史

| 日期 | 变更 |
|------|------|
| 2026-06-07 | 初版：TypeScript main-node MVP 计划 |
| 2026-06-11 | 重写：Go+Python 现状、P0/P1/P2 对齐矩阵、Implementation Tasks T1–T15 |
| 2026-06-13 | T17 web_search：DDG 默认 + Tavily 可选；`test_web_search.py` 17 tests pass |
| 2026-06-13 | 矩阵同步代码实况：web_fetch/MCP/outbound/threads 等标 ✅；T1–T4 完成；新增 T16–T22；T16 defer、T17 要做；Console QA 100/100 |
| 2026-06-16 | 二次工程对齐审查：39/46 ✅、~89% parity；P0/P1 完成率标注；Integration install proxy 503 标严重断链；Skills `DELETE .../versions/:version` 实现 + 测试 |
| 2026-06-16 | Integration Install Proxy Phase 2：`install_gateway.go` OAuth/manifest/handoff 路由；`continue.go` + GitHub manifest/install + Slack token exchange；auth 公开路径；`install_gateway_test.go` |
| 2026-06-16 | Integration Install Proxy Phase 1：`installbridge/` 进程内桥接；migration `016` GitHub 列；`start-a1`/`credentials`/`handoff-link`/`form-token` 不再 503；Linear legacy → 410 |
| 2026-07-07 | 三次工程对齐审查：44/52 ✅、~91% parity；新增矩阵域 custom tools/HITL、Session resources、Agent Team；T22 拆为 T22a Python SDK ✅ + T22b TS/CLI defer；Cookbook example1–9 + Go CI 探针 ✅；cap-cli OAuth 标为唯一 ❌ 域；更新 backlog（vault 双写、生产硬化）；DB migrations 至 020 |
