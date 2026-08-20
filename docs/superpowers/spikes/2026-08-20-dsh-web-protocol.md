# Spike 报告：dsh web 协议验证（源码核实版）

日期：2026-08-20
状态：**Go**（web HTTP RPC + WebSocket 网关接入可行）
方法：因本机无浏览器交互环境，改为直接审阅 dsh 源码中的 wire schema、载运层实现与测试基线；所有结论附源码出处，可信度等同于抓包。

## 1. 上行：HTTP RPC（POST /api/<method>）

- 方法名是**点分**的：`session.create`、`session.prompt`、`session.cancel`、`session.history`、`session.selectModel`、`llm.models` …（完整分发表：`packages/host/apiproxy/src/fetch/handler.ts` `UNARY_ROUTES`）。URL 路径即方法名：`POST /api/session.prompt`。
- 请求体是四象限信封 **ClientRequest**（`packages/host/apiproxy/src/api/rpc.ts`）：

```json
{ "type": "client-request", "rpcId": "<uuid>", "method": "session.prompt", "payload": { } }
```

- 必须 `Content-Type: application/json`（否则 415，跨站写入围栏）。
- 响应体是 **ServerResponse**，业务错误**也是 HTTP 200**：

```json
{ "type": "server-response", "rpcId": "<echo>", "result": { "ok": true, "value": { "accepted": true } } }
{ "type": "server-response", "rpcId": "<echo>", "result": { "ok": false, "error": { "code": "session-not-found", "message": "…", "details": {} } } }
```

- HTTP 状态码只表达载运层：404（未知路径）/ 415（非 JSON media type）/ 400（body 非 JSON）/ 500（handler 崩溃）。
- 关键端点 payload（`packages/host/apiproxy/src/api/sessions.schema.ts`）：
  - `session.create`: `{ workspaceId?, cwd?, sessionId?, agentPreset? }` → `{ sessionId }`。可传入自定 `sessionId`（非空字符串）——OMA 可直接用自己的 session id。
  - `session.prompt`: `{ sessionId, mode: "queue"|"steer", content: [{type:"text",text}|{type:"image",mediaType,data}], clientTimeZone? }` → `{ accepted: true }`。
  - `session.cancel`: `{ sessionId }` → `{ accepted: true }`。
  - 无 `session/wait` 之类的阻塞端点——turn 结果只能经下行流收取。

## 2. 下行：WebSocket /api/events.mux

- `GET /api/events.mux` 现在只回答 401/Upgrade Required（SSE 回退已移除，见 `.agents/notes/implemented/architecture/2026-08-04-websocket-downlink-carrier.md`）。浏览器路径为 WebSocket；Go 客户端直接拨 `ws://host:port/api/events.mux`。
- **每条 text 消息 = 一个完整 ServerRequest JSON 文档**（裸 JSON，无二进制分帧）：

```json
{ "type": "server-request", "rpcId": "<uuid>", "method": "session/event",
  "payload": { "type": "session/event", "sessionId": "<sid>", "event": { "type": "…", "seq": 3, "time": 1.7e12, "data": { } }, "view": null } }
```

- mux 是**全会话聚合流**（`packages/host/apiproxy/src/api/events.ts` `EventsApi.mux`）：打开时对每个已挂载会话发 `session/subscribed`（含 `lastSeq`），然后持续推送所有会话的事件。客户端必须按 `payload.sessionId` 过滤。
- MuxFrame 词汇表（`payload.type`）：`session/event`、`session/subscribed`、`approval/requested|resolved`、`question/requested|resolved`、`session/queue`、`session/jobs`、`session/projection`、`stream/error`。
- 断开 WS 即取消对应 host 流；`session.cancel` RPC 是显式取消手段。

## 3. SessionEvent → oma 事件映射（实现依据）

SessionEvent 信封（`packages/core/session/src/types.ts`）：`{ type, seq, time, data, sourceEventSeqs?, ignorable? }`。

| dsh 事件 | data 形态 | oma 映射 |
|---|---|---|
| `assistant/chunk` | `{ turn, step, chunk: { type: "text-delta", index, text } }` | 累积文本 → `agent.message`（增量快照） |
| `assistant/message` | `{ turn, step, message: { content: [{type:"text",text}…] }, usage?: TokenUsage }` | 完整 `agent.message`；`usage` 提取为 TurnUsage |
| `tool/call` | `{ turn, step, callId, name, arguments }` | `agent.tool_use` |
| `tool/result` | `{ turn, step, message, error?: {name, code} }` | `agent.tool_result` |
| `turn/end` | `{ turn, reason: {kind: "completed"\|"aborted"\|"blocked"\|"error"\|"max-tokens"\|"interrupted", …} }` | turn 终止信号；`kind:"error"` → turn 失败 |

`TokenUsage`（`packages/llm/llm/src/types.ts:135`）：`{ inputTokens, outputTokens, cacheReadTokens?, cacheWriteTokens?, reasoningTokens? }`。
`StreamChunk`（同文件 :312）：`block-start | text-delta | reasoning-delta | tool-call-delta | block-end | usage | finish`。

## 4. 启动与绑定

- 启动：`pnpm dsh web`（= `node --import tsx/esm apps/cli/src/bin.ts`，`web` 是 `--profile web` 别名）。
- web 应用自有旗标（`packages/bundle/web-app/src/startup.ts`）：`--host <host>`、`--port <port>`、`--trusted-host <authority...>`（可重复）。默认 `127.0.0.1:3080`。
- **`--host 0.0.0.0` 被刻意禁止**（startup.ts:69-71，安全理由）。容器部署两条路：
  1. （推荐）`--host <容器 eth0 IP>` + `--trusted-host <同值>`；compose 中用 entrypoint 脚本 `hostname -i` 解析。
  2. socat 转发：`socat TCP-LISTEN:3080,fork,reuseaddr TCP:127.0.0.1:3080`。

## 5. 安全模型

- **无鉴权**。只有浏览器信任围栏（`packages/client/connection/src/api-request-trust.ts`）：Host 围栏（loopback 或 `trustedHosts` 字面量）+ `Sec-Fetch-Site`/Origin 同源检查。
- **非浏览器客户端（OMA Go）不带 Origin** → Host 围栏即通过：loopback 天然通过；docker 内跨容器访问时 Host 是 `dsh:3080`，必须把 `dsh` 声明为 `--trusted-host`。
- 结论：dsh 端口绝不能暴露到宿主/公网（与 spec 风险 #4 一致）。

## 6. 模型与凭证

- 默认模型：`deepseek-official` provider / `deepseek-v4-flash`（`packages/bundle/base/cordis.patch.yml` `dsh-agent-default-model`）。
- 凭证：默认读环境变量 `DEEPSEEK_API_KEY`（`packages/llm/llm-deepseek/src/index.ts:45`）；也可经 settings 文件/credentials 域配置。docker compose 注入 `DEEPSEEK_API_KEY` 即可。
- 可选 `DEEPSEEK_BASE_URL`（llm-deepseek 适配器支持自定义 endpoint）。

## 7. 与实施计划的差异（已回写计划）

1. RPC 信封：计划占位的 `{args}` 请求 / `{ok,value}` 响应 → 实际为 ClientRequest/ServerResponse 四象限信封，方法名点分。
2. 无 `session/wait`：`RunTurn`（非流式）内部复用 WS 收集器（拨 mux → prompt → 收至 `turn/end`）。
3. mux 帧：非 `{sessionId, event}` 裸形，而是 ServerRequest 包 MuxFrame。
4. turn/end reason：`{kind}` 判别联合，含 `blocked`/`interrupted` 等扩展分支。
5. docker：`--host 0.0.0.0` 不可用，改为容器 IP 绑定 + trusted-host（或 socat）。

## 8. 结论

Go/No-Go：**Go**。所有接入事实均已从源码锁定，`DeepSeekClient` 可按第 1–3 节实现；部署按第 4–6 节。剩余运行时验证（真实 key 跑通一轮）留给 Task 12 E2E。
