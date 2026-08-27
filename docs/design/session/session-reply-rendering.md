# Session 页面回复 & 工具调用渲染机制

> 记录 OMA Console (`meta-harness/console`) session 详情页里"助手回复 + 工具调用"的渲染链路。
> 来源：`console/src/pages/SessionDetail.tsx` + `console/src/components/ai-elements/tool.tsx`。

---

## 1. 数据来源：事件流

`SessionDetail` 在挂载时：

- 调 `/v1/sessions/:id/events` 按 `seq` 升序分页回填历史（每页 200）。
- 通过 `streamEvents()` 建立 SSE 长连接，新事件进入 `addEvent()`。

`addEvent()` 对 **流式增量** 和 **落定事件** 做分流：

| SSE 类型 | 处理 |
|---|---|
| `agent.message_stream_start` / `agent.message_chunk` / `agent.message_stream_end` | 写入 `streams` Map（key=`message_id`），**不**入 `events` |
| `agent.thinking_stream_start` / `agent.thinking_chunk` / `agent.thinking_stream_end` | 写入 `thinkingStreams` Map |
| `agent.tool_use_input_stream_start` / `agent.tool_use_input_chunk` / `agent.tool_use_input_stream_end` | 写入 `toolInputStreams` Map（部分 JSON） |
| `agent.message` / `agent.thinking` / `agent.tool_use` / `agent.mcp_tool_use` / `agent.custom_tool_use` | 落定后从对应流 Map 里 **删除**，避免双渲染，再推入 `events` |
| `session.status_running` / `session.status_idle` / `session.error` | 更新 `status` 和 HITL 状态 |
| `system.user_message_pending/promoted/cancelled` | 维护"出站箱" `pendingByEventId`，不入 `events` |

---

## 2. 渲染顺序（Conversation 视图）

在 `<Conversation>` 容器里，按 **严格顺序** 叠加七层内容（见 `SessionDetail.tsx` 约 1248–1463 行）：

1. `events` 里过滤到当前 thread 的 **落定事件** → `EventRender`
2. `localPending` 乐观出站槽（Send 按下到 SSE 回传之间的空档）
3. `pendingByEventId` 服务端镜像的排队中用户消息
4. `thinkingStreams` 进行中的推理块（`<Reasoning isStreaming>`）
5. `toolInputStreams` 进行中的工具输入（部分 JSON，`state="input-streaming"`）
6. `streams` 进行中的助手文本流 + 光标动画
7. 什么流都没有但 `status==="running"` → 三点 typing dots

---

## 3. `EventRender`：事件 → 组件的映射

它是 switch 分发器（约 1890 行起）：

- **`user.message`** → `<Message from="user">`，带 Pending/Retracted 覆盖样式；`metadata.harness==="schedule"` 的唤醒走 `from="system"` 蓝色气泡。
- **`agent.message`** → `<Message from="assistant">` + `<Markdown>`。
- **`agent.thinking`** → `<Reasoning defaultOpen={false}>`（折叠，点击展开）。
- **工具调用**：三种落定事件共用一条路径
  - `agent.tool_use`（内置）
  - `agent.custom_tool_use`（自定义）
  - `agent.mcp_tool_use`（MCP，标题带 `mcp · <server_name>` 后缀）
- **`agent.tool_result` / `agent.mcp_tool_result`**：先做 **配对**（按 `tool_use_id` / `mcp_tool_use_id` 查表），已配对的 result 跳过独立渲染，由 use 卡片一并展示；未配对的渲染为 "tool result (unpaired)" 退化卡。
- **`session.error` / `session.warning`** → 红/琥珀色 alert div，error 还会附上从 `span.model_request_end` 配对到的上游真实原因（限流、401 等）。

---

## 4. 工具卡片的具体视觉（`ai-elements/tool.tsx`）

`<Tool>` 本质是 shadcn 的 `<Collapsible>`：

- **`<ToolHeader>`** 左侧是 **工具图标**（按名字映射：`web_search` → 放大镜、`web_fetch` → 地球、`schedule*` → 时钟、其它 → 扳手）+ **工具名** + **状态 Badge**。
  - 状态机：`input-streaming` (Pending) → `input-available` (Running) → `output-available` (Completed) / `output-error` (Error) / `output-denied` (Denied)。
  - 错误时 badge 红色 XCircle，整卡走 destructive 样式。
- **`<ToolContent>`** 折叠体里：
  - `<ToolInput>`：单键短字符串（如 `bash` 的 `command`）渲染为内联 `key: value`；其它渲染成 JSON `<CodeBlock>`。
  - `<ToolOutput>`：字符串非 JSON → 等宽 mono 白空间；对象/JSON → `<CodeBlock language="json">`；错误 → 红色背景 + `errorText`。

---

## 5. 关键设计点

- **落定事件优先**：流式增量只活在 Map 里，`agent.*` 落定事件一到就把流删掉，由 events 列表接管，避免重复渲染。
- **配对而不是并列**：`tool_use` 与 `tool_result` 通过 id 预配对，合并到一张卡里，比两条独立气泡更直观。
- **线程过滤**：所有渲染都按 `session_thread_id === activeThreadId` 过滤，sub-agent 通过顶部 `ThreadTree` 切换。
- **错误溯源**：`session.error` 自身只有通用文案，真正原因从它前面最近的 `span.model_request_end (is_error)` 取，作为 `modelErrorCause` 注入到 error 卡。

---

## 6. 总结

整体流程就是：

> SSE → `addEvent` 分流 → 流 Map 实时渲染 + 落定事件走 `EventRender` 的 switch
> → 工具类事件用 `<Tool> / <ToolHeader> / <ToolInput> / <ToolOutput>` 组成可折叠卡片，
> 状态 Badge 反映当前阶段。
