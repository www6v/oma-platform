# DeepSeek Harness 接入设计（含 Harness 架构扁平化重构）

日期：2026-08-20（v2，扁平化架构确认版）
状态：已确认（用户已审批方向、形态与扁平化决策）

## 1. 目标与范围

在 meta-harness 现有三种 harness（`piPy`/default-loop、`hermes`、`openclaw`）之外接入第 4 种：DeepSeek 官方 Agent Harness（`dsh`，仓库 `deepseek-harness-master`，v0.1.0-rc.7，developer preview）。

四种 harness 现状均为"HTTP 网关 + Go 无状态客户端"的同构形态，因此本次工作包含两部分：

1. **架构扁平化重构（先行）**：取消 `managed` Kind 下的第二层 managed agent id 分发，将 `hermes`/`openclaw` 升为一级 Kind；`deepseek` 直接以一级 Kind 落地
2. **DeepSeek 接入（增量）**：新增 `deepseek` Kind、客户端、配置、前端选项与 docker 部署

**范围内：**

- 后端 Go：registry 扁平化重构（行为保持 + legacy 归一化）、新增 `deepseek` Kind 客户端与配置
- Console 前端：下拉选项简化（去掉 `__managed_*` 哨兵与 `runtime_binding` 拼装）+ DeepSeek 选项
- 部署：docker-compose 新增 dsh 服务（Node.js 镜像）
- 测试：既有测试全绿（回归门禁）+ 新增单测/E2E
- 文档：`docs/plans/pluggable-harness-zh.md` 补决策变更记录

**范围外（明确排除）：**

- ACP 桥接路线（`dsh` 的 ACP stdio 服务器、oma 的 acp-proxy Kind）不迁移、不接入
- stdio JSON-RPC SDK 模式（仅作为 web RPC 不可行时的备选方案，见 §7）
- Phase 4 per-tenant daemon 池（`system_runtimes`）——若未来复活，按一级 Kind 分池即可
- 数据库迁移：无（harness 类型存于 `agents.config` JSON；存量数据靠分发期归一化兼容）

## 2. 架构决策

### 2.1 扁平化：一级 Kind 直达客户端（v2 新决策）

**背景**：现有两层结构（Kind → managed agent id）源自 pluggable-harness 决策 #1（对齐 open-ma 模型）与决策 #9（为 Phase 4 per-tenant daemon 池预留二级维度）。但 Phase 4 从未实施（`system_runtimes` 表只有 schema 无代码），hermes/openclaw 实际是直连共享网关的无状态 HTTP 客户端，与 piPy 的 `HTTPClient` 完全同构，两层结构的设计前提已不存在。

**目标形态**：`_oma.harness` 一级字段直接取值：

| Kind | 含义 | 别名/归一化 |
|---|---|---|
| `default-loop` | piPy sidecar（HTTP 网关） | `""`、`"pipy"` → `default-loop`（既有） |
| `hermes` | Hermes 网关客户端 | `managed` + `runtime_binding.agent="hermes"` → `hermes`（legacy 归一化） |
| `openclaw` | OpenClaw 网关客户端 | `managed` + `runtime_binding.agent="openclaw"` → `openclaw`（legacy 归一化） |
| `deepseek` | dsh web 网关客户端 | 无 legacy |
| `fake` | 测试 stub | 不变 |

wire 格式（新写入）：`{ "_oma": { "harness": "deepseek" } }`，网关型 Kind 不再需要 `runtime_binding`。

**Registry 结构**：网关客户端无状态，`ManagedFactory` 的每轮工厂解析退化为启动期构造的平铺客户端。`RegistryConfig` 变为 `Default / Hermes / OpenClaw / DeepSeek / Fake / Force` 平铺字段；`ClientFor` 单 switch 分发；legacy 归一化（`managed` + binding）在分发期完成，模式与既有 `"pipy"` 别名归一化（`registry.go:232`）一致。`DefaultOnly` 与 `OMA_FAKE_HARNESS` 强制覆盖行为不变。

**claude-acp / codex-acp 边界**：这两个 id 从未走 Go 侧 turn 分发（会话由外部 ACP runtime 处理）。本次：写入校验继续接受这两个值（避免破坏存量 ACP overlay agent 的创建/编辑），分发行为保持现状不动，作为 legacy 路径隔离，不随扁平化重构变化。

**决策记录义务**：扁平化实质推翻 pluggable-harness.md 决策 #1，需在 `docs/plans/pluggable-harness-zh.md` 补一条决策变更记录（理由：Phase 4 未实施、网关客户端同构、两层结构名不副实）。

### 2.2 接入形态：Web HTTP RPC 共享网关（已确认）

`dsh` 以 `dsh web` 形态部署为共享网关（默认 `127.0.0.1:3080`），Go 侧新增 `DeepSeekClient` 对接：

- 请求：`POST /api/<namespace>/<method>`，payload `{ args: {...} }`，响应 `{ ok, value }` 或 `{ ok: false, error: { code, message, details } }`（Typert 两段式端点）
- 事件下行：WebSocket `/api/events.mux`（mux-frame 格式，承载 `SessionEvent` 流）

### 2.3 dsh 版本锁定

dsh 处于 developer preview，其 web RPC 是前端专用 BFF 协议而非公开稳定 API。为控制上游漂移：docker 镜像内固化 dsh 源码版本（构建期 pin commit/版本）；所有协议适配逻辑隔离在 `internal/harness/deepseek_client.go` 单文件内。

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

### 3.2 dsh 侧待映射端点（spike 确认）

| oma 动作 | dsh 侧预期端点 | 说明 |
|---|---|---|
| 发起 turn | 会话创建/prompt 类 RPC（`<ns>/<method>` 待定） | payload 走 `{ args }` 包装；会话可按 sessionId 懒创建 |
| 事件流 | `/api/events.mux` WebSocket | mux-frame 逐帧解码 → 映射到 oma 事件 schema（`json.RawMessage`） |
| 取消 turn | 待定（cancel RPC 或断连兜底） | 对应 `Machine.CancelActiveTurn` 的 ctx cancel |
| 网关健康 | 待定 | 用于禁用回退判断与 docker healthcheck |

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

### Phase 0.5 — Harness 架构扁平化重构（P0，2-3 天）

原则：**行为保持型重构**（pluggable-harness 决策 #3——no behavior change de-risks the refactoring）。验收标准是既有测试全绿 + legacy 数据照常分发。

| 步骤 | 文件 | 内容 |
|---|---|---|
| A. Registry 扁平化 | `internal/harness/registry.go` | 新增 `KindHermes`/`KindOpenClaw` 常量；`RegistryConfig` 平铺 Hermes/OpenClaw 客户端字段；`ClientFor` 加 case；`managed` + binding 在 `ClientFor` 内走 legacy 归一化（复用 `ParseManagedBinding`，模式同既有 `pipy` 别名）；`ManagedFactory`/`NewManagedFactory` 移除，其分发逻辑内联进平铺客户端 |
| B. API 校验 | `internal/api/agents.go` | `validateManagedBinding` 演进为 Kind 白名单校验（新写入用一级 Kind）；`managed` + 已知 agent 的 legacy 写法继续接受；claude-acp/codex-acp 保持可写入 |
| C. 状态端点 | `internal/api/harness_config.go` + `registry.go` | `ManagedHarnessState` 演进为覆盖四个网关 harness 的启用状态（加字段，端点路径不变，前端置灰逻辑跟随） |
| D. main 装配 | `cmd/oma-server/main.go` | hermes/openclaw 配置直接装配为平铺客户端（替代 `NewManagedFactory` 包装） |
| E. 前端简化 | `console/src/pages/agents/AgentFormDialog.tsx` | 删除 `__managed_*` 哨兵值与 `runtime_binding` 拼装（create/edit/回显三处）；下拉选项值直接用 Kind；legacy 数据回显归一化 |
| F. 测试 | `registry_test.go`、`agents_managed_test.go`、`harness_config_test.go` | 既有测试全绿；新增 legacy 归一化测试（managed+hermes/openclaw → 新 Kind）与旧 wire 格式回显测试 |
| G. 文档 | `docs/plans/pluggable-harness-zh.md` | 补决策变更记录（推翻决策 #1 的理由） |

### Phase 1 — DeepSeek 后端接入（P0，1-2 天）

扁平化后此阶段为纯增量：

| 文件 | 动作 | 内容 |
|---|---|---|
| `internal/harness/registry.go` | 修改 | 新增 `KindDeepSeek` 常量、`ClientFor` case、启用状态字段 |
| `internal/harness/deepseek_client.go` | 新建 | `DeepSeekClient` 实现 `Client` + `StreamingClient`；`DeepSeekConfig`（GatewayURL/Token/Disabled） |
| `cmd/oma-server/main.go` | 修改 | 读 `OMA_DEEPSEEK_GATEWAY_URL` / `OMA_DEEPSEEK_TOKEN` / `OMA_DEEPSEEK_ENABLED` 装配客户端 |
| `.env.example` | 修改 | 增加三个环境变量 |
| 测试 | 新建/扩展 | `deepseek_client_test.go`（httptest 模拟网关）；registry/API 层加 case |

验收：API 创建 `_oma.harness="deepseek"` 的 agent，session turn 对 mock 网关跑通，再对真实 dsh 跑通。

### Phase 2 — Console 前端 DeepSeek 选项（P1，0.5-1 天）

- `AgentFormDialog.tsx`：下拉加 DeepSeek 选项（置灰逻辑跟随状态端点）
- 验证 `GET /v1/config/harnesses` 返回 deepseek 启用状态
- 前端单测更新

### Phase 3 — docker-compose 部署集成（P1，2 天）

- `deploy/docker-compose.yml` 新增 `dsh` 服务：Node 22+ 多阶段构建（pnpm install + build），入口 `dsh web`，挂载 `DSH_SESSION_ROOT` 卷，healthcheck
- 安全约束：dsh 仅加入内部 docker network、不暴露宿主端口（若 spike 确认无鉴权，此为硬性要求）；`DEEPSEEK_API_KEY` 经 env 注入
- Go 服务配置 `OMA_DEEPSEEK_GATEWAY_URL=http://dsh:3080`
- 可选：本地开发用 `start-deepseek.sh`（仿 `start-harness.sh`）

### Phase 4 — E2E 与硬化（P1，2 天）

- `scripts/e2e/smoke-deepseek-managed-e2e.sh`（仿 hermes/openclaw，无 API key 自动跳过）
- 硬化：超时控制、网关宕机回退、取消传播（ctx cancel → 断 WS / cancel RPC）、错误信息透出
- 文档：更新 pluggable-harness 实施状态

### Phase 5 — 可选增强（P2，视需要）

- token usage 映射进 `TurnResponse.Usage`（dsh token-meter 有现成数据）
- thinking/reasoning 内容在 console 的展示
- dsh 会话清理策略与多租户隔离评估

### 优先级汇总

- **P0**：Phase 0 + 0.5 + 1（重构 + 后端打通闭环，约 5-7 天）
- **P1**：Phase 2 + 3 + 4（产品可用 + 可部署 + 可回归，约 4.5-5 天）
- **P2**：Phase 5（增强项）

总预估 9-12 个工作日（不含 Phase 5）。

## 5. 风险评估

| # | 风险 | 等级 | 说明 | 缓解措施 |
|---|---|---|---|---|
| 1 | **扁平化回归影响现网 hermes/openclaw** | 中-高 | 重构触碰现网可用路径（registry 分发、API 校验、前端下拉） | 纯行为保持型重构；既有 `registry_test.go`/hermes/openclaw 客户端测试/`agents_managed_test.go` 全绿作为验收门禁；Phase 0.5 独立提交、可单独回滚 |
| 2 | **存量 legacy 数据兼容** | 中 | DB 已有 agent 存 `managed` + `runtime_binding`，归一化遗漏会导致 turn 分发失败 | 分发期归一化（模式同既有 `pipy` 别名）；专项归一化测试覆盖 managed+hermes/openclaw/claude-acp/codex-acp 全组合；前端旧格式回显测试 |
| 3 | **dsh 协议不稳定** | 高 | dsh 是 developer preview，web RPC 是前端专用 BFF 协议，上游升级可能破坏对接 | Phase 0 spike 先行；docker 镜像锁定 dsh 版本；适配逻辑隔离在 `deepseek_client.go` 单文件 |
| 4 | **dsh 鉴权缺失** | 高 | dsh web 默认绑定 127.0.0.1 且大概率无 token 机制 | 仅内部 docker network、不映射宿主端口；spike 确认鉴权能力；`OMA_DEEPSEEK_TOKEN` 预留但允许为空 |
| 5 | **事件格式映射复杂** | 中 | mux-frame 需逐帧解码并映射到 oma 事件 schema | spike 完成格式分析；必要时先交付非流式 `RunTurn`，流式降级为后续迭代 |
| 6 | **会话语义差异** | 中 | dsh 会话懒创建、落盘 `DSH_SESSION_ROOT`，与 oma session 生命周期不完全对齐 | oma session id → dsh sessionId 显式映射；卷挂载便于排查；清理策略入 Phase 5 |
| 7 | **Node 运行时依赖** | 中 | 平台现有组件是 Go + Python，新增 Node ≥22.19 + pnpm 11 | docker 多阶段构建隔离；本地开发文档说明 |
| 8 | **取消语义** | 低-中 | `Machine.CancelActiveTurn` 依赖 ctx，需映射到断连或 cancel RPC | spike 确认 cancel RPC；兜底：直接关闭 WS/HTTP 连接 |
| 9 | **E2E 依赖真实 DeepSeek API key** | 低 | CI 无 key 无法跑真实链路 | e2e 脚本无 key 自动跳过（沿用 hermes 模式），mock 网关测试兜底 |
| 10 | **ACP overlay 存量破坏** | 低 | claude-acp/codex-acp 的写入校验若收紧会破坏存量 ACP agent | 写入校验继续接受这两个值，分发行为不动，legacy 路径隔离 |

## 6. 回退方案

若 Phase 0 spike 判定 web RPC 不可对接（端点不可发现、事件格式过度耦合前端、或无法按 sessionId 驱动会话），回退方案为 **stdio JSON-RPC 守护进程模式**：

- 协议：换行分隔 JSON-RPC 2.0 over stdio（`initialize` / `session/prompt` / `session.event` / `shutdown`），文档化程度最高
- 代价：Go 侧需引入进程生命周期管理（spawn/监控/重启，平台尚无此能力），部署需 Node 运行时
- 该模式下 deepseek 仍是一级 Kind（扁平化架构对此无阻碍），仅客户端实现层从 HTTP 改为进程内 stdio

回退决策在 Phase 0 结束时做出，不推迟到后续阶段中途。

## 7. 测试策略

- **重构回归（Phase 0.5）**：既有 `registry_test.go`、`hermes_client_test.go`、`openclaw_client_test.go`、`agents_managed_test.go`、`harness_config_test.go` 全绿；新增 legacy 归一化测试（managed+各 agent id → 新 Kind）与旧 wire 格式回显测试
- **单测（Phase 1）**：`deepseek_client_test.go` 用 `httptest` 模拟 dsh 网关（RPC + WS 事件），覆盖 turn 成功/失败、流式事件映射、网关不可达回退
- **API 校验**：Kind 白名单测试（新格式 + legacy 格式均覆盖）
- **E2E**：`smoke-deepseek-managed-e2e.sh` 对真实 dsh + DeepSeek API，无 key 自动跳过
- **前端**：AgentFormDialog 相关单测覆盖新选项的创建/编辑/回显及 legacy 数据回显

## 8. 验收标准（整体）

1. 存量 `managed` + hermes/openclaw 的 agent 无需任何数据迁移，turn 分发行为与重构前一致（归一化测试证明）
2. 在 console 创建 agent 时可直接选择 piPy / hermes / openclaw / DeepSeek 四种平级选项，保存后 `_oma.harness` 为一级 Kind
3. DeepSeek agent 发起 session turn，流式事件正常渲染（或至少非流式结果返回）
4. `OMA_DEEPSEEK_ENABLED=0` 时选项置灰且网关回退 stub；hermes/openclaw/piPy 行为全程不变
5. `docker compose up` 后 dsh 服务健康，Go 服务经 `http://dsh:3080` 连通
6. 全部新增与既有测试通过
