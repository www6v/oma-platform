# OMA Platform 项目分析报告

> 分析日期：2026-07-27  
> 分析范围：meta-harness monorepo 全栈代码（Go 后端 / Console 前端 / Harness Python / Auth 认证 / 部署配置）

---

## 一、项目概述

### 1.1 项目定位

**OMA（Open Managed Agents）** 是一个可自托管的 AI Agent 管理平台，其设计哲学源自 Anthropic 发表的《Scaling Managed Agents: Decoupling the brain from the hands》一文中提出的**"元控制器（meta-harness）"**理念：

> 将"大脑"（LLM 推理循环）与"双手"（代码执行沙箱）彻底解耦，使每一层都可独立替换、独立故障、独立扩展。

### 1.2 核心目标

| 目标 | 说明 |
|------|------|
| **API 兼容** | 与 Anthropic Claude Managed Agents API 完全兼容（相同端点、相同事件类型），可与现有 SDK 无缝对接 |
| **可自托管** | 用户可在自己的基础设施上运行完整的 Agent 平台，不依赖 Anthropic 云服务 |
| **元控制器** | 对"未来模型需要什么具体控制器"不做假设，通过 Provider 注册表让每层都可替换 |
| **功能对齐** | 与 `open-managed-agents`（Cloudflare Workers 参考实现）达到 ~91% 的功能对齐度 |

### 1.3 代码规模

| 组件 | 文件数 | 代码行数（约） | 语言 |
|------|--------|----------------|------|
| Go 后端 | 390 个 `.go` | ~70,000 行 | Go 1.24 |
| Console 前端 | ~120 个 `.tsx`/`.ts` 源文件 | ~20,000+ 行 | TypeScript / React 19 |
| Harness Python | ~40 个模块 + ~40 个测试 | ~12,000 行 | Python 3.11+ |
| Auth 侧车 | 1 个 `server.mjs` | ~200 行 | Node.js 22 / ESM |
| **合计** | **~550+ 源文件** | **~102,000+ 行** | **4 种语言** |

### 1.4 当前成熟度

OMA 处于**"功能基本完整、生产硬化进行中"**的阶段：

- **P0 核心 Agent 闭环**：~93% 完成（Agent/Session/Events/SSE/HITL/Interrupt 全部可用）
- **P1 Console + 集成**：~88% 完成（全路由可用，Integrations Phase 1-2 已落地）
- **P2 平台 parity**：~88% 完成（Vault/Memory/Eval/Dream/Schedule/Teams 全部上线）
- **整体严格完成率**：~85%（44/52 功能域完全对齐）；含部分对齐 ~91%

---

## 二、整体架构

### 2.1 四进程微服务拓扑

```
┌─────────────────────────────────────────────────────────────────────┐
│                    客户端（Console SPA / curl / Python SDK）          │
└────────────────────────────────┬────────────────────────────────────┘
                                 │ HTTP + SSE (+ WS for workflows)
                                 ▼
┌─────────────────────────────────────────────────────────────────────┐
│  ① oma-server (Go / chi)                              :8787         │
│  核心平台：Agents · Sessions · Vaults · Skills · Files · Model Cards │
│  Environments · Integrations · Eval/Dream Workers · Memory Stores    │
│  Teams · Session Resources · Custom Tool HITL · MCP Proxy            │
└────────┬──────────────────────────────────────────────┬─────────────┘
         │ POST /internal/turn (NDJSON 流式)             │ /auth/* 代理
         ▼                                               ▼
┌─────────────────────────────────┐   ┌─────────────────────────────────┐
│  ② oma-harness (Python/FastAPI) │   │  ③ oma-auth (Node/better-auth)  │
│  "大脑"：无状态 LLM Turn         │   │  认证侧车：邮箱/密码 · OAuth     │
│  工具编排 · 上下文压缩            │   │  Cookie Session 管理             │
│  :8090                          │   │  :8788                          │
└─────────────────────────────────┘   └─────────────────────────────────┘

         ④ 存储层：MySQL (DATABASE_URL) + 本地文件系统 (/data)
```

### 2.2 技术选型与理由

| 层次 | 选型 | 理由 |
|------|------|------|
| **平台层** | Go 1.24 / chi / go-sql-driver/mysql | 高并发 goroutine 模型、单二进制部署、强类型保证接口安全 |
| **执行器** | Python 3.11 / FastAPI / piPy | 复用 piPy（pi_coding_agent）生态的 LLM 循环与工具框架 |
| **数据库** | MySQL（从 SQLite 迁移） | 生产级关系型数据库，支持并发读写，适合多租户场景 |
| **认证** | better-auth (Node) | 成熟的 Node 认证方案，支持邮箱/密码、OAuth、Cookie Session |
| **前端** | React 19 SPA（Console） | 与 API 同源部署（同端口 8787），无需 CORS 配置 |
| **SDK** | Python / anthropic SDK + httpx | 复用 anthropic SDK 的 base_url 机制兼容 Managed Agents API |
| **部署** | Docker Compose（三服务栈） | 本地开发和类生产部署统一；共享 /data 卷解决跨容器文件可见 |
| **沙箱** | 6 种 Provider 可插拔 | 开发用 local（零依赖），生产用 opensandbox/e2b 等容器隔离 |

### 2.3 核心设计原则

| 原则 | 实现方式 |
|------|----------|
| **平台是唯一事实来源** | 所有事件持久化在 MySQL `session_events`，Harness 无状态 |
| **流式推送** | Harness → Go 走 NDJSON 流式接口，Go 逐条写 DB + SSE 广播 |
| **串行 Session lane** | 同一 Session 同时只允许一个 active turn，队列串行消化 |
| **凭据不出沙箱** | Vault + MCP Proxy + Outbound Proxy 三层拦截，不靠约定靠架构 |
| **崩溃恢复** | 启动时扫描 `status=running` 孤儿 Session，批量重置为 interrupted |
| **接口驱动** | `sandbox.Provider`、`harness.Client`、`AgentTool` 都是接口，实现可替换 |

### 2.4 一次用户回合（Turn）的完整数据流

```
用户 POST /v1/sessions/:id/events (user.message)
    │
    ▼
① oma-server
    ├─ 校验事件类型 → 追加写入 MySQL session_events (append-only)
    ├─ 立即返回 202 Accepted（不阻塞）
    └─ Session Registry 异步入队 turn worker
         │
         ▼
    Session Machine
         ├─ 加载完整事件历史（getEvents，LIMIT 10000）
         ├─ 确保沙箱 workdir / sandbox provider
         ├─ 解析模型卡片凭证（modelresolve）
         ├─ 解析资源（ResourceResolver → Memory Store 挂载）
         ├─ 解析子 Agent（ResolveSubAgents）
         │
         ▼ POST /internal/turn/stream (NDJSON)
    ② oma-harness
         ├─ 投影事件 → piPy 消息格式
         ├─ 上下文压缩（token 超窗口 75% 时 aux_model 摘要）
         ├─ session.prompt() → LLM 推理循环
         │    ├─ 工具调用 → sandbox execute / MCP / web_fetch
         │    └─ 子 Agent 委派 → call_agent_*
         ├─ 逐条产出 NDJSON 事件流
         └─ 返回 OMA 事件
              │
              ▼
    ① oma-server（续）
         ├─ 逐条持久化 harness 输出事件到 MySQL
         ├─ 更新 Session 状态（running → idle）
         └─ SSE Hub 广播每条事件到订阅客户端
              │
              ▼
    客户端（SSE 实时接收 / 轮询 GET /events）
```

### 2.5 安全边界数据流

```
沙箱/Harness（bash curl、web_fetch）
    │ HTTP_PROXY=http://127.0.0.1:8790
    │ X-OMA-Session-Id: <session_id>
    ▼
Go Outbound Proxy (:8790)
    ├─ 校验 API Key → 解析 tenant + session
    ├─ 按 hostname 查 Vault credentials
    ├─ 注入 Authorization: Bearer <vault token>
    ▼
上游 HTTP API（Linear / GitHub / 自定义 MCP 等）
```

---

## 三、Go 后端服务

### 3.1 入口与启动流程

入口点 `cmd/oma-server/main.go`（429 行）采用**手动依赖注入**，在 `main()` 中组装所有组件：

```
main()
  ├── 环境变量解析 & 目录创建
  ├── store.Open() — MySQL 连接（MaxOpen=50, MaxIdle=10）
  ├── 所有 Repository 实例化 (agents, sessions, events, vaults, ...)
  ├── session.Registry — 内存 per-session 异步队列
  ├── stream.Hub — SSE 事件分发（内存级 pub/sub）
  ├── harness.Registry — 多 harness 后端注册
  ├── 后台 Worker 启动 (wakeup, eval, dream, memory retention)
  ├── api.NewRouter() — 路由组装
  ├── outbound proxy 启动 (:8790)
  └── http.ListenAndServe(:8787)
```

### 3.2 路由与中间件

**路由框架**：chi/v5，按资源类型组织 20+ 路由组。

| 路由组 | 覆盖资源 |
|--------|----------|
| `/v1/agents` | Agent CRUD + 版本历史 + 归档 |
| `/v1/sessions` | 创建/列表/事件/SSE 流/线程/轨迹/输出/资源挂载/团队协调/exec |
| `/v1/vaults` | 密钥管理 + OAuth 流程 |
| `/v1/skills` | 上传(zip) + ClawHub 市场导入 |
| `/v1/integrations` | Linear/GitHub/Slack webhook + OAuth |
| `/v1/environments` | 执行环境 + sandbox 配置 |
| `/v1/model_cards` | 模型凭证卡 |
| `/v1/memory_stores` | 持久化记忆 |
| `/v1/evals` | 评估运行 |
| `/v1/runtimes` | 运行时管理 |
| `/v1/internal/*` | harness 回调（model card 解析、vault 查询） |
| `/api/workflows/*` | 反向代理 → harness |

**中间件栈**：
1. **Auth 中间件**：支持 `x-api-key` / `Bearer` / better-auth cookie 三种认证方式，多租户支持
2. **Rate Limit 中间件**：内存滑动窗口限流，per-tenant 分桶

### 3.3 核心领域模型

| 模型 | 说明 | 关键特性 |
|------|------|----------|
| **Agent** | 版本化代理配置 | 含 model、tools、skills、MCP servers、harness 绑定；不可变版本快照 |
| **Session** | 会话实例 | 冻结 agent 快照，状态机 idle↔running↔interrupted |
| **Event** | Append-only 事件日志 | session 状态的唯一真相来源 |
| **Environment** | 执行环境 | 含 sandbox provider 配置 |
| **ModelCard** | 模型凭证卡 | per-tenant，支持多 LLM provider |
| **Vault/Credential** | 密钥保险库 | 支持 OAuth 刷新 |
| **Team/TeamTask** | 多 Agent 团队协作 | Mailbox 邮箱通信 + Poll Loop |
| **MemoryStore** | 持久化记忆 | 版本审计，大对象 blob offload，挂载到 sandbox |

### 3.4 数据库层

| 特性 | 详情 |
|------|------|
| **数据库** | MySQL（从 SQLite 迁移，代码中保留完整 SQLite 注释痕迹） |
| **驱动** | `go-sql-driver/mysql`，连接池 MaxOpen=50, MaxIdle=10 |
| **无 ORM** | 完全原生 SQL，每个 Repository 直接编写 SQL |
| **Schema** | 23 个 SQL 迁移文件，MySQL 版本依赖外部建表 |
| **Repository 模式** | 15+ 个 Repository，每个接收 `*sql.DB` |
| **多租户** | 所有数据行带 `tenant_id`，auth 中间件注入 context |

### 3.5 显著设计模式

- **手动 DI**：无框架，`main()` 组装 + `api.Deps` 容器
- **Per-session Lane**：每个 session 一个 goroutine worker，channel 串行化 turn
- **Event Sourcing**：session 状态由事件日志推导，harness turn 无状态
- **后台 Worker**：统一 ticker 模式（wakeup/eval/dream/memory retention）
- **分层终止保障**：L1（LLM↔工具循环）→ L2（Session Machine）→ L3（Registry Queue）→ L4（后台 Worker），每层都有明确的退出条件
- **Crash Recovery 双机制**：启动时 `RecoverRunning` + 运行时 `RecoverStuckRunningOnInterrupt`，覆盖所有僵尸 running 场景

### 3.6 外部服务集成

| 服务 | 说明 |
|------|------|
| **Harness Sidecar** (Python :8090) | LLM turn 执行，支持 HTTP/Fake/Hermes/OpenClaw 多种客户端 |
| **6 种 Sandbox** | local / e2b / daytona / litebox / boxrun / opensandbox |
| **Linear/GitHub/Slack** | OAuth + Webhook + 签名验证 |
| **Outbound Proxy** (:8790) | 自动注入 vault credentials 的 HTTP 正向代理 |
| **Auth Sidecar** (better-auth :8788) | Cookie session 认证，Go 通过 `AUTH_UPSTREAM_URL` 反向代理 |

---

## 四、Console 前端

### 4.1 技术栈

| 类别 | 技术选型 | 版本 |
|------|----------|------|
| **框架** | React | 19.1 |
| **语言** | TypeScript | 5.8 |
| **构建工具** | Vite | 6.3 |
| **路由** | React Router (data router) | 7.5 |
| **状态管理** | TanStack React Query | 5.100 |
| **UI 组件库** | shadcn/ui (radix-nova 风格) | 4.7 |
| **基础组件原语** | Radix UI | 1.4 |
| **CSS 框架** | Tailwind CSS | 4.1 |
| **认证** | better-auth | 1.6 |
| **表单** | react-hook-form + zod | 7.75 / 4.4 |
| **表格** | TanStack React Table | 8.21 |
| **动画** | Motion (Framer Motion) | 12.38 |
| **AI 流式渲染** | Vercel AI SDK + streamdown | 6.0 / 2.5 |
| **Markdown** | react-markdown + remark-gfm + remark-math + rehype-katex | — |
| **代码高亮** | Shiki + highlight.js + lowlight | — |
| **测试** | Vitest + Testing Library + MSW | 4.1 |
| **包管理** | pnpm workspace | — |

**本地子包（monorepo）**：
- `@meta-harness/api-types` — 共享 API 类型定义
- `@meta-harness/acp-known-agents` — 已知 Agent 注册表

### 4.2 目录结构

```
src/
├── main.tsx                    # 入口：路由定义 + Provider 装配（~200 行）
├── index.css                   # Tailwind v4 入口 + 设计 token（oklch 色彩系统）
├── components/
│   ├── ui/                     # shadcn 基础组件 (~28 个)
│   ├── ai-elements/            # AI 对话专用组件 (conversation, message, prompt-input, code-block, reasoning, tool...)
│   ├── timeline/               # 会话事件时间线组件
│   ├── AppShell.tsx            # 全局布局（Sidebar + Main）
│   ├── AppSidebar.tsx          # 侧边栏导航
│   ├── CommandPalette.tsx      # ⌘K 命令面板
│   ├── ListPage.tsx            # 通用列表页模板
│   ├── DataTable.tsx           # 高级数据表格（冻结表头 + 列可见性）
│   └── ...
├── pages/                      # 页面组件 (~20+ 页面)
│   ├── session-detail/         # SessionDetail 子模块拆分
│   └── agents/                 # Agent 相关子组件
├── lib/
│   ├── api.ts                  # 核心 API 层 (useApi hook)
│   ├── useApiQuery.ts          # TanStack Query 封装
│   ├── auth.tsx / auth-client.ts # 认证层
│   ├── sse.ts                  # 原生 SSE 流式客户端（~40 行）
│   ├── query-client.ts         # QueryClient 全局配置
│   ├── theme.ts                # 主题管理 (light/dark/system)
│   └── route-chords.ts         # 键盘快捷键映射
├── integrations/               # 第三方集成模块 (Linear / GitHub / Slack)
├── plugins/
│   ├── registry.ts             # 插件注册表
│   └── dynamic-workflows/      # 动态工作流插件
├── hooks/                      # 自定义 hooks
├── types/                      # 类型定义
├── data/                       # 静态数据 (MCP 注册表, 模板)
├── mocks/                      # MSW mock (测试用)
└── test/                       # 测试配置
```

### 4.3 路由架构

采用 **React Router v7 data router** 模式（`createBrowserRouter` + `RouterProvider`）：

```
/login              → 登录页（无 Shell）
/cli/login          → CLI 登录页（无 Shell）
/connect-runtime    → 运行时连接页（无 Shell）
/ (AppShell)        → 受保护路由的 Layout 包裹
  ├── /             → Dashboard
  ├── /agents /:id  → Agents 列表/详情
  ├── /sessions /:id→ Sessions 列表/详情（lazy-loaded, ~500kB gzipped）
  ├── /files        → 文件列表
  ├── /evals /:id   → 评估运行列表/详情
  ├── /environments /:id → 环境列表/详情
  ├── /vaults /:id  → 凭证保险库列表/详情
  ├── /memory /:id  → 记忆存储列表/详情
  ├── /skills       → 技能列表
  ├── /model-cards  → 模型卡片
  ├── /api-keys     → API 密钥
  ├── /runtimes     → 本地运行时
  ├── /integrations → Linear / GitHub / Slack 集成
  └── /workflows    → 动态工作流（插件注入）
```

**关键设计决策**：
- **SessionDetail 懒加载**：`lazy:` 字段做代码分割，保持首屏 bundle < 350kB
- **面包屑自动生成**：`useMatches()` + 每路由 `handle.crumb`，支持动态标签
- **插件路由注入**：`consolePlugins.flatMap(p => p.routes)` 扁平注入受保护路由组

### 4.4 状态管理模式

**没有 Redux/Zustand** — 全部通过 React 内置机制 + React Query 实现。

| 状态类型 | 方案 | 说明 |
|----------|------|------|
| **服务端状态** | TanStack React Query | `useApiQuery<T>` / `useInfiniteApiQuery<T>` / `useApiMutation`；QueryClient 配置 `staleTime: 30s`, `gcTime: 5min`, `retry: 1`, `keepPreviousData` |
| **认证状态** | Context + better-auth | `AuthProvider` 全局包裹，`useAuth()` 暴露 `{ user, isLoading, isAuthenticated }` |
| **多租户状态** | localStorage | `x-active-tenant` header 随每个请求发送；403 `not_a_member` 自愈：自动清除 + 页面重载 |

### 4.5 API 集成层

核心 `useApi()` hook：
- **`api<T>(path, init)`**：基于原生 fetch，自动 cookie 认证、tenant header、结构化 `ApiError`（status/code/requestId）、非 OK 自动 toast
- **`streamEvents(sessionId, onEvent, signal)`**：SSE 流式连接，指数退避重连（1s→2s→4s→8s），5 次失败 toast 提示
- **自研 SSE 客户端** (`lib/sse.ts`)：~40 行 SSE 帧解析器，基于 fetch + ReadableStream + TextDecoder

### 4.6 组件架构

**三层体系**：
1. **UI Primitives** (`components/ui/`) — 28 个 shadcn 基础组件
2. **Domain Components** — ListPage / DataTable / ai-elements / timeline / CommandPalette / Markdown
3. **Pages** (`pages/`) — 业务页面，组合下层组件

**列表页双模板**：
- `ListPage` — 轻量：简单表格 + 搜索 + 无限滚动
- `DataTable` — 高级：TanStack Table + 冻结表头 + 列可见性持久化 + 水平滚动同步

### 4.7 显著特性

| 特性 | 说明 |
|------|------|
| **Linear 风格键盘导航** | `g + letter` chord 快捷键 + ⌘K 命令面板 |
| **oklch 色彩系统** | 感知均匀的色彩空间 + 自定义动效 token |
| **多租户自愈** | stale tenant 自动检测 + 清除 + 重载 |
| **密码自动填充防御** | honeypot 隐藏输入框 |
| **插件系统** | `ConsolePlugin` 接口，OSS/hosted 差异化通过 build overlay |
| **bundled 字体** | Geist + JetBrains Mono，避免 FOUT |

---

## 五、Harness Python 服务

### 5.1 系统角色

Harness 是 OMA 平台的 **AI Agent 执行引擎 sidecar**，以 Python/FastAPI 运行在 Go 平台服务器旁（端口 8090）。核心职责：

- **桥接 OMA 平台与 piPy SDK**：Go 平台负责用户管理/会话持久化/前端通信；Harness 负责实际调用 LLM、执行工具、管理沙箱
- **无状态 Turn 执行**：Go 将完整事件历史 + Agent 快照 POST 给 Harness → Harness 创建 piPy Session → 执行一轮对话 → 返回结构化事件
- **流式 NDJSON 推送**：实时 thinking/message/tool_use 事件流
- **工作流编排后端**：为 `pi-dynamic-workflows` 提供 SubAgent 执行和 Bootstrap

### 5.2 技术栈

| 组件 | 详情 |
|------|------|
| Python | ≥ 3.11 |
| FastAPI | ≥ 0.115.0 |
| Uvicorn | ≥ 0.32.0 (standard) |
| httpx | ≥ 0.28.0（异步 HTTP） |
| markdownify | ≥ 0.4.0（HTML→MD） |
| MCP SDK | ≥ 1.27.2 |
| PyMySQL | ≥ 1.1.0 |
| 包管理 | uv + hatchling |
| 自研依赖 | pi-coding-agent, pi-ai, pi-subagent, pi-team, pi-dynamic-workflows, oma-sdk（均 Git 源） |

### 5.3 API 端点

| 端点 | 方法 | 用途 |
|------|------|------|
| `/health` | GET | 健康检查 |
| `/internal/turn` | POST | 同步 Turn 执行（接收 Agent 快照 + 事件历史 → 返回事件列表） |
| `/internal/turn/stream` | POST | 流式 NDJSON Turn（实时推送 + keepalive） |
| `/internal/evaluate-outcome` | POST | LLM-as-Judge 质量评估 |
| 工作流路由 | 多种 | 由 `pi_dynamic_workflows` 动态注册 |

### 5.4 AI/LLM 集成能力

| 能力 | 说明 |
|------|------|
| **多 Provider 映射** | `ant`→anthropic, `oai`→openai, `dashscope`→通义千问, 支持 `provider/model` 格式 |
| **动态凭证解析** | 通过平台 API `GET /v1/internal/model_cards/resolve` 获取 API Key |
| **上下文压缩** | token 超窗口 75% 时自动用 aux_model 生成摘要（支持增量摘要） |
| **Thinking 流式事件** | 完整映射 thinking_start/delta/end → OMA 前端事件 |
| **Web Fetch 多层降级** | 远程 MD 服务 → Jina Reader → httpx+markdownify → curl |
| **Web Search** | DuckDuckGo（三层降级）或 Tavily API |
| **LLM-as-Judge** | rubric 评估，JSON 判定 satisfied/needs_revision，带重试 |

### 5.5 工作流与沙箱执行

- **OmaWorkflowBootstrap**：通过 oma-sdk 创建 Worker/Coordinator Agent + Session，配置 SubAgentRuntime
- **OmaSubAgentRunner**：工作流步骤委派到独立 sub-thread，支持结构化输出 + schema 修复重试
- **沙箱路径规范化**：`/mnt/session/outputs/`、`/mnt/session/uploads/`、`/mnt/memory/` 等 AMA 路径
- **远程沙箱**：e2b/daytona 等通过平台 `POST /v1/sessions/:id/exec` 执行 bash
- **资源挂载**：文件(base64)、内存库、环境变量、输出目录符号链接

### 5.6 设计特点

- **Harness 自身无状态、不直接操作数据库**：所有持久化通过 Go 平台内部 API 完成
- **PyMySQL** 仅供 `pi-dynamic-workflows` 的 MySQL 后端使用
- **SQLite** 可选用于 Team/SubAgent 角色解析
- **Turn 级环境变量隔离**：每个 turn 独立设置/清理环境变量

---

## 六、Auth 认证服务

### 6.1 概述

| 属性 | 详情 |
|------|------|
| **位置** | `auth-sidecar/server.mjs`（单文件，~200 行） |
| **技术栈** | Node.js 22 + ESM，依赖仅两个：`better-auth` + `mysql2` |
| **监听地址** | 默认 `127.0.0.1:8788`，Docker 中覆盖为 `0.0.0.0:8788` |
| **角色** | Go 平台服务的认证代理/侧车，处理所有 `/auth/*` 路由 |

### 6.2 认证机制

- **核心框架**：[Better Auth](https://www.better-auth.com/) v1.6.3
- **认证方式**：
  - **邮箱+密码**：原生支持（`emailAndPassword: { enabled: true }`）
  - **Google OAuth**：可选，通过 `GOOGLE_CLIENT_ID` / `GOOGLE_CLIENT_SECRET` 环境变量启用
- **会话管理**：Better Auth 自动管理 session 表，基于 token 的会话机制
- **密钥**：通过 `BETTER_AUTH_SECRET` 环境变量配置；未设置时回退到 `randomBytes(32)` 生成临时密钥（重启后失效）

### 6.3 多租户自动创建

用户注册后，通过 `databaseHooks.user.create.after` 钩子自动调用 `ensureTenant()`：

1. 检查用户是否已有 membership（按 `created_at` 取最早的）
2. 若无，生成 `tn_<随机16字节hex>` 作为 tenant ID
3. 创建 tenant 记录（名称为 `"{用户名}'s workspace"`）
4. 创建 membership 记录（角色为 `owner`）
5. 使用 `INSERT IGNORE` 容忍并发竞争

### 6.4 标准端点

| 端点 | 用途 |
|------|------|
| `POST /auth/sign-up/email` | 邮箱注册 |
| `POST /auth/sign-in/email` | 邮箱登录 |
| `GET /auth/get-session` | 获取当前会话（也用于健康检查） |
| `POST /auth/sign-out` | 登出 |
| Google OAuth 相关端点 | 如启用 |

### 6.5 Origin 信任策略

- 自动信任 `PUBLIC_BASE_URL`、`http://127.0.0.1:8787`、`http://localhost:8787`
- 支持通过 `TRUSTED_ORIGINS` 环境变量添加额外逗号分隔的 origin
- 自动去除尾部斜杠，去重

### 6.6 数据库连接

统一使用 MySQL 连接池（`mysql2/promise`），`DATABASE_URL` 兼容三种格式：
- `mysql+aiomysql://user:pass@host:port/db`（Python 风格）
- `mysql://user:pass@host:port/db`（标准 URL）
- `user:pass@tcp(host:port)/db`（Go DSN 风格）

---

## 七、部署与运维

### 7.1 Docker 服务拓扑

| 服务 | 镜像 | 端口 | 技术栈 | 职责 |
|------|------|------|--------|------|
| `oma-auth` | `Dockerfile.auth` | 8788（内部，不暴露到宿主机） | Node.js 22 | 认证侧车 |
| `meta-harness` | `Dockerfile.platform` | **8787:8787**（暴露） | Go 1.24 + Console (React) | 主平台服务 |
| `oma-harness` | `Dockerfile.harness` | **8090:8090**（暴露） | Python 3.12 + FastAPI | Agent 执行引擎 |

### 7.2 依赖关系与启动顺序

```
meta-harness ──depends_on──► oma-auth (service_healthy)
                 ──depends_on──► oma-harness (service_healthy)
```

- `meta-harness` 等待 auth 和 harness 都健康后才启动
- `oma-auth` 和 `oma-harness` 无互相依赖，可并行启动

### 7.3 容器内网络通信

| 源 | 目标 | URL | 环境变量 |
|----|------|-----|----------|
| meta-harness | oma-auth | `http://oma-auth:8788` | `AUTH_UPSTREAM_URL` |
| meta-harness | oma-harness | `http://oma-harness:8090` | `HARNESS_URL` |
| oma-harness | meta-harness | `http://meta-harness:8787` | `OMA_PLATFORM_URL` |

### 7.4 共享存储

`../data:/data` — `meta-harness` 和 `oma-harness` 共享同一个数据卷：

| 路径 | 用途 |
|------|------|
| `/data/sandboxes` | 沙箱工作目录 |
| `/data/session-outputs` | 会话输出 |
| `/data/files` | 文件数据 |
| `/data/skills` | 技能数据 |
| `/data/memory` | 记忆数据 |

`oma-harness` 额外挂载 `${HOME}/.pi/agent:/root/.pi/agent:ro`（只读，pi agent 配置）。

### 7.5 健康检查

| 服务 | 检查方式 | 参数 |
|------|----------|------|
| oma-auth | `fetch('/auth/get-session')` — 状态码 < 500 即通过 | 5s 间隔，3s 超时，5 次重试，10s 启动期 |
| oma-harness | `urllib.request.urlopen('/health')` | 同上 |
| meta-harness | ⚠️ **无显式健康检查定义** | — |

### 7.6 构建流水线

#### Dockerfile.platform（三阶段构建）

1. **`console-build`**（node:22-bookworm）
   - `npm ci --ignore-scripts` → `npm run build`
2. **`build`**（golang:1.24-bookworm）
   - `go mod download` → `CGO_ENABLED=0 go build`（静态编译）
3. **运行阶段**（debian:bookworm-slim）
   - 仅安装 `ca-certificates`，复制 Go 二进制 + Console 构建产物

#### Dockerfile.auth（单阶段构建）

- node:22-bookworm-slim，`npm ci --omit=dev`，仅两个 npm 依赖，镜像极其精简

#### Dockerfile.harness（单阶段构建）

- python:3.12-slim，`uv sync --frozen --no-dev`，BuildKit 缓存挂载

### 7.7 中国网络适配

所有 Dockerfile 都支持通过构建参数切换中国镜像：

| 参数 | 中国镜像 |
|------|----------|
| `NPM_REGISTRY` | npmmirror.com |
| `GOPROXY` | goproxy.cn |
| `APT_MIRROR` | mirrors.aliyun.com |
| `PIP_INDEX_URL` / `UV_INDEX_URL` | 清华 PyPI 镜像 |

Harness Dockerfile 还用 `sed` 替换 `uv.lock` 中的 `files.pythonhosted.org` → 清华镜像。

### 7.8 脚本工具集

| 类别 | 脚本 | 功能 |
|------|------|------|
| **部署** | `deploy/docker.sh` | Docker Compose 统一入口：`up`/`down`/`build`/`restart`/`logs`/`ps`/`smoke` |
| **启动** | `scripts/start-auth-sidecar.sh` | 本地开发启动 auth sidecar，自动生成临时 secret |
| **启动** | `scripts/start-markdown-service.sh` | HTML→Markdown 转换服务（端口 8899） |
| **构建** | `scripts/build-console.sh` | 本地构建 Console 前端 |
| **迁移** | `scripts/migrate-auth-sqlite-to-mysql.py` | Auth + OMA SQLite → MySQL 迁移 |
| **迁移** | `scripts/migrate-workflows-sqlite-to-mysql.py` | 工作流 SQLite → MySQL 迁移 |
| **工具** | `scripts/go-env.sh` | 配置本地 Go 工具链路径 |
| **测试** | `scripts/e2e/` | 30+ 个 E2E 测试脚本，覆盖全链路 |

---

## 八、代码质量评估

### 8.1 各模块评分总览

| 维度 | Go 后端 | Console 前端 | Harness Python | Auth 侧车 |
|------|---------|-------------|----------------|-----------|
| **架构设计** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **代码组织** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ |
| **错误处理** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| **测试覆盖** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐ |
| **文档/注释** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |
| **生产就绪** | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | ⭐⭐⭐ |

### 8.2 Go 后端 — 详细评估

#### ✅ 优点

| 方面 | 说明 |
|------|------|
| **清晰三层分离** | api → session → store，职责边界明确 |
| **完善的测试覆盖** | 单元测试 + 集成测试 + cookbook E2E，测试金字塔完整 |
| **健壮的并发安全** | mutex/channel/context 三重保障，per-session lane 串行化 |
| **生产级特性** | 崩溃恢复、orphan 清理、rate limiting、SSE keepalive |
| **API 兼容性** | 完全兼容 Anthropic Managed Agents API |
| **接口驱动设计** | sandbox.Provider、harness.Client、AgentTool 都是接口，可独立替换 |

#### ⚠️ 改进空间

| 问题 | 影响 | 建议 |
|------|------|------|
| **SQLite→MySQL 迁移痕迹** | 大量注释代码残留，增加认知负担 | 一次性清理所有 SQLite 相关注释代码 |
| **`main.go` 过长（429 行）** | 启动逻辑集中，不利于维护 | 拆分为 `wire.go`（DI 组装）+ `server.go`（HTTP 启动）+ `workers.go`（后台任务） |
| **无结构化日志** | 排查问题困难 | 接入 `slog`（Go 1.21+ 标准库）或 `zerolog` |
| **事件历史全量加载** | `LIMIT 10000` 对长 session 有内存压力 | 实现分页加载或流式投影 |
| **Worker goroutine 无优雅关闭** | 进程退出时可能丢失正在执行的 turn | 添加 `context.Context` 传播 + `sync.WaitGroup` |
| **单进程架构** | Hub/Registry 在内存中，无法水平扩展 | 短期：文档说明单节点限制；长期：引入 Redis/NATS 做分布式 Registry |

### 8.3 Console 前端 — 详细评估

#### ✅ 优点

| 方面 | 说明 |
|------|------|
| **注释质量极高** | "why-first" 风格，解释每个设计决策的原因 |
| **渐进式复杂度** | API 层仅 3 个核心 hook，表面积极小 |
| **错误处理全面** | 网络/API/SSE 错误均有用户反馈 + 开发者日志 |
| **性能意识强** | 引用稳定性、代码分割、keepPreviousData、预加载 |
| **设计系统一致** | oklch + 动效 token + bundled 字体 |

#### ⚠️ 改进空间

| 问题 | 影响 | 建议 |
|------|------|------|
| **SessionDetail 过大（2134 行）** | 单文件职责过重，维护困难 | 进一步拆分为独立子组件（EventsPanel / ToolsPanel / ResourcesPanel） |
| **main.tsx 路由集中（~200 行）** | 路由定义分散困难 | 按模块拆分路由文件（agents.routes.ts / sessions.routes.ts 等） |
| **测试覆盖率 ~14%** | 17 测试文件 / ~120 源文件，核心页面缺少组件测试 | 优先为 SessionDetail / AgentDetail / API 层添加测试 |
| **AppShell 中 `void` 语句** | 可能存在过度导入 | 检查 tree-shaking 效果，移除无用导入 |

### 8.4 Harness Python — 详细评估

#### ✅ 优点

| 方面 | 说明 |
|------|------|
| **架构分层清晰** | 40+ 模块文件，职责划分合理 |
| **防御性编程** | 多层降级策略（web_fetch 4 层、web_search 3 层） |
| **流式支持完善** | keepalive + 错误兜底 + NDJSON 帧协议 |
| **测试覆盖广泛** | 40+ 测试文件，覆盖核心逻辑 |
| **沙箱路径安全检查** | 防止路径穿越 |
| **Turn 级环境变量隔离** | 每个 turn 独立设置/清理 |

#### ⚠️ 改进空间

| 问题 | 影响 | 建议 |
|------|------|------|
| **残留 `print(stderr)` 调试日志** | 生产环境日志噪声 | 替换为 `logging` 模块的结构化日志 |
| **`_run_turn_core` 约 250 行** | 职责过重，难以测试 | 拆分为 `prepare_context` / `execute_turn` / `collect_results` |
| **全局 `_runtime` 变量** | 并发不安全 | 使用依赖注入或 `ContextVar` 替代 |
| **大量 `Any` 类型注解** | 降低静态分析效果 | 逐步添加具体类型，配合 `mypy --strict` |
| **宽泛的 `except Exception`** | 可能掩盖真实错误 | 缩小异常捕获范围，仅捕获已知可恢复异常 |
| **Git branch 级别锁定依赖** | 构建可复现性不足 | 锁定到具体 commit hash 或发布 tag |

### 8.5 Auth 侧车 — 详细评估

#### ✅ 优点

| 方面 | 说明 |
|------|------|
| **极简设计** | 单文件 ~200 行，仅 2 个 npm 依赖 |
| **多租户自动化** | 注册即创建 tenant + membership，零手动操作 |
| **DATABASE_URL 兼容性强** | 支持 Python/标准/Go 三种格式 |
| **Origin 信任策略灵活** | 支持环境变量扩展 |

#### ⚠️ 改进空间

| 问题 | 影响 | 建议 |
|------|------|------|
| **`BETTER_AUTH_SECRET` 未设置时回退临时密钥** | 重启后所有会话失效 | 启动时检测未设置则报错退出，强制配置 |
| **无速率限制** | 暴力破解风险 | 添加登录尝试限制（如 5 次/分钟） |
| **无邮箱验证** | 垃圾注册风险 | 启用 better-auth 的邮箱验证插件 |

---

## 九、总结与建议

### 9.1 核心优势

1. **"不做假设"的元控制器哲学**：每层都是接口 + Provider 注册表，可独立替换。这是 OMA 最根本的架构优势，使其能够适应未来 LLM 生态的快速变化。

2. **无状态可恢复的 Harness**：每次 turn 携带完整事件历史，Harness 崩溃后新进程可从 DB 恢复。这种设计天然支持水平扩展和故障恢复。

3. **结构性安全边界**：Vault + MCP Proxy + Outbound Proxy 三层凭据拦截，不靠约定靠架构。沙箱内的 HTTP 请求必须经过 Outbound Proxy，凭据注入在代理层完成，Agent 代码永远接触不到原始密钥。

4. **流式 Turn + NDJSON**：从"做完再推"到"边跑边推"的改造，显著改善用户体验。SSE 实时广播 + 历史回放 + 15s keepalive 心跳，保证了长 turn 期间的流畅体验。

5. **完善的测试金字塔**：单元测试 → 集成测试 → E2E 冒烟 → Console Playwright QA，层次分明。30+ 个 E2E 测试脚本覆盖认证、MCP、沙箱、调度、webhook、多 agent、工作流等全链路。

6. **开发体验优良**：Fake Harness 模式支持无 API Key 开发、Docker Compose 一键启动、中国镜像适配、丰富的冒烟测试。

### 9.2 关键风险

| 风险 | 等级 | 说明 |
|------|------|------|
| **单节点存储瓶颈** | 🔴 高 | MySQL + 本地文件系统的组合适合开发和小规模部署，但缺乏多副本一致性方案。Memory Store 明确标注"单节点存储"限制。 |
| **SSE Hub 内存压力** | 🟡 中 | 内存级 SSE pub/sub 在大量并发 Session 时可能成为内存瓶颈。缺乏持久化消息队列的缓冲能力。 |
| **Harness 单点** | 🟡 中 | 虽然设计为无状态可恢复，但当前部署模式是单一 Python 进程。高并发下需要多实例 + 负载均衡。 |
| **HTTPS CONNECT 未实现** | 🟡 中 | Vault Outbound Proxy 不支持 HTTPS 隧道，沙箱内的 HTTPS 请求无法注入凭据。 |
| **硬编码默认凭据** | 🔴 高 | `DATABASE_URL` 默认值包含硬编码凭据，生产环境必须通过 `.env` 覆盖。 |
| **meta-harness 缺少健康检查** | 🟡 中 | 作为核心服务却没有定义 Docker `healthcheck`。 |

### 9.3 功能缺口

| 缺口 | 影响 | 优先级 | 建议 |
|------|------|--------|------|
| **Browser Tools** | Agent 无法操作浏览器，限制 Web 自动化 | 高 | 明确 roadmap 时间线 |
| **TypeScript SDK / `oma` CLI** | 前端开发者和运维人员缺少原生工具 | 高 | 优先发布，扩大社区 |
| **嵌套 Sub-agent** | 子 Agent 不能再委派，限制复杂任务分解 | 中 | ContextVar 已预留，尽快放开 |
| **回合写回 Memory Store** | Agent 在沙箱内修改挂载文件不会同步回 Store | 中 | 需要 FUSE 或写回队列 |
| **Memory Store 搜索** | 大批量 store 缺乏全文检索 | 中 | 接入向量搜索或 prefix 索引 |
| **Python SDK 未发布 PyPI** | 用户需本地安装 | 低 | 发布到 PyPI，低成本高收益 |

### 9.4 优先改进建议

#### 短期（1-2 周）

| # | 改进项 | 模块 | 预期收益 |
|---|--------|------|----------|
| 1 | **清理 SQLite 迁移残留** | Go 后端 | 减少 ~2000 行注释代码，降低认知负担 |
| 2 | **为 meta-harness 添加 Docker healthcheck** | 部署 | 提升部署可靠性 |
| 3 | **移除 `DATABASE_URL` 默认硬编码凭据** | 部署/安全 | 消除安全隐患 |
| 4 | **`BETTER_AUTH_SECRET` 未设置时强制报错** | Auth | 避免会话失效事故 |
| 5 | **替换 Harness 中的 `print(stderr)` 为结构化日志** | Harness | 提升生产可观测性 |

#### 中期（1-2 月）

| # | 改进项 | 模块 | 预期收益 |
|---|--------|------|----------|
| 6 | **拆分 `main.go`** | Go 后端 | 提升可维护性 |
| 7 | **接入结构化日志（slog/zerolog）** | Go 后端 | 提升排查效率 |
| 8 | **拆分 SessionDetail 组件** | Console | 降低单文件复杂度 |
| 9 | **拆分 `_run_turn_core`** | Harness | 提升可测试性 |
| 10 | **提升 Console 测试覆盖率到 40%+** | Console | 降低回归风险 |
| 11 | **Harness 依赖锁定到 commit hash** | Harness | 提升构建可复现性 |
| 12 | **添加 OpenTelemetry 集成** | 全栈 | 统一可观测性 |

#### 长期（3-6 月）

| # | 改进项 | 模块 | 预期收益 |
|---|--------|------|----------|
| 13 | **引入 Redis/NATS 做分布式 Registry** | Go 后端 | 支持水平扩展 |
| 14 | **Harness 多实例 + 负载均衡** | 部署/Harness | 消除单点瓶颈 |
| 15 | **实现 HTTPS CONNECT 代理** | Go 后端 | 完善凭据安全边界 |
| 16 | **Memory Store 接入向量搜索** | Go 后端/Harness | 支持语义检索 |
| 17 | **发布 TypeScript SDK + `oma` CLI** | 新模块 | 扩大生态 |
| 18 | **对象存储（S3/R2）替代本地文件系统** | 部署 | 支持多副本部署 |

### 9.5 最终评价

OMA Platform 是一个**架构设计精良、功能基本完整**的自托管 AI Agent 平台。它忠实地复现了 Anthropic Managed Agents 的"大脑与双手解耦"理念，并在其基础上扩展了 Agent Teams、Outcome Evaluator、Dynamic Workflows 等能力。

**~102,000 行代码**横跨 4 种语言，展现了工程团队在**接口驱动设计、流式架构、安全边界隔离**方面的深厚功力。代码注释质量（尤其是前端"why-first"风格）和测试覆盖度（Go 后端 + Harness 40+ 测试文件）都处于较高水平。

**核心挑战**在于从"功能完整"到"生产硬化"的最后一公里：多副本一致性、HTTPS 凭据注入、可观测性标准化、以及 TypeScript 生态支持。这些是决定 OMA 能否从"开发者工具"走向"企业级平台"的关键因素。

**适用场景**：适合需要自托管 Agent 平台、不依赖 Anthropic 云服务、且有工程能力运维 Go + Python 多服务栈的团队。当前 ~91% 的功能对齐度已能满足日常 Agent 对话、多 Agent 协作、工具集成等核心需求。
