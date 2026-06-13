# Eval Run Background Worker

本文说明 oma-platform 中 **Eval run background worker** 的职责、工作流程，以及与源项目 `open-managed-agents` 的对应关系。

## 一句话

**Eval run background worker 是后台自动跑 Agent 测评任务的「调度员」。**

你在 Console 或 API 里创建一次 **Eval run**（测评批次），相当于给 Agent 交了一份「考卷」：多道题（tasks），每道题有一串要发给 Agent 的话（messages）。创建后记录会先停在 **`pending`（待执行）**。如果没有后台 worker，这份考卷只会躺在数据库里，**不会真的去和 Agent 对话**。

Worker 补上这块：在 `oma-server` 里每隔约 30 秒醒一次，把还没跑完的测评往前推一步。

---

## 生活例子

| 角色 | 对应什么 |
|------|----------|
| 你 / Console | 出题、交卷（`POST /v1/evals/runs`） |
| 数据库 | 记分牌（记录 `pending` / `running` / `completed`） |
| **Eval worker** | 监考老师，按顺序发题、等 Agent 答完、记分 |
| Agent + Session | 考生，每道题开一个独立对话 |

---

## 它具体会做哪些事？

Worker 每次 `Tick` 会：

1. **找出活跃的测评**（`pending` 或 `running`）
2. **按层级推进**：Run → Task → Trial
   - **Run**：整次测评
   - **Task**：一道题
   - **Trial**：同一道题可跑多次（做稳定性测试）
3. **对每个 Trial**：
   - 创建一个新的 Session（独立对话）
   - 按顺序发送题目里的 user messages
   - 等 Agent 忙完（Session 变成 `idle`）再发下一条
   - 全部发完 → 标记 trial `completed`，记下 trajectory
   - 超时或出错 → 标记 `failed`
4. **全部 task 跑完** → 整次 run 变成 `completed` 或 `failed`，并算一个简单分数（通过率）

状态流转：

```
pending → running → completed（或 failed）
```

---

## 与 open-managed-agents 的对应

| 维度 | open-managed-agents (CF) | oma-platform |
|------|--------------------------|--------------|
| 触发方式 | cron `tickEvalRuns` | 进程内 goroutine + `time.Ticker` |
| 实现位置 | integrations / cron 路径 | `internal/eval/worker.go` |
| 会话驱动 | SessionDO + dispatch | `eval.SessionRunner` → `internal/api/eval_dispatch.go` |

迁移计划中的任务编号：**P1-4 / T8**（`MVP-MIGRATION-PLAN.md`）。

---

## 实现要点

### 启动与配置

`oma-server` 启动时默认启用 worker（`cmd/oma-server/main.go`）：

| 环境变量 | 含义 | 默认 |
|----------|------|------|
| `OMA_EVAL_WORKER_DISABLED` | 设为 `1` 关闭 worker | 启用 |
| `OMA_EVAL_WORKER_INTERVAL` | 每轮扫描间隔 | `30s` |

### 核心类型

- **`eval.Worker`**（`internal/eval/worker.go`）：扫描活跃 run，调用 `advanceRun` → `advanceTask` → `advanceTrial`
- **`eval.SessionRunner`**（`internal/eval/session.go`）：抽象「建 Session、发消息、查状态」
- **`api.evalSessionRunner`**（`internal/api/eval_dispatch.go`）：把 Session API 适配给 worker 使用

### Trial 推进逻辑（简化）

```
pending trial
  → CreateTaskSession
  → SendUserMessage（第一条）
  → status = running

running trial（每 Tick 检查一次）
  → SessionStatus == idle ?
      → 还有下一条 message → SendUserMessage
      → 没有下一条 → completed，记录 trajectory_id
  → 超时 → failed
```

默认单次 trial 超时：**1 小时**（`defaultTrialTimeout`）；task spec 可设 `timeout_ms` 覆盖。

### 评分（当前 MVP）

- 每个 trial 跑完且无错误 → `reward = 1`，计为 pass
- Run 结束时：若有 completed task，score ≈ `completed_count / task_count`
- **已接入** P2 outcome evaluator：trial 完成后按 task `rubric` 调用 harness `/internal/evaluate-outcome`（LLM-as-judge），写入 `trial.reward` / 失败原因

---

## 数据流

```
Console / API
    POST /v1/evals/runs  →  DB（status: pending）
              │
              ▼
    eval.Worker.Tick()（每 30s）
              │
              ├─ CreateTaskSession（per trial）
              ├─ EnqueueEvents（user.message）
              └─ 等待 Session idle → 下一条或完成
              │
              ▼
    UpdateProgress / MarkFinished  →  DB + Console 展示
```

与 Agent 主路径的关系：worker **不直接调 harness**，而是通过现有 Session 事件队列，与正常用户发消息走同一条 turn 链路（Registry → harness → SSE）。

---

## 当前边界与后续

| 已有 | 缺口（见 MVP 计划） |
|------|---------------------|
| CRUD + `pending` 创建 | — |
| 后台 worker 状态机 | P2-4 outcome evaluator（按输出质量评判） |
| 简单通过率 score | 更细粒度 rubric / 自动判题 |
| worker 单测 `internal/eval/worker_test.go` | 进程 crash 后重试策略（failure modes 表） |

---

## 相关代码

| 文件 | 说明 |
|------|------|
| `internal/eval/worker.go` | Worker 主逻辑 |
| `internal/eval/worker_test.go` | 状态机单测 |
| `internal/api/eval_dispatch.go` | Session 适配层 |
| `internal/api/eval_runs.go` | HTTP CRUD |
| `internal/store/eval_runs.go` | 持久化 |
| `cmd/oma-server/main.go` | 启动 ticker |

---

## 验收

迁移计划验收标准：

```text
pending → running → completed
```

可通过 `go test ./internal/eval/...` 与创建 eval run 后观察 Console / API 状态变化验证。
