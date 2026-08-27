# DeepSeek Harness 集成分析

> **分析日期**: 2026-08-24
> **分析范围**: `deepseek-harness-master` 对外 API 与 `meta-harness` 的集成方式

---

## 执行摘要

**核心架构**：`meta-harness` 与 `deepseek-harness-master` 的集成采用 **HTTP RPC + WebSocket** 架构。DeepSeek harness 作为独立服务运行（Node.js web gateway，默认 :3080），meta-harness 的 Go 客户端通过 HTTP/WebSocket 与之通信。

**重要发现**：`meta-harness/harness/` 目录（Python FastAPI sidecar）**不是** DeepSeek Harness，而是基于 piPy SDK 的默认循环适配器。DeepSeek 集成完全在 Go 层实现（`internal/harness/deepseek_client.go`）。

---

## 1. deepseek-harness-master 对外暴露的完整 API

### 1.1 项目概览

| 属性 | 值 |
|------|-----|
| 包名 | `@deepseek-ai/dsh-root` |
| 类型 | TypeScript monorepo (pnpm workspaces) |
| 架构 | Cordis DI 框架 + Typert RPC 网关 |
| 启动命令 | `pnpm dsh web` (默认 :3080) |
| 许可证 | MIT |

### 1.2 HTTP 服务器端点

| 路径 | 类型 | 用途 |
|------|------|------|
| `/api/<method>` | POST JSON | 主 RPC 通道（53 个方法） |
| `/api/events.mux` | WebSocket | 浏览器 mux-frame 事件流 |
| `/api/events.host` | WebSocket | 浏览器 host-frame 事件流 |
| `/plugins` | 静态文件 | 客户端插件 bundle |
| `/plugins/events` | SSE | HMR 事件流 (graph/rebuilt) |
| `/` | 回退 | 前端静态文件 (dist/index.html) |

**安全策略**：
- `/api` 受 `isTrustedApiRequest` DNS-rebinding 防护
- 特权方法（host/settings/credentials 等）仅限 loopback 访问

### 1.3 `/api/*` RPC 方法清单（53 个）

#### Session 命名空间（12 个）

| 方法 | 功能 | oma 使用 |
|------|------|----------|
| `session.create` | 创建会话 | ✅ 是 |
| `session.prompt` | 提交用户消息 | ✅ 是 |
| `session.list` | 列出会话 | ✅ E2E 测试 |
| `session.history` | 获取历史事件 | ❌ |
| `session.models` | 列出可用模型 | ❌ |
| `session.selectModel` | 切换模型 | ❌ |
| `session.rename` | 重命名会话 | ❌ |
| `session.fork` | 分叉会话 | ❌ |
| `session.cancel` | 取消操作 | ❌ |
| `session.search` | 搜索会话 | ❌ |
| `session.updateQueue` | 更新消息队列 | ❌ |
| `session.attachment` | 处理附件 | ❌ |

#### Subagent 命名空间（4 个）

| 方法 | 功能 |
|------|------|
| `subagent.list` | 列出子代理 |
| `subagent.history` | 获取子代理历史 |
| `subagent.prompt` | 向子代理提交提示 |
| `subagent.interrupt` | 中断子代理 |

#### Host 命名空间（5 个，仅 loopback）

| 方法 | 功能 | 特权 |
|------|------|------|
| `host.describe` | 描述宿主环境 | ⚠️ |
| `host.pickDirectory` | 打开目录选择器 | ⚠️ |
| `host.listDirectory` | 列出目录内容 | |
| `host.createDirectory` | 创建目录 | |
| `host.openPath` | 用系统程序打开路径 | ⚠️ |

#### Workspace 命名空间（7 个）

| 方法 | 功能 |
|------|------|
| `workspace.list` | 列出工作区 |
| `workspace.create` | 创建工作区 |
| `workspace.rename` | 重命名工作区 |
| `workspace.delete` | 删除工作区 |
| `workspace.insertBefore` | 在工作区中插入项 |
| `workspace.insertSessionBefore` | 插入会话到指定位置 |
| `workspace.archiveSession` | 归档会话 |

#### 其他命名空间

| 命名空间 | 方法数 | 示例 |
|----------|--------|------|
| `skill` | 1 | `skill.list` |
| `agentPreset` | 6 | `list`, `select`, `read`⚠️, `copy`⚠️, `openDocument`⚠️, `remove`⚠️ |
| `goal` | 6 | `create`, `edit`, `pause`, `resume`, `complete`, `clear` |
| `settings` | 5 | `describe`⚠️, `openDocument`⚠️, `update`⚠️, `replace`⚠️, `mutate`⚠️ |
| `credentials` | 3 | `describe`⚠️, `set`⚠️, `unset`⚠️ |
| `llm` | 3 | `providers`, `models`, `discoverModels`⚠️ |

### 1.4 Typert 网关动态 RPC

通过 `TypertRemoteService` 基类动态发布的方法：

| Service | 命名空间 | 方法 |
|---------|----------|------|
| `PluginInventoryGateway` | `pluginInventory` | `list()` |
| `MessageFeedbackService` | `messageFeedback` | `list()`, `put()`, `delete()` |
| `DynamicCordisRunnerService` | `dynamicCordisRunner` | `define()`, `undefine()`, `run()`, ... |
| `CommandRuntime` | `commands` | `register()`, `list()`, `find()`, `execute()` |
| `GoalService` | `goals` | `get()`, `disarm()`, `create()`, `edit()`, ... |

端点格式：`POST /api/{namespace}/{method}`

### 1.5 Node.js SDK（stdio JSON-RPC）

**服务端方法**（`packages/sdk/server/src/server.ts`）：

| JSON-RPC 方法 | 功能 |
|--------------|------|
| `initialize` | 握手：设置 cwd、provider、model、maxTokens |
| `session/prompt` | 向指定 sessionId 提交用户消息 |
| `shutdown` | 关闭服务器、释放所有代理 |

**服务端 → 客户端通知**：

| 通知类型 | 功能 |
|----------|------|
| `session.event` | 会话事件流 |
| `session.status` | 代理状态变化（idle/busy） |
| `subagent.started` | 子代理启动 |
| `subagent.finished` | 子代理完成 |

### 1.6 Python SDK

**高级 API**（`api.py`）：

```python
@dataclass
class DeepSeekHarnessConfig:
    provider: str = "deepseek-official"
    model: str = "deepseek-v4-flash"
    max_tokens, cwd, session_root, ...

class DeepSeekHarness:
    __init__(config=None, **kwargs)
    __enter__ / __exit__           # 上下文管理器
    start()                        # 启动子进程
    close()                        # 关闭
    start_session(session_id=None) -> Session
    run(input, *, session_id=None, on_notification=None) -> RunResult

class Session:
    run(input, *, on_notification=None) -> RunResult

@dataclass
class RunResult:
    session_id, final_response, finish_reason, events, notifications, session_root
```

**底层客户端**（`client.py`）：

```python
class HarnessClient:
    start(), close()
    initialize(*, cwd, provider, model, max_tokens=None)
    session_prompt(session_id, content_blocks, ...) -> message_id
    request(method, params, *, response_model=None, timeout_seconds=None)
    notify(method, params=None)
    next_notification() -> Notification
    subscribe_notifications(filter=None) -> NotificationSubscription
    subscribe_session_notifications(session_id) -> NotificationSubscription
    next_request() -> IncomingRequest
    respond(request_id, result)
    respond_error(request_id, code, message, data=None)
```

### 1.7 CLI 命令行

```bash
# 启动 Web UI (默认 :3080)
dsh web
# 或
dsh --profile web

# 无头运行任务
dsh --profile headless "task"

# 打印配置
dsh dump-config --profile tui

# 管理插件
dsh plugin --profile tui add <pkg>
```

---

## 2. meta-harness 中的 DeepSeek 集成架构

### 2.1 总体架构

```
┌─────────────────────────────────────────────────────────────┐
│ Client (Console / curl / SDK)                               │
└───────────────────────────────┬─────────────────────────────┘
                                │ HTTP + SSE
                                ▼
┌─────────────────────────────────────────────────────────────┐
│ oma-server (Go)                                 :8787        │
│                                                               │
│  Registry.ClientFor(agent) 分发:                               │
│  ├── KindDefaultLoop → HTTPClient (→ harness sidecar :8090)  │
│  ├── KindHermes      → HermesClient                            │
│  ├── KindOpenClaw    → OpenClawClient                          │
│  ├── KindDeepSeek    → DeepSeekClient   ◀── 本次重点          │
│  └── KindFake        → FakeClient                              │
└───────────────────────────────┬─────────────────────────────┘
                                │
        ┌───────────────────────┼───────────────────────┐
        │                       │                       │
        ▼                       ▼                       ▼
┌───────────────┐      ┌───────────────┐      ┌───────────────┐
│ harness/      │      │ dsh web       │      │ Hermes/       │
│ (Python/piPy) │      │ (Node.js)     │      │ OpenClaw      │
│ :8090         │      │ :3080         │      │ (managed)     │
└───────────────┘      └───────┬───────┘      └───────────────┘
                               │
                               │ POST /api/session.create
                               │ POST /api/session.prompt
                               │ WS /api/events.mux
                               ▼
                      deepseek-harness-master
```

### 2.2 DeepSeekClient 实现

**文件**：`internal/harness/deepseek_client.go`

**核心结构**：

```go
type DeepSeekClient struct {
    GatewayURL string        // OMA_DEEPSEEK_GATEWAY_URL, 默认 http://127.0.0.1:3080
    Token      string        // OMA_DEEPSEEK_TOKEN (可选 Bearer token)
    HTTP       *http.Client  // 默认 10 分钟超时
}
```

**实现接口**：

```go
// Client interface
func (c *DeepSeekClient) RunTurn(ctx context.Context, req TurnRequest) (TurnResponse, error)

// StreamingClient interface
func (c *DeepSeekClient) RunTurnStream(ctx context.Context, req TurnRequest, onEvent EventHandler) error
```

**调用流程**（`RunTurn`）：

1. 从 `req.Events` 提取最后一条用户消息（`extractLastUserMessage`）
2. `ensureSession()` → RPC `session.create`（忽略 `session-conflict` 错误）
3. RPC `session.prompt`，payload：`{ sessionId, mode: "queue", content: [{type:"text", text:userText}] }`
4. `collectTurn()`：拨 WS `/api/events.mux`，按 sessionId 过滤帧，直到 `turn/end`
5. 事件映射：dsh `session/event` → oma `agent.*` 事件

**调用流程**（`RunTurnStream`）：

前 3 步同上；第 4 步直接流式消费 WS，每收到一帧就调用 `onEvent`，不攒批。

### 2.3 dsh 事件 → oma 事件映射表

| dsh `session/event` type | oma 事件 | 备注 |
|---|---|---|
| `assistant/chunk` (with `chunk.type = text-delta`) | `agent.message` | 累积式文本，每次用累计字符串重发 |
| `assistant/message` | `agent.message` | 完整消息；同时提取 `usage.{input,output}Tokens` |
| `tool/call` | `agent.tool_use` | 取 `data.name` |
| `tool/result` | `agent.tool_result` | 根据 `error` 字段决定 content 是 `(completed)` 或 `(failed: ...)` |
| `turn/end` | — | 终止信号；`reason.kind == "error"` 时返回 error |

**过滤规则**：所有帧先按 `frame.SessionID == req.SessionID` 过滤，忽略其他 session 的事件。`stream/error` 直接返错。

---

## 3. 被 meta-harness 使用的 DeepSeek Harness API 清单

### 3.1 RPC 方法（上行 HTTP）

| dsh 方法 | HTTP 端点 | 用途 | 触发时机 |
|---|---|---|---|
| `session.create` | `POST /api/session.create` | 懒创建 dsh 会话 | 每次 turn 起始，`ensureSession()` |
| `session.prompt` | `POST /api/session.prompt` | 推送用户输入 | `session.create` 之后立即调用 |
| `session.list` | `POST /api/session.list` | E2E 脚本探测网关健康 | 仅 E2E 测试脚本使用 |

**请求 envelope 格式**：

```json
{
  "type": "client-request",
  "rpcId": "<random-uuid>",
  "method": "session.create | session.prompt",
  "payload": { ... }
}
```

**响应 envelope 格式**：

```json
{
  "type": "server-response",
  "rpcId": "<matching-uuid>",
  "result": {
    "ok": true,
    "value": { ... }
  }
}
```

> **注意**：HTTP 状态码是 carrier，业务错误在 `result.ok=false` 中。

### 3.2 WebSocket 端点（下行）

| 端点 | 用途 |
|---|---|
| `WS /api/events.mux` | 全局事件多路复用流；客户端按 `sessionId` 过滤 |

**WS 消息格式**（mux-frame）：

```json
{
  "type": "server-request",
  "rpcId": "...",
  "method": "",
  "payload": {
    "type": "session/event",
    "sessionId": "sess-1",
    "event": { "type": "assistant/chunk", "seq": N, "data": { ... } }
  }
}
```

### 3.3 消费的 dsh 事件类型

| 事件路径 | 用途 |
|---|---|
| `session/subscribed` | 忽略（仅日志） |
| `session/event` → `assistant/chunk` (text-delta) | 流式文本 |
| `session/event` → `assistant/message` (含 usage) | 完整消息 |
| `session/event` → `tool/call` | 工具调用 |
| `session/event` → `tool/result` | 工具结果 |
| `session/event` → `turn/end` | 终止信号 |
| `stream/error` | 错误中断 |

### 3.4 配置与环境变量

**`.env.example`** 中的 DeepSeek 配置：

```bash
# DeepSeek harness (dsh web gateway)
OMA_DEEPSEEK_GATEWAY_URL=http://127.0.0.1:3080
OMA_DEEPSEEK_ENABLED=1
# OMA_DEEPSEEK_TOKEN=  # 可选，dsh 默认无鉴权
```

**Go 侧配置加载**（`cmd/oma-server/main.go`）：

```go
deepseekCfg := harness.DeepSeekConfig{
    GatewayURL: os.Getenv("OMA_DEEPSEEK_GATEWAY_URL"),
    Token:      os.Getenv("OMA_DEEPSEEK_TOKEN"),
    Disabled:   envDisabled("OMA_DEEPSEEK_ENABLED"),
}
```

**客户端装配**：

```go
DeepSeek: harnessGatewayClient(deepseekCfg.Disabled, deepseekCfg.GatewayURL,
    func() harness.Client {
        return &harness.DeepSeekClient{
            GatewayURL: deepseekCfg.GatewayURL,
            Token:      deepseekCfg.Token,
        }
    }),
```

`harnessGatewayClient` 逻辑：当 `disabled || gatewayURL == ""` 时返回 `nil`，Registry 回退到 `ManagedClient` stub。

### 3.5 可用性端点

`GET /v1/config/harnesses` 返回：

```json
{ "openclaw": true, "hermes": false, "deepseek": true }
```

由 `harness.HarnessAvailability(openclawCfg, hermesCfg, deepseekCfg)` 计算：`!ds.Disabled && ds.GatewayURL != ""`。

### 3.6 前端消费

**Console 下拉**（`console/src/pages/agents/AgentFormDialog.tsx`）：

```tsx
const deepseek = true;  // "always enabled — first-class harness"
// 下拉选项
<SelectOption value="__managed_deepseek__">DeepSeek</SelectOption>
// 写入 payload
_oma.harness = "deepseek"
```

---

## 4. 不被 meta-harness 使用的 API

| 类别 | API | 不被使用的原因 |
|------|-----|--------------|
| Host 命名空间 | `host.*` (5 个) | 需要本地 GUI，仅 loopback 可用 |
| Settings | `settings.*` (5 个) | oma 有自己的配置系统 |
| Credentials | `credentials.*` (3 个) | oma 有自己的 Vault 系统 |
| Workspace | `workspace.*` (7 个) | oma 有独立的会话/目标管理 |
| Goals | `goal.*` (6 个) | oma 使用自己的 eval/dream 系统 |
| AgentPreset | `agentPreset.*` (部分) | oma 使用自己的 agent CRUD |
| Subagent | `subagent.*` (4 个) | oma 通过 harness 内部处理子代理 |
| Typert 网关 | `pluginInventory.*`, `commands.*` | oma 使用简化的 RPC 子集 |
| Node.js SDK | stdio JSON-RPC | oma 使用 HTTP/WS 协议 |
| Python SDK | `DeepSeekHarness`, `HarnessClient` | oma 使用 Go `net/http` + `gorilla/websocket` |
| CLI | `dsh` 命令 | oma 通过 HTTP 调用，不 spawn 子进程 |
| LLM | `llm.*` | oma 使用自己的 modelresolve |

---

## 5. 部署拓扑

### 5.1 Docker Compose

**`deploy/deepseek/docker-compose.yml`**：

```yaml
services:
  oma-deepseek:
    build:
      context: ../../../deepseek-harness-master
      dockerfile: deploy/Dockerfile.dsh
    command: ["pnpm", "dsh", "web",
              "--host", "0.0.0.0",
              "--trusted-host", "oma-deepseek"]
    ports: ["0.0.0.0:${DSH_HOST_PORT:-3080}:3080"]
    networks: [oma-network]
    healthcheck:
      test: ["CMD", "curl", "-sf", "http://127.0.0.1:3080/"]
```

### 5.2 Go 服务配置

Docker 容器内 Go 服务的配置：
```bash
OMA_DEEPSEEK_GATEWAY_URL=http://oma-deepseek:3080  # 容器 DNS
```

### 5.3 运维脚本

**`deploy/deepseek/start-deepseek.sh`**：

```bash
./start-deepseek.sh {start|stop|restart|status|logs|rebuild|clean}
```

健康检查最长等待 6 分钟。

---

## 6. 关键文件清单

### deepseek-harness-master

| 路径 | 作用 |
|------|------|
| `packages/host/webserver/src/index.ts` | HTTP 服务器入口 |
| `packages/host/apiproxy/src/fetch/handler.ts` | RPC 路由注册 (UNARY_ROUTES) |
| `packages/client/connection/src/index.ts` | RPC 传输桥 + WebSocket + PRIVILEGED_METHODS |
| `packages/sdk/server/src/server.ts` | Node.js SDK 服务端 |
| `packages/sdk/client/src/{api,client}.ts` | Node.js SDK 客户端 |
| `python/sdk/src/deepseek_harness/{api,client}.py` | Python SDK |
| `apps/cli/src/bin.ts` | CLI 入口 |
| `apps/cli/src/args.ts` | CLI 参数解析 |

### meta-harness

| 路径 | 作用 |
|------|------|
| `internal/harness/deepseek_client.go` | **唯一的 dsh 协议实现**（RPC + WS + 事件映射） |
| `internal/harness/deepseek_client_test.go` | httptest 模拟 dsh 网关的单测 |
| `internal/harness/registry.go` | Kind 常量、`normalizeKind`、`HarnessAvailability`、`DeepSeekConfig` |
| `internal/harness/client.go` | `Client` / `StreamingClient` / `TurnRequest` / `TurnResponse` 接口 |
| `cmd/oma-server/main.go` | 配置加载 + DeepSeekClient 构造 |
| `.env.example` | 配置文档 (OMA_DEEPSEEK_*) |
| `deploy/deepseek/docker-compose.yml` | dsh 容器定义 |
| `deploy/deepseek/start-deepseek.sh` | 运维脚本 |
| `console/src/pages/agents/AgentFormDialog.tsx` | Harness 下拉 + `_oma.harness: "deepseek"` 写入 |
| `scripts/e2e/smoke-deepseek-managed-e2e.sh` | 端到端冒烟测试 |
| `docs/superpowers/specs/2026-08-20-deepseek-harness-integration-design.md` | 设计规格文档 |

---

## 7. 架构设计要点

1. **扁平化架构**：DeepSeek 直接作为一级 Kind（`"deepseek"`），与 hermes/openclaw 同构
2. **协议隔离**：所有 dsh web BFF 协议知识集中在 `deepseek_client.go` 单文件
3. **版本锁定**：Docker 镜像固化 dsh 源码版本
4. **安全**：dsh 仅加入内部 `oma-network`，不强制鉴权但预留 `Token` 字段
5. **事件转译**：dsh 的 `session/event` 被转译为 oma 的 `agent.message/tool_use/tool_result`
6. **无 SDK 依赖**：直接使用 Go `net/http` + `gorilla/websocket`，不依赖 npm 或 Python SDK

---

## 8. 架构对比

| 维度 | deepseek-harness 原生 | meta-harness 集成方式 |
|------|---------------------|---------------------|
| **通信协议** | stdio JSON-RPC 或 HTTP/WS | HTTP/WS（仅 web gateway） |
| **会话管理** | dsh 内部状态 | oma Go 层持久化（MySQL） |
| **事件格式** | dsh `session/event` | oma `agent.*` 事件（转译） |
| **工具执行** | dsh 内置工具 | oma sandbox + harness sidecar |
| **认证** | 可选 Bearer Token | oma API Key / better-auth |
| **部署形态** | 独立 Node.js 进程 | Docker 容器（oma-network） |
| **SDK 使用** | npm 包或 Python SDK | 无（直接 HTTP/WS 调用） |

---

## 9. 总结

### meta-harness 使用 deepseek-harness 的方式可概括为：

1. **仅使用 web gateway**：通过 `POST /api/session.*` RPC + `WS /api/events.mux` 通信
2. **最小化集成**：只使用 `session.create`、`session.prompt`、`session.list` 三个 RPC 方法
3. **事件转译层**：dsh 的 `session/event` 被转译为 oma 的 `agent.*` 事件格式
4. **无 SDK 依赖**：不使用 npm 包或 Python SDK，直接用 Go `net/http` + `gorilla/websocket`
5. **网络隔离**：dsh 容器仅加入 `oma-network`，不直接暴露到公网

### 核心集成文件

- **唯一协议实现**：`internal/harness/deepseek_client.go`
- **注册表**：`internal/harness/registry.go`
- **装配点**：`cmd/oma-server/main.go`
- **前端**：`console/src/pages/agents/AgentFormDialog.tsx`
- **部署**：`deploy/deepseek/`
