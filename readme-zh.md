# oma-platform

> English: [README.md](./README.md)

可自托管的 **Open Managed Agents (OMA)** MVP：Go 平台运行时 + Python piPy 执行侧车（sidecar）。平台负责持久化、并发与 HTTP API；执行器负责 LLM 循环与工具调用。

## 系统特性

- **版本化 Agent** — 创建、更新、归档 Agent，并保留不可变版本快照（`/v1/agents`）。
- **持久化 Session** — 基于 SQLite 的追加式事件日志；创建 Session 时固定 Agent 版本与环境配置。
- **Harness 回合** — 用户消息触发无状态的 LLM 回合，经 piPy 侧车处理（`POST /internal/turn`）。
- **实时流式推送** — Server-Sent Events（`GET /v1/sessions/:id/events/stream`），支持可选回放。
- **回合中断** — `user.interrupt` 可取消正在执行的 harness 回合，并将 Session 置为 idle。
- **崩溃恢复** — 平台启动时将孤儿 `running` Session 重置为 `idle`。
- **按 Session 隔离沙箱** — 在 `SANDBOX_WORKDIR/<session_id>/` 下隔离工作目录；Go 负责创建路径，piPy 在其中执行工具。
- **运行环境（Environments）** — 带配置/元数据的命名执行上下文；启动时自动创建默认本地环境。
- **模型卡片（Model Cards）** — 租户级模型凭证与提供商配置；回合执行时按 Agent 引用的模型解析。
- **Agent 工具集** — OMA `agent_toolset_20260401` 映射到 piPy 内置工具：`bash`、`read`、`write`、`edit`、`glob`、`grep` 等。
- **Console 控制台** — 可选挂载 `open-managed-agents/apps/console` 的 SPA，与 API 同端口提供服务。
- **Docker Compose** — 双服务栈（`oma-platform` + `oma-harness`），含健康检查与卷挂载。
- **Fake Harness 模式** — `OMA_FAKE_HARNESS=1` 可在无 LLM API Key 时本地开发与 CI 运行。

## 系统架构

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         客户端（curl / Console / SDK）                   │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ HTTP + SSE
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-server（Go）                         :8787                          │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────┐  ┌───────────────┐  │
│  │  chi 路由   │  │ 认证(x-api-  │  │ Console SPA │  │ /health       │  │
│  │  /v1/*      │  │ key)         │  │ 静态资源    │  │               │  │
│  └──────┬──────┘  └──────────────┘  └─────────────┘  └───────────────┘  │
│         │                                                                 │
│  ┌──────▼──────────────────────────────────────────────────────────────┐  │
│  │  API 层（agents、sessions、environments、model_cards）              │  │
│  └──────┬──────────────────────────────────────────────────────────────┘  │
│         │                                                                 │
│  ┌──────▼──────────┐  ┌────────────────┐  ┌─────────────────────────┐  │
│  │ Session Registry │  │ Session Machine │  │ SSE Hub (stream.Hub)    │  │
│  │（按 Session 串行  │  │（回合生命周期、 │  │ 实时事件广播            │  │
│  │  排队执行回合）  │  │  中断处理）     │  │                         │  │
│  └──────┬──────────┘  └────────┬───────┘  └─────────────────────────┘  │
│         │                      │                                          │
│  ┌──────▼──────────────────────▼──────────────────────────────────────┐  │
│  │  SQLite 存储（modernc.org/sqlite）                                   │  │
│  │  agents · agent_versions · sessions · session_events                 │  │
│  │  environments · model_cards                                          │  │
│  └──────────────────────────────────────────────────────────────────────┘  │
│         │                      │                                          │
│  ┌──────▼──────────┐  ┌───────▼────────┐  ┌──────────────────────────┐  │
│  │ Workdir Manager │  │ Model Resolver │  │ Harness HTTP Client      │  │
│  │ 沙箱工作目录    │  │（模型卡片解析）│  │ POST /internal/turn      │  │
│  └─────────────────┘  └────────────────┘  └────────────┬─────────────┘  │
└────────────────────────────────────────────────────────┼────────────────┘
                                                         │ HTTP
                                                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-harness（Python / FastAPI）                      :8090              │
│  ┌──────────────────────────────────────────────────────────────────┐   │
│  │  oma_adapter                                                     │   │
│  │  · 将 OMA SessionEvent[] 投影为 piPy 消息                         │   │
│  │  · create_agent_session(in_memory=True, cwd=workdir)              │   │
│  │  · 执行一次无状态 prompt 回合                                     │   │
│  │  · 输出 OMA 形态事件（assistant、tool_use、tool_result 等）     │   │
│  └──────────────────────────────────────────────────────────────────┘   │
│  工具在 $SANDBOX_WORKDIR/<session_id>/ 内通过 piPy 内置能力执行         │
└─────────────────────────────────────────────────────────────────────────┘
```

### 组件职责说明

| 层级 | 组件 | 职责 |
|------|------|------|
| **平台（Go）** | `cmd/oma-server` | 进程入口；组装数据库、harness 客户端与 HTTP 服务。 |
| | `internal/api` | REST 路由、认证中间件、Console 开发态桩接口。 |
| | `internal/store` | SQLite 持久化、迁移、各资源 Repository。 |
| | `internal/session` | 回合状态机、按 Session 异步队列、中断处理。 |
| | `internal/stream` | 按 Session 的内存 SSE 发布/订阅。 |
| | `internal/workdir` | 创建并隔离每个 Session 的沙箱目录。 |
| | `internal/modelresolve` | 将 Agent 模型字符串解析为模型卡片凭证。 |
| | `internal/harness` | 调用 Python 侧车的 HTTP 客户端（开发态可用 `FakeClient`）。 |
| | `internal/console` | Console SPA 静态资源处理。 |
| **执行器（Python）** | `harness/oma_adapter` | 基于 piPy `create_agent_session` 的薄 FastAPI 适配层。 |
| | `turn.py` | 无状态回合：投影事件 → 执行 prompt → 输出 OMA 事件。 |
| | `tools.py` | 将 OMA 工具声明映射为 piPy 内置工具名。 |
| | `emit.py` / `project.py` | OMA 与 piPy 事件结构的相互转换。 |

### 一次用户回合的请求流程

1. 客户端 `POST /v1/sessions/:id/events`，提交 `user.message` 事件。
2. API 校验事件类型，写入 `session_events`，并在 Session Registry 中排队执行回合。
3. Session Machine 加载历史、确保沙箱目录、解析模型卡片，向 harness 发起 `POST /internal/turn`。
4. Harness 将持久化事件投影为 piPy 消息，以 `cwd=workdir` 创建内存 Agent Session，执行一次 prompt，返回新的 OMA 事件。
5. 平台持久化 harness 输出、更新 Session 状态，并向 SSE 订阅者广播每条事件。
6. 客户端可通过 `GET /v1/sessions/:id/events` 轮询，或 `GET /v1/sessions/:id/events/stream` 实时订阅。

Harness 侧回合是**无状态**的：每次调用都携带完整事件历史作为上下文。平台是持久化的唯一事实来源。

### 存储布局

| 路径 | 用途 |
|------|------|
| `DATABASE_PATH`（默认 `./data/oma.db`） | SQLite 数据库 |
| `SANDBOX_WORKDIR`（默认 `./data/sandboxes`） | 按 Session 隔离的工具执行目录 |

## API 概览

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/health` | 健康检查 |
| `POST` | `/v1/agents` | 创建 Agent |
| `GET` | `/v1/agents` | 列出 Agent |
| `GET` | `/v1/agents/:id` | 获取 Agent |
| `PATCH` | `/v1/agents/:id` | 更新 Agent（产生新版本） |
| `POST` | `/v1/agents/:id/archive` | 归档 Agent |
| `GET` | `/v1/agents/:id/versions` | 列出版本 |
| `POST` | `/v1/sessions` | 创建 Session |
| `GET` | `/v1/sessions` | 列出 Session |
| `GET` | `/v1/sessions/:id` | 获取 Session |
| `POST` | `/v1/sessions/:id/events` | 追加事件 / 触发回合 |
| `GET` | `/v1/sessions/:id/events` | 分页列出事件 |
| `GET` | `/v1/sessions/:id/events/stream` | SSE 事件流 |
| `POST` | `/v1/environments` | 创建环境 |
| `GET` | `/v1/environments` | 列出环境 |
| `POST` | `/v1/model_cards` | 创建模型卡片 |
| `GET` | `/v1/model_cards` | 列出模型卡片 |

除 `OMA_CONSOLE_DEV=1`（仅开发）外，受保护路由需请求头 `x-api-key: $OMA_API_KEY`。

## 本地快速开始

```bash
# 终端 1 — harness（假数据模式，无需 API Key）
cd harness
OMA_FAKE_HARNESS=1 uvicorn oma_adapter.main:app --port 8090

# 终端 2 — platform
export OMA_FAKE_HARNESS=1
export HARNESS_URL=http://127.0.0.1:8090
export OMA_API_KEY=dev-key
go run ./cmd/oma-server/
```

也可使用仓库根目录脚本：

```bash
./start-harness.sh    # 终端 1
./start-platform.sh   # 终端 2
```

## 冒烟测试

```bash
AID=$(curl -s -X POST localhost:8787/v1/agents \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d '{"name":"hello","model":"claude-sonnet-4-20250514","system_prompt":"You are helpful."}' \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')

SID=$(curl -s -X POST localhost:8787/v1/sessions \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d "{\"agent\":\"$AID\"}" \
  | python3 -c 'import sys,json; print(json.load(sys.stdin)["id"])')

curl -s -X POST localhost:8787/v1/sessions/$SID/events \
  -H "content-type: application/json" -H "x-api-key: $OMA_API_KEY" \
  -d '{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}'
```

完整脚本：`./smoke-test.sh`

## Console 控制台

来自 `open-managed-agents/apps/console` 的 OMA Console SPA 与 API 同端口提供服务（对齐 main-node 的 `CONSOLE_DIR` 模式）。使用工作区 Go 工具链 `.tools/go`：

```bash
# 终端 1 — harness
./start-harness.sh

# 终端 2 — platform + console（若缺少 dist 会自动构建）
./start-console.sh
```

浏览器打开 http://localhost:8787 — `OMA_CONSOLE_DEV=1` 会桩化 `/auth-info` 与 `/auth/get-session`，无需 better-auth 即可通过登录门。

**Docker：** `docker compose up` 在存在构建产物时，将 `../open-managed-agents/apps/console/dist` 挂载到 `/app/console`。需先构建 Console，或通过 `CONSOLE_DIST` 指定路径。

**范围：** Agents、sessions、environments、model cards 已对接 oma-platform API。Vault、skills、runtimes、integrations、evals 等 main-node 专属能力在 P2 阶段返回空列表桩（Console 可正常显示空态）；完整实现延后。

**生产环境：** `OMA_CONSOLE_DEV=1` 会关闭 API Key 校验，仅限开发。生产 Console 需 better-auth 或其他浏览器认证方案；在此之前请使用带 `x-api-key` 的 API 客户端。

## Docker

```bash
docker compose up --build
```

复制 `.env.example` 为 `.env`。真实模型调用请设置 `OMA_FAKE_HARNESS=0`，并通过 `~/.pi/agent/settings.json`、`models.json`、`auth.json` 配置 piPy（compose 会挂载到 harness 容器）。

## 配置项

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `OMA_LISTEN_ADDR` | `:8787` | 平台 HTTP 监听地址 |
| `OMA_API_KEY` | — | `x-api-key` 认证密钥 |
| `DATABASE_PATH` | `./data/oma.db` | SQLite 数据库路径 |
| `SANDBOX_WORKDIR` | `./data/sandboxes` | Session 沙箱根目录 |
| `HARNESS_URL` | `http://127.0.0.1:8090` | Harness 侧车基础 URL |
| `OMA_FAKE_HARNESS` | — | `1` = 进程内假 harness（无需 Python） |
| `HARNESS_HTTP_TIMEOUT_SEC` | `600` | 平台 → harness HTTP 超时（秒） |
| `CONSOLE_DIR` | — | 已构建 Console `dist/` 路径 |
| `OMA_CONSOLE_DEV` | — | `1` = 开发认证桩 + 放宽 API Key 规则 |

## 技术栈

- **平台：** Go 1.22+、chi、modernc.org/sqlite（纯 Go，无 CGO）
- **执行器：** Python 3.11+、FastAPI、piPy（`pi_coding_agent`）
- **部署：** 单个 Go 静态二进制 + Python 侧车；Docker Compose 用于本地/类生产运行

## MVP 范围外

Cloudflare Workers / SessionDO、多租户路由、Vault、技能市场、billing、集成（Linear/GitHub/Slack）以及 Postgres 存储均延后实现。当前 API 为完整 OMA 的子集，随功能迭代逐步对齐 SDK。
