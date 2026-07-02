# Gate HITL — GT1–GT3 迁移清单

> **目标：** 将 Anthropic cookbook `CMA_gate_human_in_the_loop.ipynb` 所需的 custom-tool HITL 能力从 [open-managed-agents](https://github.com/anthropics/claude-cookbooks/tree/main/managed_agents) 移植到 `oma-platform`。
>
> **探针：** `sdk/example/example3/gate_human_in_the_loop.py`  
> **SDK helper（已就绪）：** `oma_sdk.cookbook.stream_hitl_until_end_turn`（GT4/GT5）  
> **Gap ID：** 见 `sdk/SDK-PLAN.md` § Cookbook parity — gate HITL

---

## 现状速览

| 层 | open-managed-agents | oma-platform 今天 |
|---|---|---|
| Agent 存 `type: "custom"` | ✅ | ✅ JSON 可存，harness 忽略 |
| 模型看到 custom tool | ✅ `buildTools` 无 execute | ✅ Phase C — piPy stub 注册 + JSON Schema |
| 发出 `agent.custom_tool_use` | ✅ `default-loop.ts` | ✅ Phase A — `custom_tools.py` + `emit.py` |
| `requires_action` idle | ✅ `session-do.ts` | ✅ Phase B — `pending_custom_tools.go` + `machine.go` |
| `user.custom_tool_result` 入队 | ✅ | ✅ Phase D — promote 合成 `agent.tool_result` + 续 turn |
| SDK HITL loop | N/A | ✅ `stream_hitl_until_end_turn` |

**oma 已有、可复用：**

- `internal/session/pending.go` — `user.custom_tool_result` 为 pending 类型
- `internal/session/registry_enqueue.go` — promote → `RunTurn`
- `internal/api/tool_confirmation_test.go` — 同类 HITL pending/turn 模式
- `internal/api/eventtypes.go` — 接受 `user.custom_tool_result`
- `sdk/tests/test_gate_cookbook.py` — SDK mock 测试

---

## 推荐实施顺序

```
Phase A  事件分类 + pending 检测（Python，可先用 sim client 测）
Phase B  session machine requires_action（Go）
Phase C  custom tool 注册进 piPy（Python，依赖 piPy API）
Phase D  custom_tool_result → 续 turn（Go promote + Python history）
Phase E  GateSimulatingClient + CI + live gate 探针
```

**最小可验证路径（不等 piPy live）：** Phase A + B + `GateSimulatingClient` → SDK `stream_hitl_until_end_turn` + Go integration 先绿；Phase C/D 再打通 live LLM gate 探针。

### 实施状态（2026-06-30）

| Phase | 状态 | 关键文件 / 测试 |
|---|---|---|
| A — GT2 事件分类 | ✅ | `harness/oma_adapter/custom_tools.py`, `emit.py`, `turn.py`; `harness/tests/test_custom_tools.py` |
| B — GT3 requires_action | ✅ | `internal/harness/pending_custom_tools.go`, `internal/session/machine.go`; `machine_idle_test.go` |
| E（sim 部分） | ✅ | `internal/harness/gate_sim_client.go`, `integrationtest/gate_flow.go`, `TestGateCookbookRequiresAction` |
| C — GT1 piPy | ✅ | `extensions/custom_tools.py`, `register_custom_tools_on_session`, `custom_tools_runtime.py`; tests in `test_custom_tools.py`, `test_tools.py` |
| D — 续 turn | ✅ | `custom_tool_promote.go`, `project.py` continuation slice; `TestGateCookbookHitlResume` |

---

## Phase A — GT2：Harness 事件分类 + pending 检测

**参考（open-managed-agents）：**

| 文件 | 作用 |
|---|---|
| `apps/agent/src/harness/default-loop.ts` | `emitToolCallEvent()`、`isBuiltinTool()` |
| `apps/agent/src/harness/tools.ts` | `ALL_TOOLS` 常量 |

### A1. 工具名分类表

**改：** `harness/oma_adapter/tools.py`

- 新增 `BUILTIN_TOOL_NAMES`（对齐 `OMA_DEFAULT_TOOLS` + MCP/subagent 前缀规则）
- 新增 `is_builtin_tool(name) -> bool`、`custom_tool_names(agent) -> set[str]`
- 从 `agent.tools` 收集 `{type: "custom"}` 的 name 集合

### A2. 事件发射分流

**改：** `harness/oma_adapter/emit.py`

- `tool_use` 分支：若 name ∈ `custom_tool_names` → 发 **`agent.custom_tool_use`**，否则 `agent.tool_use`
- 对照 `default-loop.ts:76-99`

### A3. Turn 结束 pending 检测

**改：** `harness/oma_adapter/turn.py`（`_run_turn_core` 末尾）

- 扫描 pi buffer / `oma_events`：有 `custom_tool_use` 且无对应 `agent.tool_result` → 记入 `pending_custom_tool_ids`
- 对照 `default-loop.ts:779-790`

### A4. TurnResponse 扩展（推荐）

**改：**

- `harness/oma_adapter/types.py` — `TurnResponse.pending_custom_tool_ids: list[str]`
- `internal/harness/client.go` — 同字段

便于 Go 侧不必再扫 event log。

### A5. 测试

**新增：** `harness/tests/test_custom_tools.py`

- custom tool name → `agent.custom_tool_use`
- builtin bash → `agent.tool_use`
- pending id 列表正确

---

## Phase B — GT3：Session machine `requires_action`

**参考：** `apps/agent/src/runtime/session-do.ts:4718-4752`

### B1. 动态 stop_reason

**改：** `internal/session/machine.go`

当前：

```go
PublishStatusIdle(ctx) // 固定 end_turn
```

目标：

```go
PublishStatusIdle(ctx, stopReason StopReason)
```

`RunTurn` 结束后：

1. 从 harness 返回的 `pending_custom_tool_ids`，或扫描本轮 events 找 orphan `agent.custom_tool_use`
2. 若有 pending →  
   `stop_reason: { type: "requires_action", action_type: "custom_tool_result", event_ids: [...] }`
3. 否则 → `end_turn`
4. **GT5 滑动窗口：** parallel >5 时只填前 5 个 id（与 cookbook 一致）

### B2. 会话状态（可选）

**改：** `internal/store/sessions.go`

- 存 `pending_tool_calls` JSON（参考 `session-do.ts:4580-4607`）
- 或仅依赖 event log + 每 turn 重算

### B3. 测试

**新增：** `internal/session/machine_idle_test.go`

- 有 orphan `custom_tool_use` → idle 带 `requires_action` + `event_ids`
- 无 pending → `end_turn`

**新增：** `internal/harness/gate_sim_client.go`（类似 `iterate_sim_client.go`）

- 发 2–3 个 `agent.custom_tool_use`，要求 `requires_action`
- 供 Go integration test 用，无需真实 LLM

---

## Phase C — GT1：Custom tool 注册进 piPy

**参考：** `apps/agent/src/harness/tools.ts:1078-1090`

**难点：** open-managed-agents 用 AI SDK `tool()` 无 `execute`；oma 用 **piPy `create_agent_session(tools=builtin_tools)`**，custom schema 需另路注入。

### C1. 解析 agent custom tools

**改：** `harness/oma_adapter/tools.py`

```python
@dataclass
class CustomToolDef:
    name: str
    description: str
    input_schema: dict

def custom_tools_from_agent(agent: AgentSnapshot) -> list[CustomToolDef]:
    ...
```

### C2. piPy 注册（二选一）

**方案 A（推荐）：** 新 extension  
`harness/oma_adapter/extensions/custom_tools.py`

- turn 前读 agent snapshot 里的 custom defs
- `api.register_tool(...)` 注册 **stub tool**：`execute` 抛 `CustomToolPending` 或返回占位，由 listener 转成 `custom_tool_use` 并中止本轮 execute

**方案 B：** turn 后 hook `session._agent._tools`  
在 `_default_create_session` 之后动态 append stub tools（参考 `_register_mcp_tools_on_session`）

### C3. 把 schema 传给模型

- 确认 piPy / pi_ai 是否支持 JSON Schema tool defs；若不支持，adapter 层转成 pi 格式
- **改：** `_default_create_session` 或 extension，传入 custom tool schemas

### C4. 测试

**改/增：** `harness/tests/test_tools.py`

- agent 含 `decide`/`escalate` → `custom_tools_from_agent` 长度为 2
- session 创建后模型 tool list 含 custom names（mock pi session）

---

## Phase D — GT3 续 turn：`user.custom_tool_result` 恢复

**参考：** `session-do.ts:1261-1279`

### D1. Promote 时合成 `agent.tool_result`

**改：** `internal/session/pending.go` 或 `machine.go` 的 `PromoteOnePending`

当 promote 的事件是 `user.custom_tool_result` 时：

1. 写入 promoted 的 `user.custom_tool_result`（已有）
2. **额外 append** `agent.tool_result`：
   - `tool_use_id` = `custom_tool_use_id`
   - `content` = 来自 client 的 text/json
   - （可选）`parent_event_id` = 同上

与 open-managed-agents 一致，harness 才能从 history 看到 tool 结果。

### D2. Harness history 投影

**改：** `harness/oma_adapter/compaction.py` + `project.py`

- `_model_context_events` 加入 `user.custom_tool_result`（或依赖 D1 合成的 `agent.tool_result`）
- `_event_text` 处理 custom tool result 文本

### D3. 空 message 续跑（可选）

open-managed-agents 在 custom result 后再发空 `user.message` 续跑。  
oma 若 `project_oma_events` 在仅有 tool_result、无新 user.message 时仍能从 history 构造 prompt，可省略；否则在 promote 后 inject 空 `user.message`（与 session-do 对齐）。

### D4. 测试

**新增：**

- `internal/api/gate_cookbook_integration_test.go`
- `test/integration/gate_cookbook_test.go`

流程：

1. create agent（decide/escalate custom tools）
2. upload fixtures + session resources
3. sim harness 发 `custom_tool_use` + `requires_action`
4. client POST `user.custom_tool_result`
5. 断言：第二轮 turn 启动、最终 `end_turn`、12 条 decision（live 时）

**已有：** `sdk/tests/test_gate_cookbook.py`（SDK mock，继续保留）

---

## Phase E — 文档 & CI

| 文件 | 改动 |
|---|---|
| `sdk/SDK-PLAN.md` | GT1–GT3 关闭时更新状态 |
| `.github/workflows/ci.yml` | 加 `TestGateCookbook` / `TestGateCookbookMultiTurn` |
| `sdk/example/example3/gate_human_in_the_loop_main.py` | live 跑通后去掉 probe 硬失败 |

---

## 文件对照表（移植地图）

| GT | open-managed-agents | oma-platform 目标 |
|---|---|---|
| GT1 | `harness/tools.ts` L1078-1090 | `harness/oma_adapter/tools.py` + `extensions/custom_tools.py` |
| GT2 emit | `harness/default-loop.ts` L52-100 | `harness/oma_adapter/emit.py` |
| GT2 pending | `default-loop.ts` L779-790 | `harness/oma_adapter/turn.py` |
| GT3 idle | `runtime/session-do.ts` L4718-4752 | `internal/session/machine.go` |
| GT3 resume | `session-do.ts` L1261-1279 | `internal/session/pending.go` / `PromoteOnePending` |
| GT3 store | `session-do.ts` L4580-4607 | 可选 session metadata |
| 测试参考 | `test/unit/tools-execution.test.ts` | `harness/tests/test_custom_tools.py` |
| Sim client | N/A | `internal/harness/gate_sim_client.go` |

---

## 与 tool_confirmation 的关系

oma 已有 **`user.tool_confirmation`** 的 pending + turn 路径（`internal/api/tool_confirmation_test.go`）。

GT3 应 **复用同一套 pending/promote 机制**，差异在于：

| | tool_confirmation | custom tool (gate) |
|---|---|---|
| idle `action_type` | `tool_confirmation` | `custom_tool_result` |
| client 回传 | `user.tool_confirmation` | `user.custom_tool_result` |
| promote 后 | harness 内置确认逻辑 | 需 **合成 `agent.tool_result`** |

---

## 工作量粗估

| Phase | 规模 | 依赖 |
|---|---|---|
| A（GT2 事件） | 1–2 天 | 无 |
| B（GT3 idle） | 0.5–1 天 | A 的 pending ids |
| C（GT1 piPy） | 2–3 天 | piPy tool API 调研 |
| D（续 turn） | 1 天 | B + C |
| E（CI + live） | 0.5 天 | A–D |

---

## 相关文档

- Cookbook 参考：`managed_agents/CMA_gate_human_in_the_loop.ipynb`（Anthropic claude-cookbooks）
- SDK gap 矩阵：`sdk/SDK-PLAN.md` § Cookbook parity — gate HITL
- open-managed-agents 实现说明：`open-managed-agents/AGENTS.md` § Custom Tools
- open-managed-agents gap 文档（部分 stop_reason 描述已过时，以 `session-do.ts` 为准）：`open-managed-agents/docs/zh-CN/gap-analysis.zh-CN.md`
