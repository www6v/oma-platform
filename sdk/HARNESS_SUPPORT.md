# Agent Harness 支持文档

## 概述

OMA SDK 现在支持在创建 agent 时指定不同的 harness 后端。harness 是负责执行 LLM 循环和工具调用的运行时环境。

## 支持的 Harness 类型

| Harness 类型 | 描述 | 内部标识 |
|-------------|------|---------|
| `pipy` | 平台自带的 Python sidecar（默认） | `default-loop` |
| `hermes` | Hermes agent，通过 Runs API 调用 | `hermes` |
| `openclaw` | OpenClaw Gateway | `openclaw` |
| `default-loop` | `pipy` 的别名 | `default-loop` |
| `managed` | 托管 harness（当前为 stub） | `managed` |

## 使用方法

### 基本用法

```python
from oma_sdk import OMAClient
from oma_sdk.api.agents import AgentExamples

# 初始化客户端
client = OMAClient(
    base_url="http://127.0.0.1:8787",
    api_key="dev-key"
)

# 使用默认的 pipy harness
result = AgentExamples.create_and_retrieve(
    client,
    name="my-agent"
)

# 明确指定 pipy harness
result = AgentExamples.create_and_retrieve(
    client,
    name="my-agent",
    harness="pipy"
)

# 使用 hermes harness
result = AgentExamples.create_and_retrieve(
    client,
    name="my-hermes-agent",
    harness="hermes"
)

# 使用 openclaw harness
result = AgentExamples.create_and_retrieve(
    client,
    name="my-openclaw-agent",
    harness="openclaw"
)
```

### 在 Example 脚本中使用

```python
# example/my_script_main.py
from oma_sdk.api.agents import AgentExamples

# 创建使用 hermes 的 agent
agent = client.agents.create(
    name="my-agent",
    model={"id": "qwen3.7-plus"},
    system="You are a helpful assistant.",
    metadata={"_oma.harness": "hermes"}  # 手动指定
)

# 或使用 AgentExamples 辅助函数
result = AgentExamples.create_and_retrieve(
    client,
    name="my-agent",
    harness="hermes"
)
```

## API 参考

### AgentExamples 方法

以下方法现在都支持 `harness` 参数：

- `create_and_retrieve(client, name, harness=None)`
- `update_agent(client, name_before, name_after, harness=None)`
- `list_agent_versions(client, name, harness=None)`
- `archive_agent(client, name, harness=None)`
- `delete_agent(client, name, harness=None)`

### 参数说明

**harness** (str, optional): Harness 类型，可选值：
- `"pipy"` - 使用默认的 piPy harness（默认值）
- `"hermes"` - 使用 Hermes agent
- `"openclaw"` - 使用 OpenClaw Gateway
- `"default-loop"` - piPy 的别名
- `"managed"` - 使用托管 harness（当前为 stub）

如果不指定，默认使用 `pipy` harness。

## 实现细节

### Metadata 传递

Harness 类型通过 agent 的 `metadata` 字段传递：

```python
metadata = {"_oma.harness": "hermes"}
```

OMA 平台在创建 session 时会读取 agent 的 `_oma.harness` 元数据，并根据其值选择相应的 harness 客户端来处理 turn。

### Harness 映射

SDK 内部会将友好的 harness 名称映射到内部标识符：

```python
harness_map = {
    "pipy": "default-loop",
    "hermes": "hermes",
    "openclaw": "openclaw",
    "default-loop": "default-loop",
    "managed": "managed",
}
```

## 配置要求

使用不同的 harness 需要相应的环境变量配置：

### Hermes

```bash
export OMA_HERMES_BASE_URL="http://124.221.28.203:8642"
export OMA_HERMES_API_KEY="your-hermes-key"
```

### OpenClaw

```bash
export OMA_OPENCLAW_BASE_URL="http://124.221.28.203:17772"
export OMA_OPENCLAW_API_KEY="your-openclaw-key"
```

### piPy (默认)

piPy 是本地 harness，不需要额外配置。

## 测试

运行测试脚本验证不同 harness 的支持：

```bash
cd sdk
python test_harness_types.py
```

## 相关文档

- [可插拔 Harness 设计方案](../../docs/plans/pluggable-harness-zh.md)
- [Runtime Architecture](../../docs/design/runtime-architecture.md)
- [HERMES E2E 修复报告](../../HERMES_E2E_FIXES.md)
