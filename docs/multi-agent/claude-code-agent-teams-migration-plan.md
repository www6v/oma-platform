# Claude Code Agent Teams → oma-platform 迁移方案

> 前置阅读：[claude-code-agent-teams-architecture.md](./claude-code-agent-teams-architecture.md)
> 目标仓库：`oma-platform`（Go `oma-server` + Python `oma-harness`）
> 源仓库：`claude-code-rev/src`（Claude Code Agent Teams / Swarm）

**实施状态（2026-06-16）**

| 阶段 | 状态 | 说明 |
|------|------|------|
| Phase 1 | ✅ 已 ship | `pi_subagent` 扩展 + Console thread UI + eval + console E2E |
| Phase 2 | 🚧 进行中 | DB + Go API + `pi_team` + Console Team Tab 已落地；eval 待补 |
| Phase 3+ | ⏳ 未开始 | 任务看板、worktree 隔离 |

---

## 1. 迁移目标与原则

### 1.1 目标陈述

将 Claude Code 的 **Agent Teams 协调能力** 映射到 oma 的 **持久化、多租户、API 优先** 平台，而不是把 Claude Code CLI 源码搬进 monorepo。

用户应能：

1. 在 Console / API 定义 **Team 编制**（Leader Agent + 具名 Teammate Agent）。
2. 在 Session 中 **并行或串行** 运行多个 Agent turn，并通过 SSE 观测各 `session_thread_id`。
3. 使用 **Mailbox 式协调**（SendMessage 等价 API / 工具），而非仅一次性 tool return。
4. 使用 **Team 任务看板**（TaskCreate 等价），带 owner 与依赖。
5. 保持 oma 强项：**SQLite 持久化、OAuth、Integrations、Eval、租户隔离**。

### 1.2 决策原则

| 原则 | 迁移含义 |
|------|----------|
| 完整性 | 先 E2E「Team + 委派 + 观测」，再叠 mailbox / 任务图 |
| Boil lakes | 动 session、harness、agents API 时同步补测试与 Console |
| 务实 | 复用 `call_agent` / `run_sub_agent_turn`，不重写 piPy 主循环 |
| DRY | Agent 定义进 `agents` 表 + Skills，不复制 CC 的 `.claude/agents/` 文件树 |
| 显式优于巧妙 | Team / Message / Task 进 DB 表，不用 `~/.claude/teams/` 隐式目录 |
| 行动偏向 | 分阶段可 ship，每阶段有 smoke / eval |

### 1.3 非目标（首期不做）

- 复刻 tmux / iTerm2 pane 布局（oma 用进程/容器级隔离替代 UI pane）
- 1:1 移植 Claude Code 全量工具（Monitor、EnterWorktree 等可 Phase 3+）
- 嵌套 Claude Code 源码作为依赖
- Remote CCR / teleport 远程队友（另立项）
- 完整 GrowthBook feature gate 体系

---

## 2. 现状对照（Gap 矩阵）

| Claude Code 能力 | oma 现状 | Gap | 迁移策略 |
|------------------|----------|-----|----------|
| `Agent` sub-agent | ✅ `call_agent_*` / `general_subagent`（`pi_subagent`） | 无 `resume_thread_id` | Phase 1 ✅；resume 可 Phase 1.1 |
| Named teammate | ✅ `team_members.display_name` | Leader member 未自动注册 | Phase 2 🚧 |
| `TeamCreate` | ✅ `teams` 表 + `team_create` 工具 | — | Phase 2 🚧 |
| `SendMessage` | ✅ `agent_messages` + `send_team_message` | 广播 fan-out 未做 | Phase 2 🚧 |
| `TaskCreate` 看板 | ❌ | 无任务依赖 | Phase 3 `team_tasks` |
| 并行 teammate | ✅ 并行 sub-turn + 按 thread 排队 turn | 多 harness 进程 | Phase 4 |
| Worktree 隔离 | 共享 sandbox | 无 git worktree | Phase 3 sandbox 扩展 |
| Plan/shutdown 协议 | ✅ `shutdown_request` / `shutdown_response` + loop | 无 shutdown 状态机 | Phase 2.1 |
| Built-in agent 类型 | 🚧 `pi_subagent/roles.py` + Go roles | Skills seed 文件未加 | Phase 1.1 |
| Monitor 工具 | Console Team Tab ✅ | 无 Task 看板 | Phase 3 |
| In-process teammate | ✅ spawn + 长驻 poll loop | 多 harness 进程 | Phase 2.1 `pi_team/loop.py` |

参考现有文档：[subagent.md](./subagent.md)、[session-threads.md](./session-threads.md)。

---

## 3. 目标架构（oma 侧）

```mermaid
flowchart TB
  subgraph Client
    Console[Console Team 面板]
    API[REST / SSE]
  end
  subgraph Go["oma-server"]
    AgentsAPI["/v1/agents"]
    TeamsAPI["/v1/teams Phase2"]
    SessAPI["/v1/sessions"]
    TeamSvc[team.Service]
    MsgSvc[message.Service]
    Reg[session.Registry]
    Mach[session.Machine]
  end
  subgraph Py["oma-harness"]
    Turn[turn.py]
    PiSubagent[pi_subagent/]
    PiTeam[pi_team/]
    SubExt[extensions/subagent_extension.py]
    TeamExt[extensions/team_extension.py]
  end
  subgraph Store
    SQLite[(oma.db)]
    Sandbox[data/sandboxes]
  end
  Console --> TeamsAPI
  API --> SessAPI
  SessAPI --> Reg
  Reg --> Mach
  Mach --> Turn
  Turn --> SubExt
  Turn --> TeamExt
  SubExt --> PiSubagent
  TeamExt --> PiTeam
  TeamSvc --> SQLite
  MsgSvc --> SQLite
  Mach --> Sandbox
```

### 3.1 概念映射

| Claude Code | oma-platform |
|-------------|--------------|
| Team Lead session | Session 主线程 `sthr_primary` |
| Teammate session | Session Thread `sthr_*` + 可选长驻 worker |
| `TeamFile` | `teams` + `team_members` 表 |
| `inboxes/{name}.json` | `agent_messages` 表（按 team + recipient） |
| `Agent({ name })` | `spawn_teammate` 工具 + thread 注册 |
| `SendMessage` | `send_team_message` 工具 / POST message API |
| `TaskCreate` | `create_team_task` + `team_tasks` 表 |
| `subagent_type` | Agent `metadata.role` 或 Skill 绑定 |

---

## 4. 分阶段实施

### Phase 1 — Sub-agent 增强 ✅（已 ship，2026-06）

**目标**：对齐 Claude Code `Agent` 工具的「单次委派」体验，不引入 Team 实体。

#### 4.1.1 Harness（piPy 扩展，高内聚 / 低耦合）

| 项 | 状态 | 位置 |
|----|------|------|
| 并行 sub-turn | ✅ | `harness/pi_subagent/delegate.py` |
| 后台 sub-agent + SSE | ✅ | `pi_subagent/delegate.py` + Go events |
| `max_depth=3` | ✅ | `pi_subagent/runtime.py` |
| `resume_thread_id` | ⏳ | 未实现 |
| Agent 角色模板 | 🚧 | `pi_subagent/roles.py`（Skills seed 待补） |

目录布局（与 `piPy_memory` 同模式）：

```
harness/
├── pi_subagent/                 # 业务逻辑（host 无关）
│   ├── types.py
│   ├── runtime.py
│   ├── delegate.py
│   ├── roles.py
│   └── tools.py
├── extensions/subagent_extension.py   # 薄注册层
└── oma_adapter/subagent_bridge.py     # OMA turn 桥接
```

#### 4.1.2 Go API

| 项 | 状态 |
|----|------|
| `default_subagent_roles` snapshot | ✅ `internal/harness/subagent_roles.go` |
| `parallel_tool_calls` 文档化 | ⏳ |

#### 4.1.3 Console

| 项 | 状态 |
|----|------|
| Thread 树 + sub-agent 状态 | ✅ |
| Playwright console E2E | ✅ `scripts/e2e/smoke-subagent-console-e2e.sh` |

#### 4.1.4 验收

- ✅ `scripts/e2e/smoke-subagent-e2e.sh`
- ✅ `test/eval/suites/multi-agent.ts` — `T5.4-parallel-delegation`
- ✅ `harness/tests/test_call_agent.py` / `test_subagent_e2e.py`

**Phase 1 遗留（可选 1.1）**：`resume_thread_id`、Skills seed（explore/plan/verify）、`parallel_tool_calls` API 文档。

---

### Phase 2 — Team + Mailbox 🚧（进行中）

**目标**：复刻 Claude Code 的 Team 编制与 SendMessage 协调，DB 持久化。

#### 4.2.1 数据模型（SQLite）✅

Migration：`internal/store/migrations/017_teams.sql`  
Store：`internal/store/teams.go`（`TeamRepo`）

#### 4.2.2 Go 服务 ✅（合并于 `internal/api/teams.go`，非独立 package）

| 路由 | 用途 |
|------|------|
| `GET /v1/sessions/{id}/teams` | Console / 租户 API 列表 |
| `GET /v1/internal/sessions/{id}/teams` | Harness 列表 |
| `POST /v1/internal/sessions/{id}/teams` | `team_create` |
| `POST .../teams/spawn` | `spawn_teammate` → thread + `session.thread_created` |
| `POST .../teams/messages` | `send_team_message` → DB + SSE `team.message` + 可选 EnqueueEvents |
| `POST .../teams/messages/read` | `read_team_messages` + mark read |

Harness 通过 internal secret 回调（与 schedule wakeup 同模式），**不写 SQLite**。

#### 4.2.3 Harness 工具 ✅（piPy 扩展）

```
harness/
├── pi_team/                     # 业务逻辑（host 无关）
│   ├── types.py
│   ├── runtime.py
│   ├── client.py                # → /v1/internal/sessions/{id}/teams/*
│   └── tools.py
├── extensions/team_extension.py # 薄注册层
└── oma_adapter/team_bridge.py   # metadata.enable_team_tools 门控
```

| 工具 | 状态 | 行为 |
|------|------|------|
| `team_create` | ✅ | 调 Go API；emit `session.team_created` |
| `spawn_teammate` | ✅ | member + thread + SSE |
| `send_team_message` | ✅ | inbox + `team.message` + 默认 `run_target_turn` |
| `read_team_messages` | ✅ | unread + mark read |

启用方式：Agent `metadata.enable_team_tools: true` 或在 `tools[]` 中声明工具名。

#### 4.2.4 长驻 Teammate 循环 ⏳

当前：`send_team_message(run_target_turn=true)` 经 `Registry.EnqueueEvents` 唤醒目标 thread。  
待补：`pi_team/loop.py` 后台 poll + shutdown 协议。✅ 已实现：`spawn_teammate(start_poll_loop=true)` 启动 loop；`send_team_message` 在 loop 活跃时跳过 `run_target_turn` 避免重复 enqueue。

#### 4.2.5 Console ✅

- Session **Team** Tab（成员、消息时间线）
- 手动 Shutdown teammate（`POST .../members/{id}/shutdown`）

#### 4.2.6 验收

| 项 | 状态 |
|----|------|
| `internal/store/teams_test.go` | ✅ |
| `harness/tests/test_team_tools.py` | ✅ |
| `scripts/e2e/smoke-team-e2e.sh` | ✅ |
| 全链路 eval + Console E2E | ⏳ |
| 租户隔离测试 | ⏳ |

---

### Phase 3 — 任务看板 + 高级隔离（4–5 周）

**目标**：TaskCreate 等价能力 + 可选 worktree sandbox。

#### 4.3.1 `team_tasks` 表

```sql
CREATE TABLE team_tasks (
  id TEXT PRIMARY KEY,
  team_id TEXT NOT NULL,
  subject TEXT NOT NULL,
  description TEXT,
  owner_member_id TEXT,
  status TEXT NOT NULL,  -- pending | in_progress | completed
  blocks_json TEXT,      -- task id 数组
  blocked_by_json TEXT,
  metadata_json TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);
```

Harness 工具：`team_task_create`, `team_task_update`, `team_task_list`（对齐 CC Task* 工具）。

#### 4.3.2 Worktree 沙箱（可选）

- Sandbox manager：`git worktree add` per teammate
- Agent metadata：`isolation: worktree`
- 合并策略：teammate turn 结束 diff summary → leader 审批

参考：`utils/worktree.ts`, `AgentTool` isolation 分支。

#### 4.3.3 Permission 白名单

- `team_allowed_paths` JSON on `teams` 表
- Harness permission hook：member 编辑路径时跳过 ask

---

### Phase 4 — 可选增强

| 项 | 说明 |
|----|------|
| 真 subprocess teammate | 每 member 独立 harness 进程（类比 tmux spawn） |
| Coordinator 模式 | 专用 worker agent 池 + 队列 |
| Eval 扩展 | multi-agent eval 覆盖 SendMessage / task blocked |
| MCP 暴露 | `oma_team_*` tools 供外部 Cursor 调用 |

---

## 5. API 设计草案

### 5.1 创建 Team

```http
POST /v1/sessions/{session_id}/teams
{
  "name": "feature-x",
  "description": "Implement auth refactor",
  "members": [
    { "agent_id": "agt_coder", "display_name": "coder-1", "role": "coder" },
    { "agent_id": "agt_tester", "display_name": "tester-1", "role": "tester" }
  ]
}
```

### 5.2 SendMessage 等价

```http
POST /v1/sessions/{session_id}/teams/{team_id}/messages
{
  "to": "coder-1",           // 或 "*" broadcast
  "summary": "Implement JWT middleware",
  "message": { "type": "text", "text": "..." }
}
```

SSE 事件：

```json
{
  "type": "team.message",
  "team_id": "...",
  "from": "team-lead",
  "to": "coder-1",
  "session_thread_id": "sthr_abc"
}
```

### 5.3 Spawn Teammate（Harness 内部）

Leader 调用工具 `spawn_teammate` 时，Go 侧：

1. Insert `team_members`
2. Emit `session.thread_created`
3. 返回 `{ "member_id", "thread_id", "display_name" }`

---

## 6. Harness 与 Go 分工

| 层 | 负责 |
|----|------|
| **Go** | 持久化、租户 ACL、SSE 广播、Session 状态机 |
| **Harness** | LLM turn、工具实现、sub-turn 循环 |
| **Console** | Team / Thread / Task 可视化 |

原则：**Go 为 source of truth**，Harness 不直接写 SQLite（与现有 session 模式一致）。

---

## 7. 测试策略

| 层级 | 内容 |
|------|------|
| Unit | `team` store CRUD；message read/mark；delegate depth |
| Harness | `test_call_agent.py` + 新 `test_team_tools.py` |
| Go | `teams_test.go`, `teammessage_test.go` |
| E2E | `smoke-team-e2e.sh`：create team → spawn → message → assert SSE |
| Eval | 扩展 `multi-agent.ts`：team spawn + 协作完成任务 |

---

## 8. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| 长驻 teammate 消耗 token | 成本高 | idle timeout + shutdown 协议 |
| 并行 turn 争用 sandbox | 文件冲突 | worktree 或 path 白名单 |
| 与现有 subagent 语义混淆 | 文档/UX | Phase 1 明确「一次性 vs 编队内」 |
| Console 复杂度 | 交付延迟 | Phase 2 先做 API + 最小 Team 列表 |
| CC 工具名不一致 | Prompt 迁移难 | Harness 同时注册 alias（`SendMessage` → `send_team_message`） |

---

## 9. 里程碑与交付物

| 里程碑 | 交付 | 状态 |
|--------|------|------|
| M1 | Phase 1 代码 + eval 绿 | ✅ |
| M2 | `teams` / `agent_messages` + `pi_team` + smoke | 🚧 |
| M3 | Task 看板 + worktree 可选 | ⏳ |
| M4 | Console Team 面板 + 文档 | ✅ |

文档交付（本次）：

- ✅ [claude-code-agent-teams-architecture.md](./claude-code-agent-teams-architecture.md)
- ✅ 本文档

---

## 10. 与 Ruflo 方案的关系

仓库内另有 [ruflo-multi-agent-architecture.md](./ruflo-multi-agent-architecture.md)（MCP Ledger / `.claude-flow` 队列）。

| 维度 | Claude Code Agent Teams | Ruflo |
|------|----------------------|-------|
| 主通道 | 宿主工具 + 文件 inbox | MCP swarm_* + 文件队列 |
| 执行 | Claude Code 多会话 | 同一宿主 LLM |
| oma 优先级 | **P0**（与 Cursor/CC 用户模型一致） | P2（可选 MCP 网关） |

建议：**以本文档（CC Agent Teams）为主路径**；Ruflo 的 MCP 账本、联邦、neural 学习作为 Phase 4+ 可选模块，通过 oma MCP proxy 暴露。

---

## 11. 任务清单与状态

### Phase 1

| ID | 任务 | 状态 | 文件 |
|----|------|------|------|
| T1 | `call_agent` 真并行 | ✅ | `pi_subagent/delegate.py` |
| T2 | 后台 sub-agent + SSE | ✅ | `pi_subagent/delegate.py` |
| T3 | Skills 模板 explore/plan/verify | ⏳ | `data/skills/` |
| T4 | `metadata.default_subagent_roles` | ✅ | `internal/harness/subagent_roles.go` |
| T5 | Eval parallel delegation | ✅ | `test/eval/suites/multi-agent.ts` |
| T6 | Console thread + E2E | ✅ | `scripts/e2e/smoke-subagent-console-e2e.sh` |

### Phase 2

| ID | 任务 | 状态 | 文件 |
|----|------|------|------|
| T7 | `017_teams.sql` + TeamRepo | ✅ | `internal/store/` |
| T8 | Internal + public teams API | ✅ | Python `pi_team/service.py`; Go read-only `ListTeams` + `POST .../events/batch` |
| T9 | `pi_team` + `team_extension` | ✅ | `harness/pi_team/` |
| T10 | `smoke-team-e2e.sh` | ✅ | `scripts/e2e/` |
| T11 | 长驻 teammate loop | ✅ | `pi_team/loop.py` |
| T12 | Console Team Tab | ✅ | `console/src/pages/session-detail/TeamPanel.tsx` |
| T13 | Eval team 协作场景 | ⏳ | `test/eval/suites/multi-agent.ts` |
| T14 | Console Team E2E | ✅ | `scripts/e2e/smoke-team-console-e2e.sh` |

---

## 12. 结论

Claude Code 的多 Agent 本质是 **Team 持久化 + Mailbox 协调 + 多后端 spawn**；oma 已有 **sub-agent 委派 + session thread 观测** 作为地基。

迁移路径：**Phase 1 强化 call_agent → Phase 2 引入 Team/Message 数据模型 → Phase 3 任务看板与隔离**。每阶段可独立 ship，且与现有 `subagent.md` 架构兼容扩展，无需推翻 Session/Thread 模型。
