# Claude Code Agent Teams 多 Agent 方案（原项目架构说明）

> 来源项目：`/Users/t-wangwei07/Downloads/workspacePy/harness/claude/claude-code-rev/src`
> 用途：为迁移到 oma-platform 提供「原样理解」基线，描述 Claude Code 宿主内的 Agent Teams / Swarm 实现。

---

## 1. 定位与核心思想

Claude Code 的多 Agent 不是独立的 Worker 集群，而是 **同一 CLI 进程（或子进程）内，由 Leader 通过工具编排的多个 LLM 会话**。

| 角色 | 职责 | 执行主体 |
|------|------|----------|
| **Team Lead** | 拆任务、spawn 队友、SendMessage 协调、Monitor 观测 | 用户当前 Claude Code 会话 |
| **Teammate** | 接收 prompt / mailbox 消息，独立跑 agent turn | 子进程（tmux/iTerm）或同进程 AsyncLocalStorage |
| **Sub-agent（Task）** | 一次性或后台任务，可 worktree 隔离 | `runAgent()` 子链，不一定加入 Team |
| **Coordinator** | 可选模式：专用 worker agent 池 | `coordinator/workerAgent`（feature gate） |

关键约束（源码多处注释与 `isAgentSwarmsEnabled()`）：

- Agent Teams 需 **显式开启**：`CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1` 或 `--agent-teams`，且 GrowthBook `tengu_amber_flint` 未关闭。
- **Spawn 只创建协调状态 + 执行环境**，真正推理仍由 LLM 在 teammate 会话里完成。
- Leader 不应 spawn 后空等；应继续 Task / SendMessage / Monitor 驱动团队。

```mermaid
flowchart TB
  subgraph User
    U[用户 Prompt]
  end
  subgraph Lead["Team Lead 会话"]
    AT[Agent / Task 工具]
    SM[SendMessage]
    TC[TeamCreate / TaskCreate]
    MON[Monitor]
  end
  subgraph Teammates["Teammate 实例"]
    T1[Teammate A]
    T2[Teammate B]
  end
  subgraph Persistence["磁盘状态"]
    TF[~/.claude/teams/{team}/config.json]
    MB[inboxes/{name}.json]
    TL[tasks/*.json]
    WT[git worktree 可选]
  end
  U --> Lead
  AT -->|spawn| Teammates
  SM --> MB
  Teammates --> MB
  TC --> TF
  TC --> TL
  Teammates --> T1
  Teammates --> T2
  AT --> WT
```

---

## 2. 多 Agent 的四种模式（迁移时必须区分）

| # | 模式 | 入口工具 | 并行性 | 典型场景 |
|---|------|----------|--------|----------|
| 1 | **Sub-agent（Task）** | `Agent`（原 `Task`） | 可后台 async / fork | 探索代码、写补丁、验证 |
| 2 | **Named Teammate** | `Agent` + `name` + `team_name` | 真并行（多 pane/进程） | 长期协作的专员 |
| 3 | **Agent Team** | `TeamCreate` → spawn 队友 | 真并行 | 固定编制团队 |
| 4 | **Coordinator Mode** | env `CLAUDE_CODE_COORDINATOR_MODE` | 进程内 worker 池 | 大规模任务编排 |

oma-platform 当前最接近 **#1**（`call_agent_*` 单次 sub-turn）；Claude Code 额外强在 **#2/#3 持久 Team + Mailbox + 多后端 spawn**。

---

## 3. 工具面（稳定契约）

### 3.1 核心工具

| 工具 | 源文件 | 作用 |
|------|--------|------|
| `Agent` | `tools/AgentTool/AgentTool.tsx` | 启动 sub-agent；可 spawn teammate（`name`, `team_name`, `mode`） |
| `SendMessage` | `tools/SendMessageTool/SendMessageTool.ts` | 队友间消息；支持 broadcast、shutdown/plan 结构化消息 |
| `TeamCreate` | `tools/TeamCreateTool/TeamCreateTool.ts` | 创建 team 目录与 `TeamFile` |
| `TeamDelete` | `tools/TeamDeleteTool/` | 清理 team 与 task 目录 |
| `TaskCreate/List/Get/Update/Stop` | `tools/TaskCreateTool/` 等 | 团队任务看板（Todo v2） |
| `Monitor` | `tools/MonitorTool/MonitorTool.ts` | 观测在跑 agent / teammate 状态 |
| `EnterWorktree` / `ExitWorktree` | `tools/EnterWorktreeTool/` | Leader 进入隔离 worktree |

### 3.2 Agent 工具参数（多 Agent 扩展）

`AgentTool` 在 base schema 上合并 multi-agent 字段（`AgentTool.tsx`）：

```typescript
{
  description: string,
  prompt: string,
  subagent_type?: string,      // explore / plan / general-purpose / 自定义
  model?: 'sonnet' | 'opus' | 'haiku',
  run_in_background?: boolean,
  // --- multi-agent ---
  name?: string,               // 可被 SendMessage({ to: name }) 寻址
  team_name?: string,
  mode?: PermissionMode,       // 如 plan mode required
  isolation?: 'worktree' | 'remote',
  cwd?: string
}
```

### 3.3 SendMessage 结构化消息

`SendMessageTool` 支持 discriminated union（`SendMessageTool.ts`）：

- 纯文本 + `summary`（UI 预览）
- `shutdown_request` / `shutdown_response`
- `plan_approval_response`

路由目标：`to` = 队友名、`*` 广播、或（feature）`uds:` / `bridge:` 跨进程 peer。

---

## 4. 目录与持久化

### 4.1 Team 注册表

路径：`~/.claude/teams/{team_name}/config.json`（由 `teamHelpers.ts` 读写）

`TeamFile` 结构要点：

```typescript
{
  name: string,
  description?: string,
  createdAt: number,
  leadAgentId: string,
  leadSessionId?: string,
  teamAllowedPaths?: TeamAllowedPath[],  // 全队可编辑路径白名单
  members: [{
    agentId, name, agentType?, model?, prompt?, color?,
    planModeRequired?, joinedAt, tmuxPaneId, cwd,
    worktreePath?, sessionId?, backendType?, isActive?, mode?
  }]
}
```

### 4.2 Mailbox（A2A）

路径：`~/.claude/teams/{team}/inboxes/{agent_name}.json`（`teammateMailbox.ts`）

```typescript
type TeammateMessage = {
  from: string,
  text: string,
  timestamp: string,
  read: boolean,
  color?: string,
  summary?: string
}
```

- 文件锁 + 重试，支持多 Claude 实例并发写。
- Teammate 轮询 unread → 注入为 user attachment（`TEAMMATE_MESSAGE_TAG`）。

### 4.3 任务看板

路径：`~/.claude/tasks/{taskListId}/`（`tasks.ts`）

- Leader 创建 team 后 `setLeaderTeamName()`，task list 与 team 名对齐。
- Task 含 `owner`（agent id）、`blocks` / `blockedBy` 依赖图。
- `TaskCreateTool` 与 hooks 联动（`executeTaskCreatedHooks`）。

### 4.4 Agent 定义资产

| 来源 | 加载器 | 说明 |
|------|--------|------|
| Built-in | `builtInAgents.ts` | general-purpose, explore, plan, verification… |
| `.claude/agents/*.md` | `loadAgentsDir.ts` | YAML frontmatter：tools, model, mcpServers, hooks, memory |
| Plugin agents | `loadPluginAgents.ts` | 插件市场扩展 |

`AgentDefinition` frontmatter 支持：专属 MCP、permission mode、effort、memory scope、颜色。

---

## 5. Spawn 与执行后端

### 5.1 统一入口

`tools/shared/spawnMultiAgent.ts` — `spawnTeammate()`：

1. 分配 `teammateId`、`color`、pane 布局
2. 写 `TeamFile.members`
3. 按 backend 启动 Claude 子实例或 in-process runner
4. 返回 `SpawnOutput`（pane id、session、team_name…）

### 5.2 后端注册表

`utils/swarm/backends/registry.ts` 检测顺序：

| Backend | 条件 | 实现 |
|---------|------|------|
| **InProcess** | 无 tmux/iTerm 或 fallback | `InProcessBackend` + `AsyncLocalStorage` |
| **Tmux** | `isInsideTmux()` / 外部 swarm session | `TmuxBackend` |
| **iTerm2** | macOS + it2 CLI | `ITermBackend` |

环境变量：

- `CLAUDE_CODE_TEAMMATE_COMMAND` — 覆盖 spawn 命令
- `CLAUDE_CODE_AGENT_COLOR` — 队友颜色
- `CLAUDE_CODE_PLAN_MODE_REQUIRED` — 队友强制 plan mode

### 5.3 In-Process Teammate

`utils/swarm/inProcessRunner.ts`：

- `runWithTeammateContext()` 隔离 agentId / teamName / parentSessionId
- 调用 `runAgent()`，工具集含 SendMessage + Task* + Team*
- 支持 plan approval 轮询、`useSwarmPermissionPoller`
- 进度写入 `InProcessTeammateTask` AppState

### 5.4 Sub-agent 执行核心

`tools/AgentTool/runAgent.ts`：

- 独立 MCP 连接（agent frontmatter `mcpServers`）
- 独立 transcript 子目录（`setAgentTranscriptSubdir`）
- Hooks：`executeSubagentStartHooks`
- 可选 worktree（`createAgentWorktree`）或 remote CCR
- Fork 子 agent（`forkSubagent.ts`，feature gate）

---

## 6. 身份与上下文解析

`utils/teammate.ts` 优先级：

1. **AsyncLocalStorage**（in-process teammate）
2. **dynamicTeamContext**（CLI args：`--agent-id`, `--team-name`…）
3. **AppState.teamContext**（leader 侧）

辅助函数：

- `isTeammate()` / `isTeamLead()` / `getTeamName()`
- `getParentSessionId()` — 关联 leader session
- `isPlanModeRequired()` — 队友 plan gate

---

## 7. 权限与协调

| 机制 | 文件 | 行为 |
|------|------|------|
| Team 路径白名单 | `teamHelpers.ts` | `teamAllowedPaths` 全队免 ask |
| Permission sync | `permissionSync.ts` | 队友 mode 同步 |
| Leader permission bridge | `leaderPermissionBridge.ts` | 队友 ask → leader 审批 |
| Swarm permission poller | `useSwarmPermissionPoller.ts` | plan/shutdown 结构化响应 |

---

## 8. 任务生命周期与 UI

| 组件 | 作用 |
|------|------|
| `utils/task/framework.ts` | `registerTask` / poll / SDK 事件 |
| `tasks/LocalAgentTask/` | 后台 agent 进度、通知 |
| `tasks/InProcessTeammateTask/` | In-process 队友 UI 状态 |
| `TeammateViewHeader` / `TeammateSpinnerTree` | 多 pane 可视化 |

后台 agent：`run_in_background` 或 auto-background（120s GrowthBook gate）。

---

## 9. Feature Gate 总览

| Gate | 控制项 |
|------|--------|
| `isAgentSwarmsEnabled()` | Agent Teams 总开关 |
| `isTodoV2Enabled()` | TaskCreate/List 工具 |
| `isForkSubagentEnabled()` | Fork 子 agent |
| `feature('COORDINATOR_MODE')` | Coordinator worker 池 |
| `feature('UDS_INBOX')` | SendMessage UDS peer |
| GrowthBook 多项 | explore/plan agents, verification, auto-background… |

---

## 10. 典型主流程

### 10.1 创建 Team 并 spawn 队友

```
TeamCreate({ team_name, description })
→ spawnTeammate({ name, prompt, team_name, subagent_type })
  → TmuxBackend | InProcessBackend 启动队友
→ SendMessage({ to: name, message, summary })
→ 队友 readMailbox → runAgent loop
→ Monitor 观测 / TaskCreate 分配工作项
```

### 10.2 单次 Sub-agent（无 Team）

```
Agent({ description, prompt, subagent_type: 'Explore' })
→ runAgent() 同步或 async
→ 结果返回 leader tool result
```

### 10.3 Worktree 隔离

```
Agent({ isolation: 'worktree', prompt })
→ createAgentWorktree()
→ 队友在独立 git worktree 改代码
→ hasWorktreeChanges → 合并或清理
```

### 10.4 Plan mode 队友

```
spawnTeammate({ mode: 'plan' }) 或 PLAN_MODE_REQUIRED env
→ 队友必须先 ExitPlanMode 获 leader 批准
→ SendMessage plan_approval_response
```

---

## 11. 与 oma `call_agent` 的本质差异

| 维度 | Claude Code Agent Teams | oma-platform 现状 |
|------|-------------------------|-------------------|
| 执行模型 | 多会话 / 多 pane / 可选 in-process | 单 harness 进程内 sub-turn |
| 团队持久化 | `TeamFile` + mailbox 文件 | `session.thread_*` 事件 |
| 寻址 | `SendMessage({ to: name })` | 无 mailbox；一次性 tool return |
| 并行 | 真并行（多 OS 进程） | 串行 await sub-turn（tool `parallel` 仅标记） |
| 任务看板 | TaskCreate + 文件锁 | 无 |
| 隔离 | worktree / remote / cwd | 共享 sandbox workdir |
| Agent 定义 | Markdown + built-in | DB `agents` + Skills API |
| 生命周期 | 长驻队友 + shutdown 协议 | 委派结束即 thread 归档倾向 |

---

## 12. 迁移阅读清单（给 oma 团队）

按优先级阅读源码：

1. `utils/agentSwarmsEnabled.ts` — 总开关语义
2. `tools/AgentTool/AgentTool.tsx` — sub-agent vs teammate 分支
3. `tools/shared/spawnMultiAgent.ts` — spawn 全流程
4. `utils/teammateMailbox.ts` — A2A 消息模型
5. `utils/swarm/teamHelpers.ts` — TeamFile  schema
6. `tools/SendMessageTool/SendMessageTool.ts` — 结构化协调
7. `utils/swarm/inProcessRunner.ts` — 无 tmux 时的 in-process 路径
8. `tools/AgentTool/runAgent.ts` — 子 agent turn 核心
9. `utils/tasks.ts` — 团队任务看板
10. `utils/swarm/backends/registry.ts` — 后端选择

---

## 13. 能力矩阵（摘要）

| 能力 | Claude Code 实现 | 成熟度 |
|------|-----------------|--------|
| Sub-agent 委派 | AgentTool + runAgent | 高 |
| 具名 Teammate | spawn + SendMessage | 高 |
| Team 注册表 | TeamFile JSON | 高 |
| Mailbox A2A | 文件 inbox + lock | 高 |
| 多后端 spawn | tmux / iTerm / in-process | 高 |
| 任务依赖图 | TaskCreate/Update | 中 |
| Worktree 隔离 | worktree.ts | 高 |
| Plan/shutdown 协议 | SendMessage structured | 中 |
| 远程队友 | remote isolation + CCR | 中 |
| 持久化 API / 多租户 | 无（本地 CLI） | 不适用 |

此矩阵用于下一文档《Claude Code Agent Teams → oma-platform 迁移方案》的 gap 对照。
