# Resource Mounter 与 Outcome Evaluator

本文说明 oma-platform 中 **Resource Mounter**（资源挂载器）与 **Outcome Evaluator**（结果评判器）分别是什么、在系统里如何分工，以及与源项目 `open-managed-agents` 的对应关系。

---

## 一句话

| 组件 | 作用 | 发生在何时 |
|------|------|------------|
| **Resource Mounter** | 把 Environment 声明的文件、Memory、环境变量等「考前资料」放进 Agent 沙箱 workdir | 每个 **Session Turn 开始**、Agent 真正推理之前 |
| **Outcome Evaluator** | 用 LLM 对照 **Rubric** 评判 Agent 最终输出是否达标 | **Eval Trial** 全部 user 消息发完、Session idle 之后 |

二者分别解决「Agent 跑之前能读到什么」与「跑完之后算不算过关」。

---

## 在系统中的位置

```
┌─────────────────────────────────────────────────────────────────┐
│  Session Turn 路径（正常对话 / Eval 发消息）                     │
├─────────────────────────────────────────────────────────────────┤
│  Environment snapshot                                           │
│       │                                                         │
│       ▼                                                         │
│  ResourceResolver.ResolveForTurn  (Go, oma-server)              │
│       │  展开 spec → 带正文 payload                               │
│       ▼                                                         │
│  TurnRequest.resources → Harness POST /internal/turn            │
│       │                                                         │
│       ▼                                                         │
│  mount_resources(workdir, resources)  (Python harness)          │
│       │  写入 workdir + 注入 env                                  │
│       ▼                                                         │
│  project_oma_events → piPy Agent 推理                             │
└─────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────┐
│  Eval Trial 评分路径（仅测评）                                   │
├─────────────────────────────────────────────────────────────────┤
│  Trial 消息全部发完 → Session idle                                │
│       │                                                         │
│       ▼                                                         │
│  AgentOutputFromEvents  (从 session events 拼 assistant 文本)   │
│       │                                                         │
│       ▼                                                         │
│  eval.Worker.scoreTrial → Harness POST /internal/evaluate-outcome │
│       │                                                         │
│       ▼                                                         │
│  evaluate_outcome (LLM-as-judge) → reward 0/1 + feedback        │
└─────────────────────────────────────────────────────────────────┘
```

与 [Eval Run Background Worker](./eval-run-background-worker.md) 的关系：Worker 负责「发题、等答完」；Outcome Evaluator 负责「答完后的判分」。与 [Memory 设计](./memory.md) 的关系：Memory Store 作为 Environment resource 时，经 Resource Mounter 落到沙箱，而非 Agent 在回合内直接调 Memory API。

---

## Resource Mounter

### 是什么

**Resource Mounter** 把 **Environment** 里配置的 `resources`（或 `config.resources`）在回合开始前物化到当前 Session 的 **workdir**，使 piPy Agent 通过读文件、环境变量等方式使用这些资源。

在 oma-platform 中实现为 **两段式**：

1. **平台侧解析（Resolve）**：`internal/harness/resources.go` 中的 `ResourceResolver`，在 Go 进程内从 DB / blob 拉取正文，生成 Harness 可消费的 JSON。
2. **Harness 侧挂载（Mount）**：`harness/oma_adapter/resource_mounter.py` 中的 `mount_resources`，在 Python 侧写入磁盘并更新 `os.environ`。

源项目对应：`open-managed-agents` 的 `runtime/resource-mounter.ts`（迁移任务 **P2-3**）。

### 触发时机与调用链

每个 Turn 在 Session Machine 里组装 `TurnRequest` 时解析资源（`internal/session/machine.go`）：

```go
resolved, _ := m.Resources.ResolveForTurn(ctx, m.TenantID, envSnap)
// ...
harness.TurnRequest{ Resources: resources, Workdir: workdirPath, ... }
```

Harness `turn.py` 在 `project_oma_events` **之前** 调用挂载：

```python
saved_env = mount_resources(workdir, resources)
```

Turn 结束时在 `finally` 中按 `saved_env` 的 key 从 `os.environ` 移除，避免污染后续回合。

### Environment 中的 Resource Spec

从 Environment snapshot 读取，支持两种 JSON 路径：

- 顶层 `resources: [...]`
- `config.resources: [...]`

常见声明示例：

```json
{
  "type": "file",
  "file_id": "file-abc",
  "mount_path": "/mnt/session/uploads/demo.pdf"
}
```

```json
{
  "type": "memory_store",
  "memory_store_id": "memstore-xxx",
  "access": "read_only"
}
```

```json
{
  "type": "env",
  "name": "API_BASE",
  "value": "https://api.example.com"
}
```

### ResourceResolver（Go）行为

| `type` | 解析动作 | 输出 payload 要点 |
|--------|----------|-------------------|
| `file` | `FileRepo.Get` + `FileBlobs.Read` | `content_base64`、`mount_path`（默认 `/mnt/session/uploads/{file_id}`） |
| `memory_store` | `GetStore` + `ListMemories`（必要时 hydrate 正文） | `store_name`、`read_only`、`memories: [{path, content}]` |
| `env` / `env_secret` | 透传 name/value | `type`、`name`、`value` |
| `github_repository` / `github_repo` | 透传 url、branch、mount_path 等 | 供远端 Runtime 使用；本地 Harness **不 clone** |

策略：**best-effort**。单条 resource 解析失败则跳过，不阻断整次 Turn。

### mount_resources（Python）行为

| `type` | 挂载方式 | workdir 内路径 |
|--------|----------|----------------|
| `file` | 解码 `content_base64` 或写 `content` 文本 | `{workdir}/{mount_path 去首斜杠}` |
| `memory_store` | 每条 memory 写 UTF-8 文件；`read_only` 且文件已存在则跳过覆盖 | `{workdir}/mnt/memory/{store_name}/{path}` |
| `env` / `env_secret` | `os.environ[name] = value`，并返回 key 列表供回合结束清理 | 进程环境变量 |
| `github_repository` | **本地 Harness 仅打 warning 并 skip** | — |

单条挂载异常时 `logger.exception` 后跳过，不抛错阻断 Turn。

### 设计约束与边界

- **回合级 snapshot**：`ResolveForTurn` 每次拉取 memory store 内**全部** memory 正文（大 store 有体积与延迟成本，见 [memory.md](./memory.md)）。
- **仅回合开始写入**：Agent 在回合内通过 piPy 工具改 workdir 文件，**不会**自动回写 Memory Store；与平台 Memory API 的持久化路径分离。
- **GitHub 仓库**：payload 会传到 Harness，但本地 sidecar 不执行 git clone；完整能力依赖 CF Runtime / 远端 Daemon（见 [runtime-architecture.md](./runtime-architecture.md)）。

### 相关代码

| 文件 | 职责 |
|------|------|
| `internal/harness/resources.go` | `ResourceResolver`、`ResolveForTurn` |
| `internal/session/machine.go` | Turn 前调用 Resolver，填入 `TurnRequest.Resources` |
| `harness/oma_adapter/resource_mounter.py` | `mount_resources` 及 file/memory/env 实现 |
| `harness/oma_adapter/turn.py` | 回合内调用挂载与 env 清理 |
| `internal/harness/resources_test.go` | Resolver 单测 |
| `harness/tests/test_resource_mounter.py` | Mounter 单测 |

---

## Outcome Evaluator

### 是什么

**Outcome Evaluator** 在测评场景下，根据 Task 定义的 **Rubric**（描述 + 可选 criteria 列表），用 **LLM-as-judge** 判断 Agent 的**最终输出**是否满足要求。

- 判定结果：`satisfied` 或 `needs_revision`
- 附带 `feedback` 说明未达标原因

源项目对应：`open-managed-agents` 的 `harness/outcome-evaluator.ts`（迁移任务 **P2-4**）。

### 何时运行

仅 **Eval Trial** 生命周期末尾，由 `internal/eval/worker.go` 在以下条件成立后调用 `scoreTrial`：

1. Trial 状态为 `running`
2. Session 状态为 `idle`
3. 所有 task spec 中的 user messages 已发送完毕

```text
trial 消息发完 + idle
  → scoreTrial
  → reward = 1（satisfied）→ trial completed
  → reward = 0（needs_revision）→ trial failed，Error 写入 feedback
```

若无 Rubric（`description` 为空）或 Evaluator 未配置，则默认 `reward = 1`，与早期「仅看是否跑完」行为兼容。

### 数据流

```
Session Events (DB)
    → AgentOutputFromEvents
        仅聚合 type=agent.message 的 text 块，多条 assistant 消息用空行拼接

eval.Worker.resolveJudgeModel
    → 优先 agent.aux_model，否则 agent.model
    → modelresolve.Resolver 解析 provider / api_key / base_url

harness.OutcomeEvaluator.EvaluateOutcome
    → HTTPClient POST /internal/evaluate-outcome

outcome_evaluator.evaluate_outcome
    → pi_ai complete_simple，thinking_level=off
    → 解析 JSON：{"result": "satisfied"|"needs_revision", "feedback": "..."}
```

### Rubric 与请求体

Task spec 中的 rubric 映射为：

```json
{
  "rubric": {
    "description": "回答应包含三个要点……",
    "criteria": ["提到 X", "格式为 JSON", "……"]
  },
  "agent_output": "Agent 拼接后的全文",
  "model": {
    "model": "...",
    "provider": "...",
    "api_key": "...",
    "base_url": "..."
  }
}
```

Harness 返回：

```json
{
  "result": "satisfied",
  "feedback": "All criteria met."
}
```

### LLM 评判逻辑（Python）

`harness/oma_adapter/outcome_evaluator.py`：

- System prompt 要求严格但公正，**仅输出 JSON**
- User prompt 包含 Requirements、Criteria、Agent Output 三段
- 最多 **4 次**尝试（`MAX_RETRIES = 3`），指数退避 + jitter
- 用正则提取响应中第一个 `{...}` 再 `json.loads`
- 解析失败或重试耗尽 → `needs_revision`，feedback 含错误摘要

### Go 侧接口

`internal/harness/client.go`：

- `OutcomeEvaluator` 接口：`EvaluateOutcome(ctx, OutcomeEvaluateRequest)`
- `HTTPClient` 实现：POST `{BaseURL}/internal/evaluate-outcome`，超时默认 2 分钟
- `AsOutcomeEvaluator(c Client)`：从 Harness 客户端提取评判能力
- `oma-server` 启动时：`eval.Worker{ Evaluator: harness.AsOutcomeEvaluator(harnessClient) }`

### 评分与 Run 汇总

| 字段 | 含义 |
|------|------|
| `trial.reward` | `1.0` = satisfied；`0.0` = needs_revision 或无输出 |
| `trial.error` | 未达标时写入 evaluator feedback |
| Run 通过率 | 各 task trial 完成后由 worker 汇总（见 eval-run-background-worker） |

### 相关代码

| 文件 | 职责 |
|------|------|
| `harness/oma_adapter/outcome_evaluator.py` | LLM judge 核心逻辑 |
| `harness/oma_adapter/main.py` | `POST /internal/evaluate-outcome` |
| `harness/oma_adapter/types.py` | `OutcomeEvaluateRequest/Response` |
| `internal/eval/outcome.go` | `scoreTrial`、`resolveJudgeModel` |
| `internal/eval/agent_output.go` | 从 events 提取 assistant 文本 |
| `internal/eval/worker.go` | Trial 结束时调用 `scoreTrial` |
| `internal/harness/client.go` | `OutcomeEvaluator` 接口与 HTTP 客户端 |
| `harness/tests/test_outcome_evaluator.py` | JSON 解析与评判单测 |

---

## 二者对比

| 维度 | Resource Mounter | Outcome Evaluator |
|------|------------------|-------------------|
| 目标 | 输入侧：给 Agent 上下文与文件 | 输出侧：评判 Agent 是否完成任务 |
| 运行进程 | Go 解析 + Python 写盘 | Go 调度 + Python 调 LLM |
| 触发 | 每个 Session Turn | 每个 Eval Trial 结束 |
| 与 Session 关系 | 正常对话与 Eval 共用 | 仅 Eval |
| 失败策略 | best-effort，跳过单条 resource | 解析/调用失败 → needs_revision |
| 源项目文件 | `resource-mounter.ts` | `outcome-evaluator.ts` |
| MVP 任务 | P2-3 ✅ | P2-4 ✅ |

---

## 验收与测试

```bash
# Harness 侧
cd harness && uv run pytest tests/test_resource_mounter.py tests/test_outcome_evaluator.py -v

# Go 侧
go test ./internal/harness/... ./internal/eval/...
```

端到端冒烟脚本 `scripts/smoke-t13-e2e.sh` 包含上述 Python 测试。

---

## 相关文档

- [Eval Run Background Worker](./eval-run-background-worker.md) — Trial 状态机与 Worker Tick
- [Memory 设计](./memory.md) — memory_store resource 与挂载路径
- [Runtime 架构](./runtime-architecture.md) — GitHub 等资源在远端 Runtime 的扩展点
- [MVP 迁移计划](../MVP-MIGRATION-PLAN.md) — P2-3 / P2-4 任务表
