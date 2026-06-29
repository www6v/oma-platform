# Cookbook ↔ OMA 对齐 — 工程评审报告

> **目标：** 不是让 `data_analyst_agent.py`「能跑」，而是与 Anthropic cookbook notebook
> (`managed_agents/data_analyst_agent.ipynb`) **API 与语义一致**，使示例成为 parity
> 探针，而非 workaround 集合。
>
> **对比文件：**
> - Cookbook: `harness/claude-cookbooks-main/managed_agents/data_analyst_agent.ipynb`
> - OMA 示例: `oma-platform/sdk/example/data_analyst_agent.py`
>
> **P0 范围（已确认）：** session.resources 挂载 + report.html→Files API + 示例去 workaround

---

## Step 0 — Scope Challenge

### 已有能力（可复用，勿重建）

| 组件 | 位置 | 状态 |
|------|------|------|
| Files upload/list/download | `internal/api/files.go`, `oma_sdk/resources/files.py` | ✅ |
| ResourceResolver（file_id→content_base64） | `internal/harness/resources.go` | ✅ 仅读 **environment snapshot** |
| Resource mounter | `harness/oma_adapter/resource_mounter.py` | ✅ |
| Session outputs store | `internal/sessionoutputs/store.go` | ✅ 读 `SESSION_OUTPUTS_DIR/{tenant}/{session_id}/` |
| workdir outputs symlink | `internal/workdir/manager.go` | ✅ `.mnt/session/outputs` → sessionoutputs |
| Events SSE | `GET /v1/sessions/{id}/events/stream` | ✅ |
| Anthropic SDK 路由 | `OMAClient` + `base_url` | ✅ agents/sessions/environments |

### 最小对齐集（P0）

1. **Session 级 `resources[]` 在 turn 前 resolve + mount**
2. **`/mnt/session/outputs/` 写入 → `sessionoutputs` → `files.list(scope_id=session.id)` 可下载**
3. **`data_analyst_agent.py` 改回 cookbook 流程，删除 workaround**

### 明确 NOT in scope（本阶段）

| 项 | 理由 |
|----|------|
| 真实 cloud 容器 + Docker 镜像构建 | 需独立 runtime 项目；cookbook `type:cloud` 本地无法等价 |
| `environments.work.*` 自托管 worker | SDK-PLAN 已标 out-of-scope |
| `managed-agents-2026-04-01` beta header | OMA 无 Anthropic beta 概念 |
| Slack bot 续篇（cookbook §7 `.env` 复用） | 独立 feature |
| `client.beta.files` 完全合并进 anthropic SDK | P2 DX，不阻塞 parity |

### 可延后 P1/P2

- Environment `packages.pip` 本地安装（或文档声明仅 Docker cloud）
- `sessions.resources.*` CRUD（POST create 之外的 add/update/delete）
- Turn 超时 API 化（非 env var）
- SDK 封装 `GET /sessions/{id}/outputs`

---

## 逐步 Gap 矩阵（Cookbook vs OMA 现状）

```
Cookbook 7 步                    OMA 现状                         Gap ID
─────────────────────────────────────────────────────────────────────
1. environments.create(packages)  config 仅存 DB，本地不 pip install   E1 (P1)
2. agents.create                  ✅ 基本对齐                        —
3. beta.files.upload              httpx async，非 beta.files          F1 (P2)
4. sessions.create(resources=[])  Go 忽略 resources[]                 S1 (P0)
5. events.send + stream           send ✅；示例用 poll 非 stream       EV1 (P1)
6. files.list(scope_id)+download  scope_id 常返回 []                  O1 (P0)
7. archive + 复用 agent/env       示例每次 timestamp 新建             C1 (P1)
```

---

## 1. Architecture Review

### 1A — Session resources 未进入 turn 链路 [P0] (confidence: 10/10)

**Cookbook：**

```python
session = client.beta.sessions.create(
    environment_id=env.id,
    agent={"type": "agent", "id": agent.id, "version": agent.version},
    resources=[{"type": "file", "file_id": dataset.id, "mount_path": MOUNT_PATH}],
    title="Sales analysis",
)
```

**OMA Go server 实际行为：**

- `createSessionRequest` **没有** `resources` 字段（`internal/api/sessions.go`）
- 响应里 `resources` **恒为** `[]`（`internal/api/sessionwire.go`）
- Turn 资源只来自 **environment snapshot** 的 `config.resources`（`internal/session/machine.go` → `ResourceResolver.ResolveForTurn`）

TS 参考实现（`test/integration/files.test.ts`）有 session resources，**Go server 未实现**。

**推荐方案：** Session 表增 `resources` JSON；create 接受 `resources[]`；`ResolveForTurn` 合并 **session resources + env resources**（session 优先）。

```
┌─────────────┐     upload      ┌──────────────┐
│  Files API  │◄────────────────│   Client     │
└──────┬──────┘                 └──────┬───────┘
       │ file_id                        │
       ▼                                ▼
┌──────────────────────────────────────────────────┐
│  POST /v1/sessions  { resources: [{file_id,...}] }│
│       │ persist session.resources[]               │
│       ▼                                           │
│  Session Machine → ResolveForTurn(session+env)    │
│       │ content_base64                            │
│       ▼                                           │
│  Harness mount_resources → /mnt/session/uploads/…  │
└──────────────────────────────────────────────────┘
```

---

### 1B — 输出路径分裂：sandbox vs sessionoutputs [P0] (confidence: 9/10)

**Cookbook：** agent 写 `/mnt/session/outputs/report.html` → Files API `scope_id=session.id` 可列出并下载。

**OMA 设计：**

- `workdir/manager.go` 把 `.mnt/session/outputs` symlink 到 `session-outputs/default/{session_id}/`
- `files.go` 在 `files.list(scope_id=...)` 时合并 sessionoutputs

**实测问题：** agent 通过 bash 写到 `sandboxes/{id}/mnt/session/outputs/`（无 `.` 前缀），**未进入** sessionoutputs，`files.list` 返回 `[]`。

**推荐方案：**

- harness `bash` 与 piPy file tools 共用 `sandbox_paths.normalize_sandbox_path`
- turn 结束后 scan workdir outputs，缺失则 copy/sync 到 sessionoutputs
- 或禁止 `mnt/` 非 symlink 路径（mkdir 时只保留 `.mnt/session/outputs`）

---

### 1C — 双客户端 + 示例 workaround 掩盖 gap [P1] (confidence: 9/10)

当前 `data_analyst_agent.py` 的 workaround **不应成为终态**：

| Workaround | 应对的 gap |
|------------|-----------|
| CSV 挂 `environment.config.resources` | S1 |
| `wait_for_idle` 用 `events.list` 轮询 | EV1（可接受，但应同时支持 stream） |
| `download_report` 读 sandbox 磁盘 | O1 |
| 每次 `name=f"...-{timestamp}"` | C1 |

**对齐后示例应恢复为：**

```python
# 与 cookbook 同序、同 API 形态
env = client.environments.create(name=ENV_NAME, config={packages...})  # 无 file
agent = client.agents.create(...)
dataset = await client.files.upload(...)
session = client.sessions.create(..., resources=[{file_id, mount_path}])
client.sessions.events.send(...)
await wait_for_idle_stream(session.id)  # stream 优先
await download_via_files_api(session.id)  # 无磁盘 fallback
```

磁盘 fallback 可保留为 **dev-only**（`OMA_DEV_FALLBACK=1`），默认关闭。

---

### 1D — Environment packages 本地不生效 [P1] (confidence: 8/10)

Cookbook 依赖预装 pandas/plotly。OMA 存 config，harness **无 pip install**。实测 `ModuleNotFoundError: plotly`。

**推荐：** 二选一，写进文档 + 测试 skip 条件：

- **Boring path：** 本地 harness 在 turn 前读 `env.config.packages.pip` 并 `uv pip install`（慢但 parity）
- **Explicit path：** 文档声明 packages 仅 cloud/Docker 生效；本地 dev 用 harness venv 预装（`start-harness.sh` 加 optional sync）

---

## 2. Code Quality Review

### 2A — 示例脚本职责混乱 [P2] (confidence: 8/10)

`data_analyst_agent.py` 混了 parity demo、本地 fallback、错误处理。建议拆分：

- `example/data_analyst_agent.py` — cookbook 1:1（失败即暴露 gap）
- `oma_sdk/api/data_analyst.py` — `DataAnalystExamples.run_cookbook_flow()` 供 E2E 调用

### 2B — SDK-PLAN 与 Go 实现漂移 [P2] (confidence: 9/10)

`SDK-PLAN.md` 写 session resources 为 P2 gap，但 TS integration tests 已覆盖。需以 **Go server 行为** 为 single source of truth，更新 PLAN + 示例注释。

---

## 3. Test Review

### 代码路径 + 用户流覆盖图

```
COOKBOOK PARITY E2E (目标: test/integration/data_analyst_cookbook.go 或 sdk e2e)
[+] POST /v1/files (CSV upload)
  ├── [GAP] multipart + JSON 双模式
[+] POST /v1/sessions { resources: [{file_id, mount_path}] }
  ├── [GAP] [→E2E] resources 出现在 session 响应
  ├── [GAP] [→E2E] harness turn 内 cat /mnt/session/uploads/sales_data.csv 成功
[+] POST /v1/sessions/{id}/events (user.message)
  ├── [GAP] [→E2E] session.status_idle
[+] GET /v1/files?scope_id={session_id}
  ├── [GAP] [→E2E] 含 report.html + sales_data.csv
[+] GET /v1/files/{out:...}/content
  └── [GAP] [→E2E] report.html bytes > 10KB

COVERAGE: 0/7 cookbook steps on Go server (0%)
REGRESSION: 现有 test/integration/files.test.ts 跑 CF Workers，不覆盖 Go — 需 Go 等价测试
```

### 必加测试（P0 验收标准）

| 测试 | 断言 |
|------|------|
| `TestSessionCreateWithFileResource` | create 返回 `resources[0].mount_path` |
| `TestResourceMountInHarnessTurn` | bash cat 挂载路径 = upload 内容 |
| `TestSessionOutputInFilesList` | agent write report.html → `files.list(scope_id)` 非空 |
| `test_data_analyst_cookbook_e2e.sh` | SDK 示例零 fallback 跑通 |

**LLM eval [→EVAL]：** 不阻塞 API parity；report 质量另测。

---

## 4. Performance Review

### 4A — Turn 超时 300s [P1] (confidence: 9/10)

`HARNESS_TURN_TIMEOUT_SEC=300`（`harness/oma_adapter/main.py`）。Cookbook 分析 ~3–5 分钟，复杂 agent 易超时。

**推荐：** session 或 environment config 增加 `turn_timeout_sec`；默认 900，上限可配。

---

## What Already Exists

- ResourceResolver + mounter 管道（改数据源即可）
- Files API + scope_id 合并 outputs 逻辑
- workdir symlink 机制（修路径一致性即可）
- OMA SDK events stream（示例未用）
- `GET /v1/sessions/{id}/outputs/{filename}`（SDK 未封装）

---

## Implementation Tasks（P0 对齐，按依赖排序）

```
Lane A (platform, sequential):
  T1 → T2 → T3 → T4

Lane B (SDK, after T2):
  T5 → T6

Lane C (tests, after T3+T5):
  T7
```

| ID | Priority | Task | Files | Verify |
|----|----------|------|-------|--------|
| **T1** | P0 | Session create 接受并持久化 `resources[]` | `internal/api/sessions.go`, `internal/store/sessions.go`, `sessionwire.go` | unit: create 返回 resources |
| **T2** | P0 | `ResolveForTurn` 合并 session + env resources | `internal/harness/resources.go`, `machine.go` | harness test: file mounted |
| **T3** | P0 | 统一 outputs 路径 + turn 后 sync | `sandbox_paths.py`, `resource_mounter.py`, optional `machine.go` post-turn | files.list 含 report.html |
| **T4** | P0 | Go integration test 复刻 cookbook §3–6 | `test/integration/data_analyst_cookbook_test.go` | CI green |
| **T5** | P0 | 重写 `data_analyst_agent.py` 为 cookbook 1:1 | `sdk/example/data_analyst_agent.py` | 无 fallback 跑通 |
| **T6** | P1 | `DataAnalystExamples` helper + E2E | `oma_sdk/api/data_analyst.py`, `tests/test_data_analyst.py` | pytest |
| **T7** | P1 | 更新 SDK-PLAN gap 状态 | `sdk/SDK-PLAN.md` | doc review |

---

## Failure Modes（生产视角）

| 场景 | 当前 | P0 后 |
|------|------|-------|
| session.resources 被忽略 | agent 找不到 CSV | mount 成功 |
| bash 写错 outputs 路径 | Files API 空，用户无报告 | 统一路径或 sync |
| turn 300s 超时 | session.error + 半成品 report | P1 可配 timeout |
| packages 未安装 | plotly ImportError | P1 文档或本地 pip |
| files.list 空但磁盘有文件 | 脚本 fallback 掩盖 | fallback 默认 off，测试失败 |

---

## 对齐后的目标示例形态（终态）

与 notebook 逐步对应：

| Step | Cookbook | OMA 对齐后 |
|------|----------|-----------|
| 1 | `environments.create(packages)` | 同（packages P1 另议） |
| 2 | `agents.create` | 同（model 对象形态） |
| 3 | `beta.files.upload` | `client.files.upload`（async，文档说明） |
| 4 | `sessions.create(resources=...)` | **同，不再挂 environment** |
| 5 | `events.send` + `events.stream` | send 同；stream 用 OMA SSE 或 anthropic stream |
| 6 | `files.list(scope_id)` + download | **同，无磁盘 fallback** |
| 7 | `sessions.archive` + `.env` 复用 | archive 同；可选 `SessionExamples` 复用 helper |

---

## 总览 Gap 表

| 维度 | Cookbook (Anthropic) | OMA 现状 | 差距等级 |
|------|---------------------|----------|---------|
| 客户端模型 | 单一 `Anthropic()` 客户端 | `OMAClient` = anthropic SDK + httpx 双通道 | 中 |
| 运行时 | 托管 cloud 容器 | 本地 piPy harness + 本地 subprocess 沙箱 | **高** |
| Session 资源挂载 | `sessions.create(resources=[...])` | Go server **不处理** session.resources | **高** |
| 环境 packages | 容器构建时预装 | 仅存储配置，本地 **不安装** | **高** |
| 输出文件回收 | Files API `scope_id` 即可下载 | 路径不一致时 Files API 为空 | **高** |
| 事件流 | `sessions.events.stream()` 同步 SSE | 需用 OMA 原生 `events.list` 轮询 | 中 |

---

## 建议补齐顺序

```
P0 — 阻塞 data analyst 与 cookbook 对齐
├── S1/S3  Session create resources[] + turn 前 resolve/mount
├── O1/O2  统一 /mnt/session/outputs 写入 → sessionoutputs → Files API scope_id
└── 示例   去掉 workaround，恢复 cookbook 流程

P1 — API 形态对齐
├── F1/T20  client.beta.files 别名到 OMA FilesResource
├── EV1     统一 events.stream 事件结构与 SDK 对象
├── E1      本地 harness 执行 environment.packages（或文档明确仅 Docker cloud）
└── EV2     turn 超时可通过 session/agent 配置或 API 传递

P2 — 完整 Managed Agents parity
├── S2      sessions.resources CRUD
├── O3      SDK 封装 GET /sessions/{id}/outputs
└── E2      真实 cloud sandbox（packages/networking 按 environment 构建）
```

---

## Completion Summary

| 项 | 结果 |
|----|------|
| Step 0 Scope | **Accepted A** — P0 三件套 |
| Architecture | **4 issues** (S1/O1 为 P0 blocker) |
| Code Quality | **2 issues** (示例拆分、PLAN 漂移) |
| Test Review | **7 gaps**, 0% Go-server cookbook coverage |
| Performance | **1 issue** (turn timeout) |
| NOT in scope | cloud runtime, work.*, beta header |
| What exists | ResourceResolver, Files API, workdir symlink |
| Failure modes | **2 critical** (S1, O1) |
| Parallelization | Lane A sequential → B → C |

**Lake Score：** P0 选完整 parity（10/10），非 shortcut。

---

## 建议下一步

1. **开 implementation issue/branch**：按 T1→T7 顺序，T1+T2 可先做（session resources）。
2. **把 `data_analyst_agent.py` 标为 parity probe**：在 README 注明「失败 = 平台 gap」，workaround 移到 `example/v1/` 或 dev-only flag。
3. **补 Go integration test**：Port `files.test.ts` 的 session resources 用例到 Go，避免 TS/Go 漂移。
