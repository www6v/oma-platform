# oma-platform

> English: [README.md](./README.md)

可自托管的 **Open Managed Agents (OMA)** 栈：Go 平台运行时 + Python piPy 执行侧车（sidecar）。平台负责持久化、并发与 HTTP API；执行器负责 LLM 循环与工具调用。

## 在线 Demo

> **立即体验：** [http://www.managed-agent.cloud:8787](http://www.managed-agent.cloud:8787)
>
> **账号密码：** `demo@126.com` &nbsp;/&nbsp; `12345678`

## 目录

- [在线 Demo](#在线-demo)
- [系统特性](#系统特性)
- [部署](#部署)
- [系统架构](#系统架构)
- [API](#api)
- [Python SDK](#python-sdk)
- [Console 控制台](#console-控制台)
- [配置项](#配置项)
- [设计文档](#设计文档)
- [技术栈](#技术栈)
- [仍属延后范围](#仍属延后范围)
- [许可证](#许可证)

## 系统特性

### 核心 Agent 闭环

- **版本化 Agent** — 创建、更新、归档，并保留不可变版本快照（`/v1/agents`）。
- **持久化 Session** — 基于 MySQL 的追加式事件日志；创建 Session 时固定 Agent 版本与环境配置。
- **Harness 回合** — 用户消息触发无状态 LLM 回合，经 piPy 侧车处理（`POST /internal/turn`）。
- **实时流式推送** — Server-Sent Events（`GET /v1/sessions/:id/events/stream`），支持可选回放。
- **回合中断** — `user.interrupt` 可取消正在执行的 harness 回合（可选按 `session_thread_id` 范围取消），清空 HITL pending 状态并将 Session 置为 idle。Console 支持 Stop 按钮。
- **崩溃恢复** — 平台启动时将孤儿 `running` Session 重置为 `idle`。
- **可插拔沙箱** — 通过 `SANDBOX_PROVIDER` 选择 `local` | `litebox` | `boxrun` | `e2b` | `daytona` | `opensandbox`；支持按环境绑定；Session 内执行（`POST /v1/sessions/:id/exec`）与文件提升（[设计](./docs/design/env/sandbox.md)、[绑定](./docs/design/environment-sandbox-binding.md)）。
- **Agent 工具集** — OMA `agent_toolset_20260401` 映射到 piPy 内置工具及 `web_fetch`、`web_search`。
- **Custom tools + HITL** — Agent 声明的自定义工具，支持 `requires_action` / `user.custom_tool_result` 人机协同流程（[设计文档](./docs/design/loop-task-termination.md)）。
- **Session resources** — 创建 Session 时或中途挂载 skill、文件等资源（`/v1/sessions/:id/resources`）。
- **子 Agent** — `call_agent_*` 与 `general_subagent` 委派，配合 Session 线程（[设计文档](./docs/design/orch/subagent.md)）。
- **Agent Team** — 多 Agent 协作，经 `team_*` harness 工具与 `/v1/sessions/:id/teams/*`（[设计文档](./docs/design/orch/agent-team.md)）。
- **Dynamic workflow** — YAML 工作流（支持自然语言生成），经 harness `pi_dynamic_workflows` 扩展执行；平台将 `/api/workflows` 代理到侧车，步骤以子 Agent 挂到 Session，执行轨迹可在 Session 中查看（Console 插件 `console/src/plugins/dynamic-workflows/`，扩展见 [`../piPy-dynamic-workflows/`](../piPy-dynamic-workflows/)）。
- **上下文压缩** — 长回合前的事件摘要（`harness/oma_adapter/compaction.py`）。
- **Schedule 工具** — `schedule`、`cancel_schedule`、`list_schedules`，MySQL 后台 wakeup worker 驱动。
- **MCP 工具** — Agent 声明的 MCP 服务，经 harness loader + `/v1/mcp-proxy` 挂载。
- **Resource mounter + outcome evaluator** — Session 资源挂载与基于 rubric 的结果评估（[设计文档](./docs/design/env/resource-mounter-and-outcome-evaluator.md)）。

### 平台 API（对齐 Console）

- **运行环境** — 带配置/元数据的命名执行上下文（可选沙箱配置）；启动时自动创建默认环境 `env-local-default`。
- **模型卡片** — 租户级模型凭证；回合执行时解析；harness 通过 internal 端点获取密钥。
- **Skills** — 内置目录 + 自定义技能，支持 zip/文件上传（`/v1/skills`）；ClawHub 导入（`/v1/clawhub`）。
- **Files** — 按 Session 作用域上传/下载文件（`/v1/files`）。
- **Vaults 与凭据** — 密钥存储与 OAuth 刷新；outbound HTTP 代理注入凭据。
- **Session 辅助接口** — 线程（从事件派生）、待确认工具、轨迹导出、输出文件、沙箱 exec。
- **统计与身份** — `/v1/stats`、`/v1/me`、`/v1/api_keys`。
- **Integrations** — Linear、GitHub、Slack 的 publication、OAuth、install proxy 与 webhook 分发。
- **Eval runs** — CRUD + 后台 worker（`internal/eval/worker.go`）。
- **Dreams** — CRUD + 后台 dream worker（`internal/dream/worker.go`）。
- **Cost report** — 用量与成本汇总（`/v1/cost_report`）。
- **Runtimes** — ACP daemon connect/exchange，供本地 IDE 挂载（[设计文档](./docs/design/runtime-architecture.md)）。
- **Memory stores** — 大对象存储 + retention worker（[设计文档](./docs/design/memory/memory.md)）。
- **Managed harnesses** — 可选 OpenClaw / Hermes 后端（Agent `_oma.harness: "managed"`）；可用性见 `GET /v1/config/harnesses`（`OMA_OPENCLAW_*` / `OMA_HERMES_*`）。

## 部署

### 本地

前置条件：工作区 Go 工具链位于 `../.tools/go`（辅助脚本会自动使用）；Harness 需要 Python 3.11+ 与 [uv](https://docs.astral.sh/uv/)；平台需要可达的 MySQL（在 `.env` 中配置 `DATABASE_URL`）。

```bash
cp .env.example .env
# 将 DATABASE_URL 改为你的 MySQL DSN / URL

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

### Docker

```bash
./deploy/docker.sh up
```

复制 `.env.example` 为 `.env`，并将 `DATABASE_URL` 指向可达的 MySQL。远程部署时将 `PUBLIC_BASE_URL` 设为浏览器访问地址（如 `http://124.221.28.203:8787`），并设置 `BETTER_AUTH_SECRET`。真实模型调用请设置 `OMA_FAKE_HARNESS=0`，并通过 `~/.pi/agent/settings.json`、`models.json`、`auth.json` 配置 piPy（compose 会挂载到 harness 容器）。

Compose 会把 `SESSION_OUTPUTS_DIR`、`FILES_DATA_DIR` 等指向共享卷 `/data/...`（见 `deploy/docker-compose.yml`）。若 agent 写入 `/mnt/session/outputs/` 后 `files.list(scope_id=session.id)` 看不到 `out:` 前缀文件，通常是 platform 与 harness 容器的数据目录未对齐——重新 `./deploy/docker.sh up --build` 即可。

`./deploy/docker.sh up` 在存在构建产物时，可将 `./console/dist` 挂载到 `/app/console`。需先运行 `./scripts/build-console.sh`，或在 compose 中设置 `CONSOLE_DIST`。

## 系统架构

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    客户端（Console / curl / SDK）                        │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ HTTP + SSE（工作流另含 WS）
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-server（Go）                                           :8787        │
│  agents · sessions · vaults · skills · files · model_cards              │
│  integrations · eval worker · dream worker · runtimes · memory_stores   │
│  teams · session resources · custom tool HITL · clawhub                 │
│  workflows proxy（/api/workflows） · mcp-proxy · outbound-proxy         │
│  internal API · Console SPA · session.Registry + stream.Hub（SSE）      │
├─────────────────────────────────────────────────────────────────────────┤
│  存储：MySQL（DATABASE_URL）+ 本地文件系统                               │
│    sandboxes/ · skills/ · files/ · memory/ · session-outputs/           │
│  Auth DB：SQLite（AUTH_DATABASE_PATH）                                   │
└───────────────────────────────────┬─────────────────────────────────────┘
                                    │ POST /internal/turn
                                    │ （工作流流量代理到 harness）
                                    ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  oma-harness（Python / FastAPI）                            :8090        │
│  turn · tools · compaction · web_fetch · web_search · mcp_loader        │
│  call_agent · custom tools · team_* · outcome supervisor                │
│  pi_dynamic_workflows · workflow bootstrap / sub-agent runner           │
│  工具经沙箱 provider 执行（本地 workdir 或远程）                          │
└─────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────┐
│  oma-sdk（Python）— https://github.com/www6v/oma-sdk                    │
│  anthropic base_url + httpx OMA-only 资源 · cookbook 辅助函数            │
└─────────────────────────────────────────────────────────────────────────┘
```

### 组件职责说明

| 层级 | 组件 | 职责 |
|------|------|------|
| **平台（Go）** | `cmd/oma-server` | 进程入口；组装数据库、worker、harness 客户端与 HTTP 服务。 |
| | `internal/api` | REST 路由、认证、集成、workflows 代理、Console 桩接口。 |
| | `internal/store` | MySQL 持久化与各资源 Repository（`DATABASE_URL`）。 |
| | `internal/session` | 回合状态机、按 Session 异步队列、中断处理。 |
| | `internal/stream` | 按 Session 的内存 SSE 发布/订阅。 |
| | `internal/sandbox` | 可插拔沙箱 provider 与环境绑定。 |
| | `internal/workdir` | 创建并隔离每个 Session 的沙箱目录（local provider）。 |
| | `internal/modelresolve` | 将 Agent 模型字符串解析为模型卡片凭证。 |
| | `internal/harness` | 调用 Python 侧车的 HTTP 客户端（开发态可用 `FakeClient`，或 managed OpenClaw/Hermes）。 |
| | `internal/outbound` | Vault 凭据注入（沙箱 HTTP 出站代理）。 |
| | `internal/eval` | Eval run 后台 worker。 |
| | `internal/dream` | Dream 后台 worker。 |
| | `internal/memory` | Memory retention 定时任务。 |
| | `internal/runtime` | ACP daemon 的 runtime room 注册表。 |
| | `internal/integrations/*` | Linear、GitHub、Slack gateway 处理器。 |
| | `workflows_proxy.go` | 将 `/api/workflows*` 反向代理到 harness（REST + WebSocket）。 |
| **执行器（Python）** | `harness/oma_adapter` | 基于 piPy `create_agent_session` 的 FastAPI 适配层。 |
| | `turn.py` | 无状态回合：投影事件 → 执行 prompt → 输出 OMA 事件。 |
| | `tools.py` | 将 OMA 工具声明映射为 piPy 内置/扩展工具名。 |
| | `custom_tools.py` | 自定义工具执行与 HITL `requires_action` 流程。 |
| | `compaction.py` | 回合前上下文压缩。 |
| | `call_agent/` | 子 Agent 委派运行时。 |
| | `workflow_*.py` | 对接 `pi_dynamic_workflows` 的 OMA bootstrap 与子 Agent runner。 |
| | `extensions/` | `web_fetch`、`web_search`、`mcp_loader`、`call_agent` 等 piPy 扩展。 |
| **SDK（Python）** | [oma-sdk](https://github.com/www6v/oma-sdk) | `OMAClient` + 资源类；cookbook 流式辅助函数。 |

### 一次用户回合的请求流程

1. 客户端 `POST /v1/sessions/:id/events`，提交 `user.message` 事件。
2. API 校验事件类型，写入 `session_events`，并在 Session Registry 中排队执行回合。
3. Session Machine 加载历史、确保沙箱目录、解析模型卡片，向 harness 发起 `POST /internal/turn`。
4. Harness 将持久化事件投影为 piPy 消息，以 `cwd=workdir` 创建内存 Agent Session，执行一次 prompt，返回新的 OMA 事件。
5. 平台持久化 harness 输出、更新 Session 状态，并向 SSE 订阅者广播每条事件。
6. 客户端可通过 `GET /v1/sessions/:id/events` 轮询，或 `GET /v1/sessions/:id/events/stream` 实时订阅。

Harness 侧回合是**无状态**的：每次调用都携带完整事件历史作为上下文。平台是持久化的唯一事实来源。

### 存储布局

| 路径 / 变量 | 用途 |
|-------------|------|
| `DATABASE_URL` | 平台 MySQL DSN / URL（必填） |
| `SANDBOX_WORKDIR`（默认 `./data/sandboxes`） | 按 Session 隔离的工具执行目录（local provider） |
| `SKILLS_DATA_DIR`（默认 `./data/skills`） | Skill 文件存储 |
| `FILES_DATA_DIR`（默认 `./data/files`） | 上传文件 blob |
| `MEMORY_DATA_DIR`（默认 `./data/memory`） | Memory 大对象存储 |
| `SESSION_OUTPUTS_DIR`（默认 `./data/session-outputs`） | Session 输出产物 |
| `AUTH_DATABASE_PATH`（默认 `./data/auth.db`） | better-auth SQLite 库 |

## API

与 [Claude Managed Agents API](https://docs.anthropic.com/en/docs/agents/managed-agents) 兼容。相同端点、相同事件类型，可与现有 SDK 一起使用。

<details>
<summary><strong>Agents</strong> — 创建和管理智能体配置</summary>

```http
POST   /v1/agents                          # 创建 Agent
GET    /v1/agents                          # 列出 Agent
GET    /v1/agents/:id                      # 获取 Agent
PATCH  /v1/agents/:id                      # 更新 Agent（产生新版本）
POST   /v1/agents/:id/archive              # 归档 Agent
GET    /v1/agents/:id/versions             # 版本历史
```

</details>

<details>
<summary><strong>Environments</strong> — 沙箱执行环境</summary>

```http
POST   /v1/environments                    # 创建环境
GET    /v1/environments                    # 列出环境
GET    /v1/environments/:id                # 获取环境
PATCH  /v1/environments/:id                # 更新环境
DELETE /v1/environments/:id                # 删除环境
```

</details>

<details>
<summary><strong>Sessions</strong> — 运行智能体对话</summary>

```http
POST   /v1/sessions                        # 创建 Session
GET    /v1/sessions                        # 列出 Session
GET    /v1/sessions/:id                    # 获取 Session
POST   /v1/sessions/:id/events             # 发送事件（用户消息）
GET    /v1/sessions/:id/events             # 分页获取事件
GET    /v1/sessions/:id/events/stream      # SSE 流
GET    /v1/sessions/:id/threads            # Session 线程（子 Agent）
GET    /v1/sessions/:id/pending            # 待确认工具
GET    /v1/sessions/:id/trajectory         # 轨迹导出
GET    /v1/sessions/:id/outputs            # Session 输出文件
POST   /v1/sessions/:id/exec               # 沙箱 exec
GET    /v1/sessions/:id/resources          # 列出 Session 资源
POST   /v1/sessions/:id/resources          # 附加资源
DELETE /v1/sessions/:id/resources/:resId   # 移除资源
GET    /v1/sessions/:id/teams/*            # Agent Team 协作
POST   /v1/sessions/:id/teams/*            # Agent Team 协作
```

</details>

<details>
<summary><strong>Vaults</strong> — 安全凭据存储</summary>

```http
POST   /v1/vaults                          # 创建 Vault
GET    /v1/vaults                          # 列出 Vault
POST   /v1/vaults/:id/credentials          # 添加凭据
GET    /v1/vaults/:id/credentials          # 列出（已脱敏密钥）
GET    /v1/oauth/*                         # Vault OAuth 流程
POST   /v1/oauth/*                         # Vault OAuth 流程
```

</details>

<details>
<summary><strong>Memory Stores</strong> — 持久化存储；Claude Managed Agents Memory 协议</summary>

附加到 Session 时，每个存储会挂载到沙箱的 `/mnt/memory/<store_name>/`。智能体使用**标准文件工具**（bash/read/write/edit/glob/grep）读写，没有专门的 `memory_*` 工具。

```http
POST   /v1/memory_stores                   # 创建存储
GET    /v1/memory_stores                   # 列出存储
GET    /v1/memory_stores/:id               # 获取存储
POST   /v1/memory_stores/:id/archive       # 归档（单向）
DELETE /v1/memory_stores/:id               # 删除存储
```

</details>

<details>
<summary><strong>Files & Skills</strong></summary>

```http
POST   /v1/files                           # 上传文件
GET    /v1/files                           # 列出文件
GET    /v1/files/:id/content               # 下载文件
POST   /v1/skills                          # 创建 Skill
GET    /v1/skills                          # 列出 Skills
GET    /v1/clawhub/*                       # ClawHub 技能搜索 / 导入
POST   /v1/clawhub/*                       # ClawHub 技能搜索 / 导入
```

</details>

此外还有：`GET /health`、模型卡片、managed harnesses（`GET /v1/config/harnesses`）、MCP proxy、工作流（`/api/workflows/*`）、evals、dreams、runtimes、cost report 与 integrations。受保护路由支持请求头 `x-api-key: $OMA_API_KEY`、`Authorization: Bearer $OMA_API_KEY` 或 better-auth Cookie 会话。仅本地开发可设 `AUTH_DISABLED=1`（勿用于生产）。

## Python SDK

SDK 位于 [`https://github.com/www6v/oma-sdk`](https://github.com/www6v/oma-sdk)，版本 `oma-sdk` v0.1.0（仅本地安装，尚未发布到 PyPI）。

```bash
# 需 platform 在 :8787 运行
git clone https://github.com/www6v/oma-sdk.git
cd oma-sdk
uv sync
export OMA_API_KEY=dev-key

# 运行示例（完整输出需真实 LLM）
uv run python example/example1/data_analyst_agent.py

# 对运行中服务器执行 SDK E2E 测试
uv run pytest tests/ -x
```

```python
from oma_sdk import OMAClient

client = OMAClient()  # 默认 http://localhost:8787
agent = client.agents.create(name="hello", model={"id": "claude-sonnet-4-6"})
session = client.sessions.create(agent=agent.id)
```

资源覆盖与 Cookbook 示例见 [SDK-PLAN.md](https://github.com/www6v/oma-sdk/blob/master/SDK-PLAN.md) 与 [example/README.md](https://github.com/www6v/oma-sdk/blob/master/example/README.md)。

## Console 控制台

当设置 `CONSOLE_DIR` 时，本仓库 `console/` 下的 OMA Console SPA 与 API 同端口提供服务。`./start-console.sh` 会在缺少 `console/dist/` 时自动构建，启动 better-auth 侧车并代理 `/auth/*`，支持邮箱密码注册登录。

**覆盖范围：** Agents、sessions、environments、model cards、skills、vaults、files、integrations、evals、dreams、runtimes、memory stores，以及 **dynamic workflows**（`/workflows`，插件 `console/src/plugins/dynamic-workflows/`）已对接 oma-platform API。Managed harness 下拉（OpenClaw / Hermes）跟随 `GET /v1/config/harnesses`。browser tools、`/v1/cap-cli/oauth`（Vault CLI tab）及部分 CF 专属能力仍延后 — 详见 [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md)。

## 配置项

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `OMA_LISTEN_ADDR` | `:8787` | 平台 HTTP 监听地址 |
| `OMA_API_KEY` | — | `x-api-key` / Bearer 认证密钥 |
| `DATABASE_URL` | — | 平台 MySQL URL/DSN（必填） |
| `SANDBOX_WORKDIR` | `./data/sandboxes` | Session 沙箱根目录（local provider） |
| `SANDBOX_PROVIDER` | `local` | `local` \| `litebox` \| `boxrun` \| `e2b` \| `daytona` \| `opensandbox` |
| `SKILLS_DATA_DIR` | `./data/skills` | Skill 文件存储 |
| `FILES_DATA_DIR` | `./data/files` | 文件 blob 存储 |
| `MEMORY_DATA_DIR` | `./data/memory` | Memory 大对象存储 |
| `SESSION_OUTPUTS_DIR` | `./data/session-outputs` | Session 输出产物 |
| `HARNESS_URL` | `http://127.0.0.1:8090` | Harness 侧车基础 URL |
| `OMA_FAKE_HARNESS` | — | `1` = 进程内假 harness（无需 Python） |
| `OMA_OPENCLAW_ENABLED` | — | 启用 managed OpenClaw harness |
| `OMA_OPENCLAW_GATEWAY_URL` / `OMA_OPENCLAW_TOKEN` | — | OpenClaw Gateway 端点 |
| `OMA_HERMES_ENABLED` | — | 启用 managed Hermes harness |
| `OMA_HERMES_GATEWAY_URL` / `OMA_HERMES_API_KEY` | — | Hermes Agent 端点 |
| `HARNESS_HTTP_TIMEOUT_SEC` | `600` | 平台 → harness HTTP 超时（秒） |
| `OMA_PUBLIC_URL` | `http://127.0.0.1:8787` | MCP proxy 与集成的对外 URL |
| `GATEWAY_ORIGIN` | （回退到 `PUBLIC_BASE_URL`） | Slack/GitHub OAuth `redirect_uri` 主机（与 open-managed-agents 一致） |
| `OMA_HARNESS_PLATFORM_BASE` | — | Harness → 平台回调基础 URL |
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

更多沙箱 provider 凭据（OpenSandbox、LiteBox、E2B、Daytona）、冒烟测试与 OAuth 相关变量见 `.env.example`。

## 设计文档

| 文档 | 主题 |
|------|------|
| [docs/design/session/streaming-turn-and-sse.md](./docs/design/session/streaming-turn-and-sse.md) | 回合生命周期与 SSE |
| [docs/design/loop-task-termination.md](./docs/design/loop-task-termination.md) | Custom tools、HITL 与中断 |
| [docs/design/orch/subagent.md](./docs/design/orch/subagent.md) | 子 Agent 委派 |
| [docs/design/orch/agent-team.md](./docs/design/orch/agent-team.md) | Agent Team 协作 |
| [docs/design/session/session-threads.md](./docs/design/session/session-threads.md) | Session 线程 |
| [docs/design/mcp-architecture.md](./docs/design/mcp-architecture.md) | MCP proxy 与 loader |
| [docs/design/vault/vault-and-credentials.md](./docs/design/vault/vault-and-credentials.md) | Vault 与 outbound 代理 |
| [docs/design/env/resource-mounter-and-outcome-evaluator.md](./docs/design/env/resource-mounter-and-outcome-evaluator.md) | Resource mounter 与 outcome 评估 |
| [docs/design/env/sandbox.md](./docs/design/env/sandbox.md) | 沙箱 providers |
| [docs/design/env/opensandbox-environment.md](./docs/design/env/opensandbox-environment.md) | OpenSandbox |
| [docs/design/environment-sandbox-binding.md](./docs/design/environment-sandbox-binding.md) | 环境 ↔ 沙箱绑定 |
| [docs/design/memory/memory.md](./docs/design/memory/memory.md) | Memory stores |
| [docs/design/runtime-architecture.md](./docs/design/runtime-architecture.md) | Runtimes 与 ACP daemon |
| [docs/design/eval-run-background-worker.md](./docs/design/eval-run-background-worker.md) | Eval worker |
| [docs/design/schedule-session-wakeup.md](./docs/design/schedule-session-wakeup.md) | Schedule 工具与 Session 唤醒 |
| [`../piPy-dynamic-workflows/`](../piPy-dynamic-workflows/) | Dynamic workflows 扩展 |

## 技术栈

- **平台：** Go 1.24+、chi、go-sql-driver/mysql
- **执行器：** Python 3.11+、FastAPI、piPy（`pi_coding_agent`）、`pi_dynamic_workflows`
- **SDK：** Python 3.11+、anthropic SDK 0.111+ 自定义 `base_url`、httpx（[oma-sdk](https://github.com/www6v/oma-sdk)）
- **部署：** 单个 Go 静态二进制 + Python 侧车；Docker Compose 用于本地/类生产运行；平台数据使用 MySQL

## 仍属延后范围

Cloudflare Workers / SessionDO、**CF Container** 沙箱（不同于上文已实现的 local/OpenSandbox/E2B 等可插拔沙箱）、R2/FUSE memory、Analytics Engine 计费、**browser tools**（T16）、多区域 D1 分片、**`/v1/cap-cli/oauth`**、Integration install → vault 双写，以及 **TypeScript SDK / `oma` CLI** 包仍在范围外或仅部分实现。**Python SDK**（`oma-sdk` v0.1.0）见 [`https://github.com/www6v/oma-sdk`](https://github.com/www6v/oma-sdk)，尚未发布到 PyPI。完整对齐矩阵与 backlog 见 [MVP-MIGRATION-PLAN.md](./docs/api-migrate/MVP-MIGRATION-PLAN.md)。

## 许可证

[Apache 2.0](https://www.apache.org/licenses/LICENSE-2.0)
