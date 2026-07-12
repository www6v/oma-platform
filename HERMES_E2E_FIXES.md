# Hermes Runs API E2E 测试问题修复总结

## 问题描述

Hermes E2E 测试中存在两个主要问题：
1. **工具事件未发出**：`agent.tool_use` 和 `agent.tool_result` 事件偶尔缺失
2. **使用量未捕获**：`span.model_request_end` 事件显示 `input_tokens=0 output_tokens=0`

## 根本原因分析

### 问题 1：工具事件缺失

**原因**：模型行为不确定性
- Hermes 的 SSE 事件发出逻辑是正确的（通过直接 curl 测试验证）
- 模型有时选择不调用工具，而是直接生成文本响应（幻觉）
- 原始提示词不够强制性，导致模型有概率跳过工具调用

**修复方案**：
1. 强化提示词，使用更强制性的语言：
   ```
   CRITICAL: You MUST invoke the terminal tool. Do not describe or simulate anything.
   Call the terminal tool with command='echo hello-from-hermes-e2e' and report the
   actual output. This is a hard requirement — do not skip the tool call.
   ```

2. 添加重试逻辑：如果第一轮没有工具事件，在同一会话中发送催促消息
   - 利用 Hermes 的会话状态保持，第二轮可以看到第一轮的上下文
   - 催促消息明确指出模型没有调用工具

3. 修复多轮等待逻辑：`wait_for_agent_reply` 现在等待最近 `user.message` 之后的终止信号
   - 避免在多轮测试中看到第一轮的 `session.status_idle` 就返回

### 问题 2：使用量未捕获

**原因**：JSON 字段命名不匹配
- OpenAI 格式：`prompt_tokens` / `completion_tokens` / `total_tokens`
- Hermes Runs API 格式：`input_tokens` / `output_tokens` / `total_tokens`
- `openClawUsage` 结构体只有 OpenAI 字段，导致 Hermes 的使用量解码为零值

**修复方案**：
1. 在 `openClawUsage` 结构体中添加 Hermes 字段：
   ```go
   type openClawUsage struct {
       PromptTokens     int `json:"prompt_tokens"`
       CompletionTokens int `json:"completion_tokens"`
       TotalTokens      int `json:"total_tokens"`
       // Hermes Runs API 命名
       InputTokens  int `json:"input_tokens"`
       OutputTokens int `json:"output_tokens"`
       // ...
   }
   ```

2. 在 `toTurnUsage()` 中添加回退逻辑：
   ```go
   in := u.PromptTokens
   if in == 0 {
       in = u.InputTokens
   }
   out := u.CompletionTokens
   if out == 0 {
       out = u.OutputTokens
   }
   ```

3. 添加单元测试 `TestHermesClient_RunTurn_HermesUsageNaming` 验证 Hermes 命名解码

## 验证结果

### 测试输出
```
event count: 14
type histogram: {
  'agent.tool_use': 1,
  'agent.tool_result': 1,
  'agent.message': 6,
  'span.model_request_end': 1,
  ...
}

-- vocabulary check --
  agent.tool_use                 OK
  agent.tool_result              OK
  agent.message                  OK
  span.model_request_end         OK

-- event detail --
  TOOL_USE    name=terminal preview=echo hello-from-hermes-e2e
  TOOL_RESULT name=terminal content=(completed in 0.063s)
  SPAN        model=hermes-agent provider=hermes duration_ms=5460 in=33605 out=120

[hermes-e2e] GET /v1/cost_report
  OUR AGENT agent-n7iz7betw4y3b1ch:
    input_tokens=33605 output_tokens=120 span_count=1
```

### 关键指标
- ✅ 工具事件正常发出（`tool.started` → `agent.tool_use`，`tool.completed` → `agent.tool_result`）
- ✅ Span 捕获使用量（`in=33605 out=120`，之前是 `in=0 out=0`）
- ✅ 成本报告正确显示使用量
- ✅ 所有必需的词汇检查通过

## 修改的文件

1. **`internal/harness/openclaw_client.go`**
   - 添加 `InputTokens` 和 `OutputTokens` 字段到 `openClawUsage`
   - 更新 `toTurnUsage()` 支持 Hermes 命名回退

2. **`internal/harness/hermes_client_test.go`**
   - 添加 `TestHermesClient_RunTurn_HermesUsageNaming` 测试

3. **`scripts/e2e/smoke-hermes-managed-e2e.sh`**
   - 强化提示词
   - 添加重试逻辑（`has_tool_events` 检查 + 催促消息）
   - 修复 `wait_for_agent_reply` 多轮等待逻辑

## 技术要点

### Hermes Runs API 事件流
```
tool.started      → agent.tool_use{name, input:{preview}}
tool.completed    → agent.tool_result{name, content:"(completed in Xs)"}
message.delta     → agent.message (累积文本)
run.completed     → final agent.message + span.model_request_end (带使用量)
```

### 会话状态管理
- Hermes 通过 `session_id` 维护服务端会话状态
- 每轮只需要发送最新的用户消息，不需要重放完整历史
- 多轮对话时，后续轮次可以看到之前的上下文

### 模型行为注意事项
- 即使提示词很强制，模型仍有小概率不调用工具
- 重试机制可以有效提高测试稳定性
- 工具事件标记为可选（optional），不会因为缺失而失败

## 后续建议

1. **监控模型行为**：如果发现工具调用率持续低于预期，可能需要调整提示词或模型参数
2. **考虑 tool_choice 参数**：如果 Hermes API 支持 `tool_choice`，可以强制模型调用工具
3. **优化重试策略**：当前是单次重试，可以根据需要调整为多次重试或指数退避
