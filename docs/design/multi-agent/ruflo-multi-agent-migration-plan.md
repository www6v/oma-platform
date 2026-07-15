# Ruflo 多 Agent 方案 → oma-platform 迁移方案

> 前置阅读：[ruflo-multi-agent-architecture.md](./ruflo-multi-agent-architecture.md)
> 目标仓库：`oma-platform`（Go `oma-server` + Python `oma-harness`）
> 源仓库：`ruflo-main`（`@claude-flow/cli` 生态）

---

## 1. 迁移目标与原则

### 1.1 目标陈述

把 Ruflo 的 **多 Agent 协调能力** 落到 oma 的 **持久化、多租户、API 优先** 平台上，而不是把 `npx ruflo` 整包搬进 monorepo。

用户应能：

1. 在 Console / API 定义 **协调者 + 专员 Agent 编制**（等同 Ruflo 的 swarm roster）。
2. 在 Session turn 中 **并行或串行委派** 子 Agent，并可观测线程（已有 `session.thread_*`）。
3. （可选）使用 **Swarm 账本**：拓扑、任务队列、共享记忆 namespace（Ruflo MCP 等价物）。
4. （可选）**Hooks 式路由与学习**：任务文本 → 推荐 Agent、模式沉淀（oma 侧 Go/Python 实现）。
5. 保持 oma 强项：**SQLite 持久化、OAuth、Integrations、Eval、SSE**。

### 1.2 决策原则（对齐 autoplan 六原则）

| 原则 | 迁移含义 |
|------|----------|
| 完整性 | 先打通 E2E 委派 + 观测，再叠记忆/联邦 |
| Boil lakes | 动到的模块（session、harness、agents API）一并补测试与文档 |
| 务实 | 复用 `call_agent`，不重写 piPy turn 循环 |
| DRY | Agent 定义进 `agents` 表 + Skills，不复制 108 份 Markdown 到代码里 |
| 显式优于巧妙 | Swarm 状态进 DB 表，不用隐式 `.claude-flow/` 目录 |
| 行动偏向 | 分阶段可 ship，每阶段有 smoke/e2e |

### 1.3 非目标（首期不做）

- 完整复刻 313+ MCP 工具
- 照搬 `.claude-flow/swarm/` 文件队列（改为 DB + 事件）
- 嵌套 `@claude-flow/cli` npm 作为运行时依赖
- Federation / WireGuard 多节点（Phase 4 可选）
- 容器级沙箱（oma 仍为目录沙箱，另立项）

---

## 2. 现状对照（Gap 矩阵）

| Ruflo 能力 | oma 现状 | Gap | 迁移策略 |
|------------|----------|-----|----------|
| Agent 角色库（Markdown） | `agents` + `skills` API | 无统一「类型→prompt」目录 | Phase 1：Skills 包 + 模板 Agent |
| `callable_agents` 协调 | ✅ `call_agent_*` | 单层委派 | Phase 1 增强；Phase 2 嵌套 |
| `general_subagent` | ✅ | — | 保持 |
| Swarm 拓扑 / maxAgents | ❌ | 无账本 | Phase 2：`swarms` 表 + API |
| MCP `swarm_*` / `agent_*` | ❌ | 无 | Phase 2：Go MCP 或 REST 等价 |
| 文件 mailbox / A2A | ❌ | 无 | Phase 2：`agent_messages` 表 |
| Claude Agent Teams 并行 | 依赖 Cursor 宿主 | oma harness 内串行 sub-turn | Phase 2：可选并行 sub-turn |
| 任务路由（router.cjs） | ❌ | 无 | Phase 3：`TaskRouter` 服务 |
| 模式学习 / neural | 部分 `dream` worker | 无 hooks 环 | Phase 3：post-turn 模式写入 |
| AgentDB 向量记忆 | `memory_stores` | 模型不同 | Phase 3：namespace 对齐 |
| SPARC / Autopilot 插件 | ❌ | 无 | Phase 3：Skill + wakeup 组合 |
| Federation | ❌ | 无 | Phase 4 |
| 类型化流水线门控 | ❌ | 无 | Phase 3：可选 `pipeline` 扩展 |
| Web Chat + MCP Bridge | Console 部分 | UI  swarm 面板缺 | Phase 2 Console |
| ADR + smoke 契约 | 部分 Go 测试 | 无 swarm contract | Phase 2：`swarm_contract_test` |

---

## 3. 目标架构（oma 侧）

```mermaid
flowchart TB
  subgraph Client
    API[REST / SSE / Console]
  end
  subgraph Go["oma-server"]
    AgentsAPI[/v1/agents]
  SwarmAPI[/v1/swarms  Phase2]
    Reg[session.Registry]
    Mach[session.Machine]
    SwarmSvc[swarm.Service Phase2]
    Router[task.Router Phase3]
    EvalW[eval.Worker]
  end
  subgraph Py["oma-harness"]
    Turn[turn.py]
    CallAgent[call_agent/]
    SwarmTools[swarm_tools Phase2]
  end
  subgraph Store
    SQLite[(oma.db)]
    SkillsFS[data/skills]
    Sandbox[data/sandboxes]
  end
  API --> AgentsAPI
  API --> Reg
  Reg --> Mach
  Mach --> Turn
  Turn --> CallAgent
  Mach --> SwarmSvc
  SwarmSvc --> SQLite
  Turn --> SwarmTools
  Router --> Mach
```

**核心映射**：

| Ruflo | oma 落点 |
|-------|----------|
| Ledger（MCP） | Go `internal/swarm` +（可选）MCP proxy 工具 |
| Executor | 现有 `oma-harness` turn |
| Agent Markdown | `agents` 行 + `skills` 附件 |
| `.claude-flow/swarm/` | `swarms`, `swarm_agents`, `agent_messages` 表 |
| `memory namespace` | `memory_stores` + key 前缀 `coordination/` |
| Hooks route | `internal/swarm/router.go` 或 harness 前置 hook |
| Eval 多 Agent | 已有 `eval.Worker` + `thread_created` 断言 |

---

## 4. 分阶段迁移计划

### Phase 0 — 基线（1–2 天，human / CC: 同会话）

**交付**：文档 + 冒烟清单（本文 + architecture  doc）。

| 任务 | 文件/动作 |
|------|-----------|
| 冻结 Ruflo 对照版本 | 记录 `ruflo-main` commit / `package.json` 3.10.x |
| 确认 oma sub-agent E2E 绿 | `internal/api/subagent_e2e_test.go` |
| 导入 Ruflo 角色为 Skills 种子（可选） | `data/skills/ruflo-core-*.md` |

**验收**：`go test ./internal/api -run SubAgent -count=1` 通过。

---

### Phase 1 — 协调者编制与 Ruflo 角色对齐（3–5 天 human / ~2h CC）

**目标**：API 层表达 Ruflo 的「hierarchical + specialized roster」，不新增 Swarm 表。

#### 1.1 Agent 配置扩展

在 `AgentConfig` JSON 增加可选块（向后兼容）：

```json
{
  "callable_agents": [
    { "type": "agent", "id": "agt_coder", "version": 1, "role": "coder" },
    { "type": "agent", "id": "agt_tester", "version": 1, "role": "tester" }
  ],
  "multiagent": {
    "topology": "hierarchical",
    "max_agents": 8,
    "strategy": "specialized"
  },
  "metadata": {
    "enable_general_subagent": true,
    "ruflo_role_map": { "coder": "agt_coder", "tester": "agt_tester" }
  }
}
```

| 组件 | 变更 |
|------|------|
| `internal/api/agents.go` | 接受 `multiagent.topology/strategy/max_agents`（校验 + 存储） |
| `internal/api/agentwire.go` | AMA wire 往返 |
| `internal/harness/snapshot.go` | snapshot 携带 `multiagent` 元数据 |
| `docs/design/subagent.md` | 补充 Ruflo 对照章节 |

#### 1.2 Skills 导入管线

| 任务 | 说明 |
|------|------|
| `scripts/import-ruflo-agents.sh` | 从 ruflo `.claude/agents/core/*.md` 生成 Skill zip |
| `POST /v1/skills` | 上传 `ruflo-coder`, `ruflo-reviewer` 等 |
| Agent 绑定 | coordinator Agent `skills: ["ruflo-swarm-orchestration"]` |

#### 1.3 Harness 提示增强

| 文件 | 变更 |
|------|------|
| `harness/oma_adapter/turn.py` | 若 `multiagent.topology=hierarchical`，注入协调者 system -append |
| `extensions/call_agent.py` | tool description 带 role 别名 |

#### 1.4 测试

- 扩展 `subagent_e2e_test.go`：断言 `TurnRequest` 含 `multiagent` 元数据
- `harness/tests/test_call_agent.py`：role map 工具名

**验收**：

- 创建 coordinator + 2 worker Agent，用户任务触发 `session.thread_created`
- Console `/threads` 可见子 Agent 轨迹

**不在 Phase 1**：嵌套委派、Swarm CRUD API、并行 sub-turn。

---

### Phase 2 — Swarm 账本（Go Ledger）（5–8 天 human / ~1 天 CC）

**目标**：等价 Ruflo `swarm_init` + `agent_spawn` + `swarm_status` 的 **REST/MCP 持久化实现**。

#### 2.1 数据库（`migrations/017_swarms.sql`）

```sql
-- swarms: 一个 session 或 agent 绑定一个 active swarm
CREATE TABLE swarms (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL,
  session_id TEXT,           -- 可选：session 级 swarm
  coordinator_agent_id TEXT NOT NULL,
  topology TEXT NOT NULL,    -- hierarchical | mesh | hierarchical-mesh
  max_agents INTEGER NOT NULL DEFAULT 8,
  strategy TEXT NOT NULL DEFAULT 'specialized',
  status TEXT NOT NULL,      -- active | shutdown
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE swarm_agents (
  id TEXT PRIMARY KEY,
  swarm_id TEXT NOT NULL REFERENCES swarms(id),
  agent_id TEXT,             -- 具名 agent 或 null（逻辑角色）
  role TEXT NOT NULL,        -- coder | tester | coordinator ...
  status TEXT NOT NULL,
  spawned_at TEXT NOT NULL,
  metadata JSON
);

CREATE TABLE agent_messages (
  id TEXT PRIMARY KEY,
  swarm_id TEXT NOT NULL,
  from_agent_id TEXT,
  to_agent_id TEXT,          -- null = broadcast
  priority INTEGER DEFAULT 2,
  payload JSON NOT NULL,
  delivered_at TEXT,
  created_at TEXT NOT NULL
);
```

#### 2.2 Go 服务

| 包/文件 | 职责 |
|---------|------|
| `internal/swarm/service.go` | Init / Status / Shutdown / Spawn / List |
| `internal/store/swarms.go` | CRUD |
| `internal/api/swarms.go` | `POST /v1/swarms`, `GET /v1/swarms/{id}`, `POST .../agents` |
| `internal/session/machine.go` | turn 开始：若 agent 配 `multiagent`，自动 `swarm_init` 记录 |

#### 2.3 Harness 工具（可选 MCP 暴露）

| 工具名 | 映射 |
|--------|------|
| `swarm_init` | `POST /v1/internal/swarms`（internal secret） |
| `agent_spawn` | `POST /v1/swarms/{id}/agents` |
| `swarm_status` | `GET /v1/swarms/{id}` |
| `swarm_message_send` | `POST /v1/swarms/{id}/messages` |

实现位置：`harness/oma_adapter/extensions/swarm_tools.py` → HTTP 调 Go internal API。

#### 2.4 契约测试

参照 `ruflo-swarm/scripts/smoke.sh`：

- `internal/swarm/contract_test.go`：12 工具名 + topology 枚举 + namespace `swarm-state`

#### 2.5 Console（最小）

- Session 详情页：Swarm 成员列表 + 消息时间线（读 `agent_messages`）

**验收**：

- E2E：创建 swarm → spawn 3 agents → 一次 coordinator turn → DB 有记录 + thread 事件
- `swarm_shutdown` 后不可 spawn

---

### Phase 3 — 路由、记忆、流水线（5–10 天 human）

#### 3.1 Task Router（`router.cjs` 等价）

| 组件 | 说明 |
|------|------|
| `internal/swarm/router.go` | 正则/规则：implement→coder, test→tester |
| Hook 点 | `session.Machine` turn 前：写 `session_events` type `agent.route_recommendation` |
| Harness | 可选自动 tool hint 注入 system append |

#### 3.2 记忆 Namespace

对齐 Ruflo namespace：

| Namespace | oma 映射 |
|-----------|----------|
| `coordination` | `memory_stores` + prefix `coordination/` |
| `swarm-state` | `memory_stores` + `swarm_id` |
| `patterns` | turn 结束后 worker 写入（简化版 intelligence） |

`internal/dream/worker.go` 或新 `pattern.Worker`：扫描成功 turn → 提取 tool 序列 → 存 patterns。

#### 3.3 嵌套委派（可选，打破单层限制）

| 文件 | 变更 |
|------|------|
| `harness/oma_adapter/call_agent/delegate.py` | 移除或放宽 `_strip_delegation()` |
| `docs/design/subagent.md` | 文档：max depth = 2 |
| 测试 | 嵌套 thread 树 + eval 断言 |

#### 3.4 类型化流水线（可选）

为 Integrations 或自定义场景：

```json
{
  "pipeline": {
    "stages": [
      { "role": "analyst", "agent_id": "...", "output_schema": "RegimeVerdict" },
      { "role": "risk", "agent_id": "...", "blocking": true }
    ]
  }
}
```

Go `internal/pipeline/runner.go`：按 stage 顺序 `call_agent`，schema 校验失败即 halt。

参考：`ruflo-neural-trader/src/pipeline-messages.ts`。

#### 3.5 SPARC / Autopilot 轻量复刻

| Ruflo | oma |
|-------|-----|
| SPARC 五阶段 | 5 个 Agent + `callable_agents` + phase gate 存 `memory` |
| Autopilot `/loop` | 已有 `schedule` + `wakeup_worker` + eval loop |

不移植插件代码；用 **Agent 编制 + wakeup** 表达同等行为。

**验收**：

- Router 推荐与 LLM 实际 call_agent 一致率 > 基线（人工 spot check）
- patterns namespace 可 `memory search` 检索（若 Console 支持）

---

### Phase 4 — 联邦与高级（可选，10+ 天）

| 项 | 说明 |
|----|------|
| Federation | 多 oma-server 实例 WSS + Ed25519（读 Ruflo `docs/federation`） |
| 并行 sub-turn | harness asyncio 多 `sub_turn` + Go 并发 turn 锁 per thread |
| AgentDB / 向量 | 若需 HNSW，接现有 memory 或外置向量库 |
| 容器沙箱 | 与多 Agent 独立立项 |

---

## 5. 组件级映射表（实施时查阅）

| Ruflo 路径/概念 | oma 目标 |
|-----------------|----------|
| `.claude/agents/core/coder.md` | Skill + Agent `system` |
| `swarm_init` | `POST /v1/swarms` |
| `agent_spawn` | `POST /v1/swarms/{id}/agents` + harness spawn 记录 |
| `memory_store(namespace=coordination)` | memory API + key 前缀 |
| `router.cjs` | `internal/swarm/router.go` |
| `swarm-comms.sh` | `agent_messages` 表 |
| `Task` / `SendMessage` | `call_agent_*` + `session.thread_*` |
| `SubagentStop` hook | turn 结束 → pattern worker |
| `plugins/ruflo-sparc` | 5 Agent + gates in memory |
| `plugins/ruflo-autopilot` | wakeup schedules |
| `mcp-bridge` | 已有 `mcp-proxy` + Console |
| ADR smoke | `contract_test.go` |

---

## 6. API 草案（Phase 2）

```
POST   /v1/swarms
GET    /v1/swarms/{swarm_id}
POST   /v1/swarms/{swarm_id}/shutdown
GET    /v1/swarms/{swarm_id}/agents
POST   /v1/swarms/{swarm_id}/agents
GET    /v1/swarms/{swarm_id}/messages
POST   /v1/swarms/{swarm_id}/messages

# Internal（harness）
POST   /v1/internal/swarms
POST   /v1/internal/swarms/{id}/agents
```

鉴权：用户 API key（Console）；internal 用 `OMA_INTERNAL_SECRET`（与 wakeup 相同模式）。

---

## 7. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| 误以为 `swarm_init` 会执行 | 用户期望自动跑 swarm | 文档 + harness 提示：委派靠 `call_agent` |
| 单层委派限制 | 无法复刻深层 hive-mind | Phase 3 嵌套 + max depth |
| 目录沙箱 | 子 Agent 互删文件 | workdir 子目录 per thread（可选） |
| SQLite 并发 | 多 swarm 写锁 | swarm 行级短事务；消息批量插入 |
| 108 Agent 全导入 | 维护成本高 | 只导入 core 6 角色 + Skills |
| 依赖 npm `@claude-flow/cli` | 版本漂移 | **不依赖**；只复刻 12 工具契约 |
| Codex 双模 | oma 无 Codex 宿主 | 用多 model card / 多 Agent 代替 dual-mode |

---

## 8. 测试策略

| 层级 | 内容 |
|------|------|
| Go unit | `swarm/service_test.go`, router 规则 |
| Go API | `swarms_test.go`, 扩展 `subagent_e2e_test.go` |
| Harness | `test_swarm_tools.py`, `test_call_agent_nested.py` |
| Contract | `swarm_contract_test.go` ← ADR 0001 对照 |
| Shell | `scripts/e2e/swarm-smoke.sh` |
| Eval | eval run 断言 `thread_created` + swarm 表行数 |

沿用 `OMA_FAKE_HARNESS=1` 做 CI 快速路径；夜间 job 跑真实 harness。

---

## 9. 里程碑与时间线（估算）

| 里程碑 | 内容 | 累计 |
|--------|------|------|
| M0 | 两篇设计文档 + subagent 测试绿 | 第 1 周 |
| M1 | multiagent 配置 + Skills 种子 + E2E | 第 2 周 |
| M2 | Swarm 表 + API + harness tools + contract test | 第 3–4 周 |
| M3 | Router + patterns +（可选）嵌套委派 | 第 5–6 周 |
| M4 | Federation / 并行 / 向量（按需） | 第 7+ 周 |

人力假设：1 后端熟悉 Go session 机 + 1 熟悉 harness Python。

---

## 10. Autoplan 审查摘要（CEO / Eng / DX）

### CEO — 战略

- **前提成立**：oma 需要多 Agent 是为 **可观测委派 + 集成触发 + eval**，不是为营销「swarm」一词。
- **范围**：Phase 1–2 为 MVP；联邦为可选；不阻塞现有 OAuth/Integrations 主线。
- **推迟**：完整 108 Agent、313 MCP 工具、WireGuard。

### Eng — 架构

- **Ledger 在 Go**：session 已有队列与事件流，Swarm 是自然扩展，而非第二个 orchestrator 进程。
- **Harness 保持 Executor**：与 Ruflo 分工一致。
- **测试**：Section 3 级 test diagram 需覆盖 swarm spawn + call_agent + thread 事件链。

### DX — 开发者体验

- 对外暴露 **REST 优先**（`/v1/swarms`），MCP 工具为 harness 内部便利。
- Skills 导入脚本 + 示例 coordinator JSON 降低上手成本。
- 文档：`subagent.md` + 本文 + 一条 `scripts/e2e/swarm-smoke.sh`。

### 待你确认的「口味」决策（Taste）

1. **Swarm 绑定粒度**：Session 级 vs Agent 级 swarm 记录？（推荐 Session 级，便于 Console 展示）
2. **并行 sub-turn**：Phase 2 是否要做 asyncio 并行？（推荐 Phase 3，先串行稳定）
3. **Ruflo Skills 导入范围**：仅 core 6 角色 vs 全量 108？（推荐 core 6 + swarm-orchestration skill）

### User Challenge（无）

模型未建议改变你的核心方向（迁移到 oma-platform）；按上述分阶段执行即可。

---

## 11. 立即行动清单（本周可执行）

1. 跑通现有 sub-agent E2E：`go test ./internal/api -run SubAgent -v`
2. 从 ruflo 复制 6 个 core agent markdown → `scripts/import-ruflo-agents.sh` 草稿
3. 在 `agents.go` 增加 `multiagent` JSON 校验（只存不消费也可）
4. 起草 `017_swarms.sql`（可先不 apply，评审 schema）
5. 在 `MVP-MIGRATION-PLAN.md` 增加一行：「Ruflo 多 Agent → Phase 1–2」

---

## 12. 相关 oma 文档

- [subagent.md](./subagent.md)
- [session-threads.md](./session-threads.md)
- [streaming-turn-and-sse.md](./streaming-turn-and-sse.md)
- [eval-run-background-worker.md](./eval-run-background-worker.md)
- [memory.md](./memory.md)
- [MVP-MIGRATION-PLAN.md](../api-migrate/MVP-MIGRATION-PLAN.md)

---

**状态**：DRAFT — 待 Phase 1 开工前评审 schema 与 API 路径。
**恢复点**：Git 分支当前工作树；无 plan 文件 restore（用户未使用 Cursor plan mode）。
