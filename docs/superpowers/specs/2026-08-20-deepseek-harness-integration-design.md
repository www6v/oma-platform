# DeepSeek Harness 接入设计（第 4 种 managed agent）

日期：2026-08-20
状态：已确认（用户已审批方向与形态选择）

## 1. 目标与范围

在 oma-platform 现有三种 harness 接入形态（`pyPy`/default-loop、`hermes`、`openclaw`）之外，接入第 4 种：DeepSeek 官方 Agent Harness（`dsh`，仓库 `deepseek-harness-master`，v0.1.0-rc.7，developer preview）。

**范围内：**

- 后端 Go：新增 managed agent id `"deepseek"`，含客户端实现、注册表分发、环境变量配置
- Console 前端：agent 表单新增 DeepSeek 选项（创建/编辑/回显）
- 部署：docker-compose 新增 dsh 服务（Node.js 镜像）
- 测试：单测（mock 网关）、配置回归、E2E smoke 脚本
- 文档：更新 pluggable-harness 实施状态

**范围外（明确排除）：**

- ACP 桥接路线（`dsh` 的 ACP stdio 服务器、oma 的 acp-proxy Kind）不迁移、不接入
- stdio JSON-RPC SDK 模式（仅作为 web RPC 不可行时的备选方案，见 §4.1）
- Phase 4 per-tenant daemon 池（`system_runtimes`）——沿用现有 managed 直连共享网关模式
- 数据库迁移：无（harness 类型存于 `agents.config` JSON）

## 2. 架构决策

### 2.1 接入形态：Web HTTP RPC 共享网关（已确认）

`dsh` 以 `dsh web` 形态部署为共享网关（默认 `127.0.0.1:3080`），Go 侧新增 `DeepSeekClient` 对接：

- 请求：`POST /api/<namespace>/<method>`，payload `{ args: {...} }`，响应 `{ ok, value }` 或 `{ ok: false, error: { code, message, details } }`（Typert 两段式端点）
- 事件下行：WebSocket `/api/events.mux`（mux-frame 格式，承载 `SessionEvent` 流）

这与 hermes/openclaw 的"共享网关 + Go 网关客户端"模式完全同构，是架构一致性最高的方案。

### 2.2 平台侧定位：managed agent，非新 Kind

oma-platform 的 harness 是两层结构：

- 第一层 Kind（`internal/harness/registry.go`）：`default-loop`（pyPy sidecar）、`managed`、`fake`
- 第二层 managed agent id（`KnownAgents` 白名单 + `ManagedFactory` 分发）：`hermes`、`openclaw`、`claude-acp`、`codex-acp`

DeepSeek 作为 `managed` Kind 下的第 3 个网关型 agent（与 hermes/openclaw 并列），**不新增 Kind**。harness Kind 枚举、`Registry.ClientFor` 主分发逻辑均不改动。

### 2.3 dsh 版本锁定

dsh 处于 developer preview（v0.1.0-rc.7），其 web RPC 是前端专用 BFF 协议而非公开稳定 API。为控制上游漂移风险：

- docker 镜像内固化 dsh 源码版本（构建期 pin commit/版本）
- 所有协议适配逻辑隔离在 `internal/harness/deepseek_client.go` 单文件内，上游升级只改此文件

## 3. 关键接口映射（以 Phase 0 spike 结果为准）

### 3.1 oma 侧接口（已存在，不变）

```go
// internal/harness/client.go
type Client interface {
    RunTurn(ctx context.Context, req TurnRequest) (TurnResponse, error)
}
type StreamingClient interface {
    RunTurnStream(ctx context.Context, req TurnRequest, onEvent EventHandler) error
}
```

`TurnRequest` 含 agent 配置、会话 id、用户输入、模型配置等；`TurnResponse` 含助手输出与可选 `Usage`。

### 3.2 dsh 侧待映射端点（spike 确认）

| oma 动作 | dsh 侧预期端点 | 说明 |
|---|---|---|
| 发起 turn | 会话创建/prompt 类 RPC（`<ns>/<method>` 待定） | payload 走 `{ args }` 包装；会话可按 sessionId 懒创建 |
| 事件流 | `/api/events.mux` WebSocket | mux-frame 逐帧解码 → 映射到 oma 事件 schema（`json.RawMessage`） |
| 取消 turn | 待定（cancel RPC 或断连兜底） | 对应 `Machine.CancelActiveTurn` 的 ctx cancel |
| 网关健康 | 待定 | 用于 `Disabled` 回退判断与 docker healthcheck |

### 3.3 会话映射

oma session id → dsh sessionId 显式映射（dsh 支持按 sessionId 懒创建会话），会话日志落盘于 dsh 的 `DSH_SESSION_ROOT`（docker 卷挂载）。

## 4. 分阶段计划与优先级

### Phase 0 — 接口 Spike 验证（P0，1-2 天）

目标：验证 dsh web 协议可对接性，输出 Go/No-Go 决策。

1. 本地起 dsh web（Node ≥22.19、pnpm 11、`DEEPSEEK_API_KEY`）
2. 从 `packages/api/gateway` + `packages/api/remotes` 源码确定会话创建、prompt、事件订阅的确切 RPC 端点与参数结构
3. 抓包分析 `/api/events.mux` mux-frame 格式与 `SessionEvent` 结构；验证按 sessionId 懒创建语义
4. 确认鉴权机制（决定 docker 网络方案）与 cancel RPC 存在性
5. 产出 spike 报告：端点映射表 + 事件样例 + Go/No-Go

### Phase 1 — 后端核心接入（P0，3-4 天）

| 文件 | 动作 | 内容 |
|---|---|---|
| `internal/harness/registry.go` | 修改 | `KnownAgents` 加 `"deepseek"`；新增 `DeepSeekConfig`（参照 `OpenClawConfig`）；`NewDeepSeekFactory`；`NewManagedFactory` switch 加分支；`ManagedHarnessState` 加字段 |
| `internal/harness/deepseek_client.go` | 新建 | `DeepSeekClient` 实现 `Client` + `StreamingClient`；网关不可用时回退 `ManagedClient` stub |
| `cmd/oma-server/main.go` | 修改 | 读 `OMA_DEEPSEEK_GATEWAY_URL` / `OMA_DEEPSEEK_TOKEN` / `OMA_DEEPSEEK_ENABLED`，装配进 `NewManagedFactory` |
| `.env.example` | 修改 | 增加三个环境变量 |
| 测试 | 新建/扩展 | `deepseek_client_test.go`（httptest 模拟网关）；`registry_test.go`、`agents_managed_test.go`、`harness_config_test.go` 加分支 |

验收：API 创建 `runtime_binding.agent="deepseek"` 的 agent，session turn 对 mock 网关跑通，再对真实 dsh 跑通。

### Phase 2 — Console 前端（P1，1-2 天）

- `console/src/pages/agents/AgentFormDialog.tsx`：下拉选项（参照 `__managed_hermes__`/`__managed_openclaw__` 模式）、create/edit 两处 `_oma` wire 体生成、编辑回显
- 验证 `GET /v1/config/harnesses` 自动返回 deepseek 启用状态（结构体驱动，后端无需改路由）
- 前端单测更新

### Phase 3 — docker-compose 部署集成（P1，2 天）

- `deploy/docker-compose.yml` 新增 `dsh` 服务：Node 22+ 多阶段构建（pnpm install + build），入口 `dsh web`，挂载 `DSH_SESSION_ROOT` 卷，healthcheck
- 安全约束：dsh 仅加入内部 docker network、不暴露宿主端口（若 spike 确认无鉴权，此为硬性要求）；`DEEPSEEK_API_KEY` 经 env 注入
- Go 服务配置 `OMA_DEEPSEEK_GATEWAY_URL=http://dsh:3080`
- 可选：本地开发用 `start-deepseek.sh`（仿 `start-harness.sh`）

### Phase 4 — E2E 与硬化（P1，2 天）

- `scripts/e2e/smoke-deepseek-managed-e2e.sh`（仿 hermes/openclaw，无 API key 自动跳过）
- 硬化：超时控制、网关宕机回退、取消传播（ctx cancel → 断 WS / cancel RPC）、错误信息透出
- 文档：更新 `docs/plans/pluggable-harness-zh.md` 实施状态

### Phase 5 — 可选增强（P2，视需要）

- token usage 映射进 `TurnResponse.Usage`（dsh token-meter 有现成数据）
- thinking/reasoning 内容在 console 的展示
- dsh 会话清理策略与多租户隔离评估

### 优先级汇总

- **P0**：Phase 0 + 1（后端打通闭环，约 4-6 天）
- **P1**：Phase 2 + 3 + 4（产品可用 + 可部署 + 可回归，约 5-8 天）
- **P2**：Phase 5（增强项）

总预估 9-14 个工作日（不含 Phase 5）。

## 5. 风险评估

| # | 风险 | 等级 | 说明 | 缓解措施 |
|---|---|---|---|---|
| 1 | 协议不稳定 | 高 | dsh 是 developer preview，web RPC 是前端专用 BFF 协议，上游升级可能破坏对接 | Phase 0 spike 先行；docker 镜像锁定 dsh 版本；适配逻辑隔离在 `deepseek_client.go` 单文件 |
| 2 | 鉴权缺失 | 高 | dsh web 默认绑定 127.0.0.1 且大概率无 token 机制，与 hermes/openclaw 的 token 鉴权不同 | 仅内部 docker network、不映射宿主端口；spike 确认鉴权能力；`OMA_DEEPSEEK_TOKEN` 预留但允许为空 |
| 3 | 事件格式映射复杂 | 中 | mux-frame 需逐帧解码并映射到 oma 事件 schema | spike 完成格式分析；必要时先交付非流式 `RunTurn`，流式降级为后续迭代 |
| 4 | 会话语义差异 | 中 | dsh 会话懒创建、落盘 `DSH_SESSION_ROOT`，与 oma session 生命周期不完全对齐 | oma session id → dsh sessionId 显式映射；卷挂载便于排查；清理策略入 Phase 5 |
| 5 | Node 运行时依赖 | 中 | 平台现有组件是 Go + Python，新增 Node ≥22.19 + pnpm 11 | docker 多阶段构建隔离；本地开发文档说明 |
| 6 | 取消语义 | 低-中 | `Machine.CancelActiveTurn` 依赖 ctx，需映射到断连或 cancel RPC | spike 确认 cancel RPC；兜底：直接关闭 WS/HTTP 连接 |
| 7 | 回归影响现有三种 harness | 低 | 改动集中于 registry 分发与新增文件，无 DB 迁移 | 现有 `registry_test.go`/hermes/openclaw 测试作为回归门禁 |
| 8 | E2E 依赖真实 DeepSeek API key | 低 | CI 无 key 无法跑真实链路 | e2e 脚本无 key 自动跳过（沿用 hermes 模式），mock 网关测试兜底 |

## 6. 回退方案

若 Phase 0 spike 判定 web RPC 不可对接（端点不可发现、事件格式过度耦合前端、或无法按 sessionId 驱动会话），回退方案为 **stdio JSON-RPC 守护进程模式**：

- 协议：换行分隔 JSON-RPC 2.0 over stdio（`initialize` / `session/prompt` / `session.event` / `shutdown`），文档化程度最高
- 代价：Go 侧需引入进程生命周期管理（spawn/监控/重启，平台尚无此能力），部署需 Node 运行时
- 该模式下 §2.2 的 managed agent 定位不变，仅客户端实现层从 HTTP 改为进程内 stdio

回退决策在 Phase 0 结束时做出，不推迟到 Phase 1 中途。

## 7. 测试策略

- **单测**：`deepseek_client_test.go` 用 `httptest` 模拟 dsh 网关（RPC + WS 事件），覆盖 turn 成功/失败、流式事件映射、网关不可达回退
- **注册回归**：`registry_test.go` 增加 deepseek 分发分支；现有 hermes/openclaw/default-loop 测试必须全绿
- **API 回归**：`agents_managed_test.go` 校验 binding 白名单；`harness_config_test.go` 校验配置暴露
- **E2E**：`smoke-deepseek-managed-e2e.sh` 对真实 dsh + DeepSeek API，无 key 自动跳过
- **前端**：AgentFormDialog 相关单测覆盖新选项的创建/编辑/回显

## 8. 验收标准（整体）

1. 在 console 创建 agent 时可选择 DeepSeek（managed），保存后 `_oma` wire 体正确
2. 通过该 agent 发起 session turn，流式事件正常渲染（或至少非流式结果返回）
3. `OMA_DEEPSEEK_ENABLED=0` 时选项不出现/网关回退 stub，现有三种 harness 行为不变
4. `docker compose up` 后 dsh 服务健康，Go 服务经 `http://dsh:3080` 连通
5. 全部新增与既有测试通过
