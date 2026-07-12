# Harness 支持实现总结

## 修改内容

### 1. 核心修改：`oma_sdk/api/agents.py`

添加了 harness 参数支持，允许在创建 agent 时指定不同的 harness 后端。

#### 新增内容：

1. **类型定义**
   ```python
   HarnessType = Literal["pipy", "hermes", "openclaw", "default-loop", "managed"]
   ```

2. **辅助函数 `_build_metadata()`**
   - 将 harness 参数转换为 metadata 格式
   - 映射友好的名称到内部标识符
   - 返回 `{"_oma.harness": "..."}` 格式的字典

3. **更新的方法**（都新增了 `harness` 参数）：
   - `create_and_retrieve(client, name, harness=None)`
   - `update_agent(client, name_before, name_after, harness=None)`
   - `list_agent_versions(client, name, harness=None)`
   - `archive_agent(client, name, harness=None)`
   - `delete_agent(client, name, harness=None)`

#### 修改模式：

所有方法都遵循相同的修改模式：

```python
# 之前
agent = client.beta.agents.create(name=name, model=MODEL)

# 之后
metadata = _build_metadata(harness)
create_kwargs = {"name": name, "model": MODEL}
if metadata:
    create_kwargs["metadata"] = metadata

agent = client.beta.agents.create(**create_kwargs)
```

### 2. 测试脚本：`test_harness_types.py`

创建了测试脚本，验证所有 harness 类型的支持：
- 测试 5 种 harness 类型
- 显示创建结果和 metadata
- 错误处理和报告

### 3. 使用示例：`example/example_harness_selection.py`

创建了实际使用示例，展示：
- 如何创建 pipy agent（默认）
- 如何创建 hermes agent
- 如何创建 openclaw agent
- 完整的错误处理和输出格式

### 4. 文档：`HARNESS_SUPPORT.md`

创建了完整的用户文档，包含：
- Harness 类型说明
- 使用方法
- API 参考
- 配置要求
- 测试说明

## 支持的 Harness 类型

| 参数值 | 内部标识 | 说明 |
|-------|---------|------|
| `"pipy"` | `default-loop` | 平台自带的 Python sidecar（默认） |
| `"hermes"` | `hermes` | Hermes agent，通过 Runs API |
| `"openclaw"` | `openclaw` | OpenClaw Gateway |
| `"default-loop"` | `default-loop` | pipy 的别名 |
| `"managed"` | `managed` | 托管 harness（当前为 stub） |

## 使用示例

### 简单用法

```python
from oma_sdk import OMAClient
from oma_sdk.api.agents import AgentExamples

client = OMAClient(base_url="http://127.0.0.1:8787", api_key="dev-key")

# 使用 hermes harness
result = AgentExamples.create_and_retrieve(
    client,
    name="my-agent",
    harness="hermes"
)
```

### 直接调用 API

```python
# 手动指定 metadata
agent = client.beta.agents.create(
    name="my-agent",
    model={"id": "qwen3.7-plus"},
    metadata={"_oma.harness": "hermes"}
)
```

## 测试运行

```bash
# 运行测试脚本
cd /Users/t-wangwei07/Downloads/workspacePy/mycode/oma/oma-platform/sdk
python test_harness_types.py

# 运行示例
cd example
python example_harness_selection.py
```

## 向后兼容性

- 所有修改都是向后兼容的
- `harness` 参数是可选的，默认为 `None`
- 不指定 harness 时，行为与之前完全相同
- 现有代码无需修改即可继续工作

## 实现原理

1. **Metadata 传递**：harness 类型通过 agent 的 `metadata` 字段传递
2. **OMA 平台**：在创建 session 时读取 `_oma.harness` 元数据
3. **Harness 派发**：`harness.Registry` 根据元数据选择相应的 Client
4. **Turn 执行**：使用选定的 harness 客户端处理 LLM turn

## 相关文件

- `oma_sdk/api/agents.py` - 核心实现
- `test_harness_types.py` - 测试脚本
- `example/example_harness_selection.py` - 使用示例
- `HARNESS_SUPPORT.md` - 用户文档
- `docs/plans/pluggable-harness-zh.md` - 设计方案

## 下一步

1. 运行测试验证功能
2. 在实际 example 中集成 harness 选择
3. 更新其他 example 脚本支持 harness 参数
4. 添加环境变量配置文档（OMA_HERMES_*, OMA_OPENCLAW_*）
