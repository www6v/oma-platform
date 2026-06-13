# oma-platform

> English: [README.md](./README.md)

可自托管的 **Open Managed Agents (OMA)** 栈：Go 平台运行时 + Python piPy 执行侧车（sidecar）。平台负责持久化、并发与 HTTP API；执行器负责 LLM 循环与工具调用。

本仓库实现了 [open-managed-agents](https://github.com/open-ma/open-managed-agents) 协议在自托管场景下的大部分能力。功能对齐矩阵见 [MVP-MIGRATION-PLAN.md](./MVP-MIGRATION-PLAN.md)。

## 系统特性

### 核心 Agent 闭环

- **版本化 Agent** — 创建、更新、归档，并保留不可变版本快照（`/v1/agents`）。
- **持久化 Session** — 基于 SQLite 的追加式事件日志；创建 Session 时固定 Agent 版本与环境配置。
- **Harness 回合** — 用户消息触发无状态 LLM 回合，经 piPy 侧车处理（`POST /internal/turn`）。
- **实时流式推送** — Server-Sent Events（`GET /v1/sessions/:id/events/stream`），支持可选回放。
- **回合中断** — `user.interrupt` 可取消正在执行的 harness 回合，并将 Session 置为 idle。
- **崩溃恢复** — 平台启动时将孤儿 `running` Session 重置为 `idle`。
- **按 Session 隔离沙箱** — 在 `SANDBOX_WORKDIR/<session_id>/` 下隔离工作目录。
- **Agent 工具集** — OMA `agent_toolset_20260401` 映射到 piPy 内置工具及 `web_fetch`。
- **子 Agent** — `call_agent_*` 与 `general_subagent` 委派，配合 Session 线程（[设计文档](./docs/design/subagent.md)）。
- **上下文压缩** — 长回合前的事件摘要（`harness/oma_adapter/compaction.py`）。
- **MCP 工具** — Agent 声明的 MCP 服务，经 harness loader + `/v1/mcp-proxy` 挂载。

### 平台 API（对齐 Console）

- **运行环境** — 带配置/元数据的命名执行上下文；启动时自动创建默认环境 `env-local-default`。
- **模型卡片** — 租户级模型凭证；回合执行时解析；harness 通过 internal 端点获取密钥。
- **Skills** — 内置目录 + 自定义技能，支持 zip/文件上传（`/v1/skills`）。
- **Files** — 按 Session 作用域上传/下载文件（`/v1/files`）。
- **Vaults 与凭据** — 密钥存储与 OAuth 刷新；outbound HTTP 代理注入凭据。
- **Session 辅助接口** — 线程（从事件派生）、待确认工具、轨迹导出、输出文件。
- **统计与身份** — `/v1/stats`、`/v1/me`、`/v1/api_keys`。
- **Integrations** — Linear、GitHub、Slack 的 publication、OAuth 与 webhook 分发。
- **Eval runs** — CRUD + 后台 worker（`internal/eval/worker.go`）。
- **Runtimes** — ACP daemon connect/exchange，供本地 IDE 挂载（[设计文档](./docs/design/runtime-architecture.md)）。
- **Memory stores** — 大对象存储 + retention worker。

### 运维与开发

- **Console 控制台** — 本仓库 `console/` 下的 SPA，与 API 同端口。
- **认证** — API Key（`x-api-key` / `Authorization: Bearer`）或 better-auth Cookie 会话。
- **Docker Compose** — 双服务栈（`oma-platform` + `oma-harness`），含健康检查。
- **Fake Harness 模式** — `OMA_FAKE_HARNESS=1` 可在无 LLM API Key 时本地开发与 CI 运行。
- **冒烟与集成脚本** — `smoke-test.sh`、`scripts/console-integration.sh`、各 provider webhook、MCP、runtime、子 Agent E2E。

## 系统架构

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    客户端（Console / curl / SDK）                        │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ HTTP + SSE
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-server（Go）                                           :8787        │
│  agents · sessions · vaults · skills · files · model_cards              │
│  integrations · eval worker · runtimes · memory_stores                  │
│  mcp-proxy · outbound-proxy · internal API · Console SPA                │
│  session.Registry + stream.Hub（SSE）                                   │
├─────────────────────────────────────────────────────────────────────────┤
│  存储：SQLite（oma.db）+ 本地文件系统                                    │
│    sandboxes/ · skills/ · files/ · memory/ · session-outputs/           │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ POST /internal/turn
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-harness（Python / FastAPI）                            :8090        │
│  turn · tools · compaction · web_fetch · mcp_loader · call_agent        │
│  工具在 $SANDBOX_WORKDIR/<session_id>/ 内执行                           │
└─────────────────────────────────────────────────────────────────────────┘
```

### 组件职责说明

| 层级 | 组件 | 职责 |
|------|------|------|
| **平台（Go）** | `cmd/oma-server` | 进程入口；组装数据库、worker、harness 客户端与 HTTP 服务。 |
| | `internal/api` | REST 路由、认证、集成、Console 桩接口。 |
| | `internal/store` | SQLite 持久化、迁移、各资源 Repository。 |
| | `internal/session` | 回合状态机、按 Session 异步队列、中断处理。 |
| | `internal/stream` | 按 Session 的内存 SSE 发布/订阅。 |
| | `internal/workdir` | 创建并隔离每个 Session 的沙箱目录。 |
| | `internal/modelresolve` | 将 Agent 模型字符串解析为模型卡片凭证。 |
| | `internal/harness` | 调用 Python 侧车的 HTTP 客户端（开发态可用 `FakeClient`）。 |
| | `internal/outbound` | Vault 凭据注入（沙箱 HTTP 出站代理）。 |
| | `internal/eval` | Eval run 后台 worker。 |
| | `internal/memory` | Memory retention 定时任务。 |
| | `internal/runtime` | ACP daemon 的 runtime room 注册表。 |
| | `internal/integrations/*` | Linear、GitHub、Slack gateway 处理器。 |
| **执行器（Python）** | `harness/oma_adapter` | 基于 piPy `create_agent_session` 的 FastAPI 适配层。 |
| | `turn.py` | 无状态回合：投影事件 → 执行 prompt → 输出 OMA 事件。 |
| | `tools.py` | 将 OMA 工具声明映射为 piPy 内置/扩展工具名。 |
| | `compaction.py` | 回合前上下文压缩。 |
| | `call_agent/` | 子 Agent 委派运行时。 |
| | `extensions/` | `web_fetch`、`mcp_loader`、`call_agent` 等 piPy 扩展。 |

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
| `SKILLS_DATA_DIR`（默认 `./data/skills`） | Skill 文件存储 |
| `FILES_DATA_DIR`（默认 `./data/files`） | 上传文件 blob |
| `MEMORY_DATA_DIR`（默认 `./data/memory`） | Memory 大对象存储 |
| `SESSION_OUTPUTS_DIR`（默认 `./data/session-outputs`） | Session 输出产物 |
| `AUTH_DATABASE_PATH`（默认 `./data/auth.db`） | better-auth SQLite 库 |

## API 概览

### 核心

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
| `GET` | `/v1/sessions/:id/threads` | Session 线程（子 Agent） |
| `GET` | `/v1/sessions/:id/pending` | 待确认工具 |
| `GET` | `/v1/sessions/:id/trajectory` | 轨迹导出 |
| `GET` | `/v1/sessions/:id/outputs` | Session 输出文件 |

### 平台资源

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` / `GET` | `/v1/environments` | 运行环境 |
| `POST` / `GET` | `/v1/model_cards` | 模型卡片 |
| `GET` | `/v1/models/list` | 提供商模型列表 |
| `POST` / `GET` | `/v1/skills` | Skills |
| `POST` / `GET` | `/v1/files` | 文件 blob |
| `POST` / `GET` | `/v1/vaults` | Vault 与凭据 |
| `GET` | `/v1/stats` | 租户统计 |
| `GET` | `/v1/me` | 当前用户 / 租户 |
| `POST` / `GET` | `/v1/api_keys` | API Key |
| `POST` / `GET` | `/v1/runtimes` | Runtimes |
| `POST` / `GET` | `/v1/memory_stores` | Memory stores |
| `POST` / `GET` | `/v1/evals/runs` | Eval runs |
| `GET` | `/v1/integrations/*` | 集成 publication |

受保护路由支持请求头 `x-api-key: $OMA_API_KEY`、`Authorization: Bearer $OMA_API_KEY` 或 better-auth Cookie 会话。仅本地开发可设 `AUTH_DISABLED=1`（勿用于生产）。

## 本地快速开始

前置条件：工作区 Go 工具链位于 `../.tools/go`（辅助脚本会自动使用）；Harness 需要 Python 3.11+ 与 [uv](https://docs.astral.sh/uv/)。

```bash
cp .env.example .env

# 终端 1 — harness（假数据模式，无需 API Key）
./start-harness.sh

# 终端 2 — platform（仅 API）
source scripts/go-env.sh
export OMA_FAKE_HARNESS=1
export HARNESS_URL=http://127.0.0.1:8090
export OMA_API_KEY=dev-key
go run ./cmd/oma-server/
```

需要 Console + auth 侧车：

```bash
# 终端 1
./start-harness.sh

# 终端 2 — 若缺少 dist 会自动构建 Console，并启动 auth 侧车
./start-console.sh
```

浏览器打开 http://localhost:8787

## 验收测试

### 冒烟测试（完整 P1+P2 API + 可选真实 LLM）

```bash
# 需 platform + harness 运行（真实 LLM 时设 OMA_FAKE_HARNESS=0）
./smoke-test.sh

# 仅 API，无需 harness / LLM
SMOKE_SKIP_LLM=1 ./smoke-test.sh
```

在 `.env` 中设置 `ANTHROPIC_API_KEY`，或通过 `~/.pi/agent/{settings,models,auth}.json` 配置 piPy，即可进行真实模型调用。

### 其他脚本

| 脚本 | 用途 |
|------|------|
| `scripts/console-integration.sh` | Console 线型契约集成测试 |
| `scripts/smoke-mcp-e2e.sh` | MCP proxy + harness MCP loader |
| `scripts/smoke-subagent-e2e.sh` | 子 Agent 委派 E2E |
| `scripts/smoke-runtime-e2e.sh` | Runtime / ACP daemon |
| `scripts/smoke-linear-webhook.sh` | Linear webhook 分发 |
| `scripts/smoke-github-webhook.sh` | GitHub webhook 分发 |
| `scripts/smoke-slack-webhook.sh` | Slack webhook 分发 |

## Console 控制台

当设置 `CONSOLE_DIR` 时，本仓库 `console/` 下的 OMA Console SPA 与 API 同端口提供服务。`./start-console.sh` 会在缺少 `console/dist/` 时自动构建，启动 better-auth 侧车并代理 `/auth/*`，支持邮箱密码注册登录。

**Docker：** `docker compose up` 在存在构建产物时，可将 `./console/dist` 挂载到 `/app/console`。需先运行 `./scripts/build-console.sh`，或在 compose 中设置 `CONSOLE_DIST`。

**覆盖范围：** Agents、sessions、environments、model cards、skills、vaults、files、integrations、evals、runtimes、memory stores 已对接 oma-platform API。Dreams、cost report、browser tools 及部分 CF 专属能力仍延后 — 详见 [MVP-MIGRATION-PLAN.md](./MVP-MIGRATION-PLAN.md)。

## Docker

```bash
docker compose up --build
```

复制 `.env.example` 为 `.env`。真实模型调用请设置 `OMA_FAKE_HARNESS=0`，并通过 `~/.pi/agent/settings.json`、`models.json`、`auth.json` 配置 piPy（compose 会挂载到 harness 容器）。

## 配置项

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `OMA_LISTEN_ADDR` | `:8787` | 平台 HTTP 监听地址 |
| `OMA_API_KEY` | — | `x-api-key` / Bearer 认证密钥 |
| `DATABASE_PATH` | `./data/oma.db` | SQLite 数据库路径 |
| `SANDBOX_WORKDIR` | `./data/sandboxes` | Session 沙箱根目录 |
| `SKILLS_DATA_DIR` | `./data/skills` | Skill 文件存储 |
| `FILES_DATA_DIR` | `./data/files` | 文件 blob 存储 |
| `MEMORY_DATA_DIR` | `./data/memory` | Memory 大对象存储 |
| `SESSION_OUTPUTS_DIR` | `./data/session-outputs` | Session 输出产物 |
| `HARNESS_URL` | `http://127.0.0.1:8090` | Harness 侧车基础 URL |
| `OMA_FAKE_HARNESS` | — | `1` = 进程内假 harness（无需 Python） |
| `HARNESS_HTTP_TIMEOUT_SEC` | `600` | 平台 → harness HTTP 超时（秒） |
| `OMA_PUBLIC_URL` | `http://127.0.0.1:8787` | MCP proxy 与集成的对外 URL |
| `OMA_INTERNAL_SECRET` | — | `/v1/internal/*` 与 harness 取钥的共享密钥 |
| `OMA_OUTBOUND_PROXY_ADDR` | `:8790` | Vault outbound HTTP 代理监听地址 |
| `OMA_EVAL_WORKER_DISABLED` | — | `1` = 关闭 eval 后台 worker |
| `OMA_MEMORY_RETENTION_DISABLED` | — | `1` = 关闭 memory retention worker |
| `CONSOLE_DIR` | — | 已构建 Console `dist/` 路径 |
| `AUTH_DISABLED` | `0` | `1` = 跳过鉴权并桩化 `/auth/get-session`（仅开发） |
| `AUTH_UPSTREAM_URL` | `http://127.0.0.1:8788` | better-auth 侧车地址 |
| `AUTH_DATABASE_PATH` | `./data/auth.db` | better-auth SQLite 库 |
| `BETTER_AUTH_SECRET` | — | Cookie 签名密钥（生产必填） |
| `PUBLIC_BASE_URL` | `http://127.0.0.1:8787` | 对外 Origin（Cookie 域） |
| `ANTHROPIC_API_KEY` | — | 无匹配 model card 时的模型密钥回退 |

更多冒烟测试与 OAuth 相关变量见 `.env.example`。

## 设计文档

| 文档 | 主题 |
|------|------|
| [docs/design/streaming-turn-and-sse.md](./docs/design/streaming-turn-and-sse.md) | 回合生命周期与 SSE |
| [docs/design/subagent.md](./docs/design/subagent.md) | 子 Agent 委派 |
| [docs/design/session-threads.md](./docs/design/session-threads.md) | Session 线程 |
| [docs/design/mcp-architecture.md](./docs/design/mcp-architecture.md) | MCP proxy 与 loader |
| [docs/design/vault-and-credentials.md](./docs/design/vault-and-credentials.md) | Vault 与 outbound 代理 |
| [docs/design/runtime-architecture.md](./docs/design/runtime-architecture.md) | Runtimes 与 ACP daemon |
| [docs/design/eval-run-background-worker.md](./docs/design/eval-run-background-worker.md) | Eval worker |

## 技术栈

- **平台：** Go 1.22+、chi、modernc.org/sqlite（纯 Go，无 CGO）
- **执行器：** Python 3.11+、FastAPI、piPy（`pi_coding_agent`）
- **部署：** 单个 Go 静态二进制 + Python 侧车；Docker Compose 用于本地/类生产运行

## 仍属延后范围

Cloudflare Workers / SessionDO、CF Container 沙箱、R2/FUSE memory、Analytics Engine 计费、Dreams API、browser tools、web search、多区域 D1 分片，以及官方 SDK/CLI 包仍在范围外或仅部分实现。完整对齐矩阵与 backlog 见 [MVP-MIGRATION-PLAN.md](./MVP-MIGRATION-PLAN.md)。
