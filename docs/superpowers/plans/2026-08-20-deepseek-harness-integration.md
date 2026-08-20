# DeepSeek Harness 接入 + Harness 架构扁平化 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 oma-platform 的 harness 分发从两层结构（Kind → managed agent id）扁平化为一级 Kind，并以一级 Kind `deepseek` 接入 DeepSeek 官方 Agent Harness（dsh web 网关）。

**Architecture:** 行为保持型重构先行（Task 2-6：registry 平铺客户端 + legacy 归一化，既有测试全绿为门禁），然后纯增量接入 DeepSeekClient（HTTP RPC + WebSocket 事件流，隔离在单文件），最后前端、docker、E2E 收尾。存量 `managed` + `runtime_binding` 数据靠 `ClientFor` 分发期归一化兼容，无数据库迁移。

**Tech Stack:** Go 1.24（gorilla/websocket 已在 go.mod）、React 19 + vitest（console）、docker compose（Node 22 镜像承载 dsh）。

**Spec:** `docs/superpowers/specs/2026-08-20-deepseek-harness-integration-design.md`（v2，已确认）

## Global Constraints

- Go 测试命令：`go test ./...`（在 `oma-platform/` 根目录执行）；定向测试用 `go test ./internal/harness/... -run TestXxx -v`
- 前端测试命令：`npm test`（在 `console/` 目录，vitest run）；类型检查 `npm run typecheck`
- 重构阶段（Task 2-6）是行为保持型：任何既有测试失败都是缺陷，不是"测试需要更新"；仅当测试直接引用被删除的符号（`ManagedFactory` 字段、`NewManagedFactory` 等）时才按本计划给出的代码改写
- dsh web 网关**无鉴权**（spike 待确认）：docker 中不暴露宿主端口，仅内部网络
- legacy 归一化行为必须精确保留：`managed`+`hermes` → HermesClient；`managed`+`openclaw` → OpenClawClient(`openclaw/default`)；`managed`+其他已知 agent（claude-acp/codex-acp 等）→ OpenClawClient(`openclaw/<agent>`)（现状 pass-through，见 registry.go:150-159）
- 提交信息使用 conventional commits（`refactor(harness):` / `feat(harness):` / `chore(deploy):` / `docs:`）
- 环境变量命名：`OMA_DEEPSEEK_GATEWAY_URL` / `OMA_DEEPSEEK_TOKEN` / `OMA_DEEPSEEK_ENABLED`（ENABLED 语义同现有：`envDisabled` 判定 0/false/no/off 为禁用）

---

## File Structure

| 文件 | 动作 | 责任 |
|---|---|---|
| `internal/harness/registry.go` | 修改 | 一级 Kind 分发 + legacy 归一化 + 启用状态 |
| `internal/harness/deepseek_client.go` | 新建 | dsh web 网关客户端（RPC + WS 事件映射），唯一协议适配层 |
| `internal/api/agents.go` | 修改 | Kind 白名单校验（新旧格式） |
| `internal/api/harness_config.go` | 修改 | 状态端点（结构体驱动，无需改路由） |
| `cmd/oma-server/main.go` | 修改 | 平铺客户端装配 + 状态计算 |
| `internal/harness/registry_test.go` | 修改 | 工厂测试改写为平铺/归一化测试 |
| `internal/api/harness_config_test.go` | 修改 | 状态计算新签名 |
| `internal/api/agents_kinds_test.go` | 新建 | 一级 Kind 写入校验测试 |
| `internal/harness/deepseek_client_test.go` | 新建 | httptest 模拟网关 |
| `console/src/pages/agents/AgentFormDialog.tsx` | 修改 | 下拉直连 Kind + DeepSeek 选项 |
| `console/src/types/agent.ts` | 修改 | `_oma.harness` 类型注释更新 |
| `.env.example` | 修改 | 新增 OMA_DEEPSEEK_* 段 |
| `deploy/Dockerfile.dsh` | 新建 | dsh 多阶段构建镜像 |
| `deploy/docker-compose.yml` | 修改 | dsh 服务（内部网络、不暴露端口） |
| `scripts/e2e/smoke-deepseek-managed-e2e.sh` | 新建 | 真实链路 E2E（无 key 自动跳过） |
| `start-deepseek.sh` | 新建 | 本地开发启动 dsh（仿 start-harness.sh） |
| `docs/plans/pluggable-harness-zh.md` | 修改 | 决策变更记录（推翻原决策 #1） |

---

### Task 1: Phase 0 Spike — dsh web 协议验证

**Files:**
- Create: `docs/superpowers/spikes/2026-08-20-dsh-web-protocol.md`（spike 报告）

**Interfaces:**
- Produces: Task 12 的 `DeepSeekClient` 依赖本报告确认的三个事实——① 发起 prompt 的 RPC 端点与 args 结构；② `/api/events.mux` 的帧编码；③ 会话创建的显式端点（或懒创建语义）

- [ ] **Step 1: 启动 dsh web**

在 `deepseek-harness-master/` 目录：

```bash
pnpm install
pnpm run build
DEEPSEEK_API_KEY=<key> pnpm dsh web
```

Expected: 监听 `http://127.0.0.1:3080`，浏览器可见 Web UI。若 Node 版本不足（需 `^22.19 || >=24`）先升级 Node。

- [ ] **Step 2: 确认容器监听地址**

dsh web 默认绑定 127.0.0.1，docker 内必须改为 0.0.0.0。查找启动参数/env：

```bash
rg -n "3080|DSH_.*HOST|listen" apps/cli/src packages/host/webserver/src
```

记录确切的配置项（如 `--port`/`DSH_HOST` 之类），写入 spike 报告。若不存在，评估在镜像入口用 `socat` 转发的兜底方案。

- [ ] **Step 3: 抓取 RPC 端点**

用浏览器 DevTools 对 Web UI 发一条消息，记录 Network 面板中的请求。已知线索（源码核对过）：

- `ISession.prompt(content: PromptContentPart[], mode: 'queue' | 'steer')` → `Promise<RpcResult<{ accepted: true }>>`（见 `packages/extensions/cordis-client-runner/src/client/api-catalog.ts:574`）
- `ISession.cancel()` → `Promise<RpcResult<{ accepted: true }>>`（同文件）
- `ISession` 是 scoped remote：调用携带 sessionId 上下文。确认其在 wire 上如何表达（URL 段、header 或 args 字段），并确认会话创建/打开的确切端点
- RPC 包装格式：`POST /api/<namespace>/<method>`，payload `{ args: {...} }`，响应 `{ ok, value }` / `{ ok: false, error: { code, message, details } }`

用 curl 脱离浏览器重放一次完整 turn（创建/打开会话 → prompt → 收到完成），记录每个请求的完整报文。

- [ ] **Step 4: 分析事件流**

连接 `/api/events.mux` WebSocket，记录：

- 帧编码：是否每条消息前有长度/类型前缀（mux-frame）？还是裸 JSON 行？
- 会话过滤：帧是否自带 `sessionId`？如何只订阅一个会话？
- `SessionEvent` 信封：`{ type, seq, time, data }`（见 `packages/core/session/src/types.ts:404-436`）。确认 wire 上事件是直接裸信封还是再包一层

需要映射的事件类型（词汇表见 `packages/core/session/src/known-event-types.ts`）：`assistant/chunk`（delta 文本）、`assistant/message`（含 `usage?: TokenUsage`）、`tool/call`（callId/name/arguments）、`tool/result`（message/error）、`turn/end`（reason: completed/aborted/error/max-tokens）。

- [ ] **Step 5: 确认鉴权与 cancel 语义**

- 鉴权：检查 webserver 插件源码是否支持 token；记录结论（预期：无）
- cancel：除 `ISession.cancel()` RPC 外，确认断开 prompt 请求/WS 是否也能终止 turn

- [ ] **Step 6: 写 spike 报告并 commit**

报告必须包含：端点映射表（oma 动作 → dsh 端点 + 完整示例报文）、mux 帧解码伪代码、Go/No-Go 结论。

```bash
git add docs/superpowers/spikes/2026-08-20-dsh-web-protocol.md
git commit -m "docs: add dsh web protocol spike report"
```

**决策门：** No-Go（RPC 无法脱离浏览器驱动、或事件格式无法解码）则停止本计划，启用 spec §6 的 stdio 回退方案重新规划。Go 则继续 Task 2。

---

### Task 2: Registry 扁平化 — Kind 常量与 RegistryConfig

**Files:**
- Modify: `internal/harness/registry.go`
- Test: `internal/harness/registry_test.go`

**Interfaces:**
- Produces: `KindHermes` / `KindOpenClaw` 常量、`normalizeKind`（legacy 归一化）、平铺 `RegistryConfig`（`Hermes`/`OpenClaw`/`DeepSeek` 字段，Task 4 使用）、新 `ClientFor` 分发。Task 3 的 API 校验依赖归一化后的 Kind 集合

- [ ] **Step 1: 写失败测试（平铺分发 + legacy 归一化）**

在 `internal/harness/registry_test.go` 末尾追加：

```go
func TestClientFor_HermesKind_Flat(t *testing.T) {
	hc := &stubClient{name: "hermes"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, Hermes: hc})
	got, err := r.ClientFor(store.AgentConfig{Harness: "hermes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hc {
		t.Fatalf("expected hermes client")
	}
}

func TestClientFor_OpenClawKind_Flat(t *testing.T) {
	oc := &stubClient{name: "openclaw"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, OpenClaw: oc})
	got, err := r.ClientFor(store.AgentConfig{Harness: "openclaw"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != oc {
		t.Fatalf("expected openclaw client")
	}
}

func TestClientFor_LegacyManaged_Hermes(t *testing.T) {
	hc := &stubClient{name: "hermes"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, Hermes: hc})
	got, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"hermes"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != hc {
		t.Fatalf("managed+hermes must normalize to hermes client")
	}
}

func TestClientFor_LegacyManaged_OpenClaw(t *testing.T) {
	oc := &stubClient{name: "openclaw"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, OpenClaw: oc})
	got, err := r.ClientFor(store.AgentConfig{
		Harness:        "managed",
		RuntimeBinding: json.RawMessage(`{"agent":"openclaw"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != oc {
		t.Fatalf("managed+openclaw must normalize to openclaw client")
	}
}

func TestClientFor_HermesKind_NotConfigured_ReturnsStub(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}})
	got, err := r.ClientFor(store.AgentConfig{Harness: "hermes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", got)
	}
}

func TestNormalizeKind(t *testing.T) {
	cases := []struct {
		harness string
		binding json.RawMessage
		want    Kind
	}{
		{"", nil, KindDefaultLoop},
		{"pipy", nil, KindDefaultLoop},
		{"default-loop", nil, KindDefaultLoop},
		{"hermes", nil, KindHermes},
		{"openclaw", nil, KindOpenClaw},
		{"fake", nil, KindFake},
		{"managed", json.RawMessage(`{"agent":"hermes"}`), KindHermes},
		{"managed", json.RawMessage(`{"agent":"openclaw"}`), KindOpenClaw},
		{"managed", json.RawMessage(`{"agent":"claude-acp"}`), KindOpenClaw},
	}
	for _, c := range cases {
		got, err := normalizeKind(
			store.AgentConfig{Harness: c.harness, RuntimeBinding: c.binding},
		)
		if err != nil {
			t.Fatalf("%q: unexpected error: %v", c.harness, err)
		}
		if got != c.want {
			t.Errorf("%q+%s: got %q want %q",
				c.harness, string(c.binding), got, c.want)
		}
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go build ./...`
Expected: 编译失败——`RegistryConfig` 无 `Hermes`/`OpenClaw` 字段，`normalizeKind` 未定义，`KindHermes`/`KindOpenClaw` 未定义。

- [ ] **Step 3: 实现 registry.go 扁平化**

在 `registry.go` 中：

(a) Kind 常量段（registry.go:14-22）改为：

```go
const (
	// KindDefaultLoop is the piPy HTTP sidecar. The legacy alias "pipy" is
	// accepted on input and normalized to KindDefaultLoop.
	KindDefaultLoop Kind = "default-loop"
	// KindManaged is the legacy two-layer kind. Agents written before the
	// 2026-08 flattening carry harness="managed" plus runtime_binding.agent;
	// normalizeKind maps them onto the flat gateway kinds at dispatch time.
	// Kept for legacy data only — new agents write the flat kinds directly.
	KindManaged Kind = "managed"
	// KindHermes is the Hermes Agent gateway client.
	KindHermes Kind = "hermes"
	// KindOpenClaw is the OpenClaw Gateway client.
	KindOpenClaw Kind = "openclaw"
	// KindDeepSeek is the DeepSeek harness (dsh web) gateway client.
	// Declared here so validation/state code has the constant; the
	// dispatch case lands with DeepSeekClient.
	KindDeepSeek Kind = "deepseek"
	// KindFake is the test stub. OMA_FAKE_HARNESS env var resolves here.
	KindFake Kind = "fake"
)
```

(b) `Registry` 结构体（registry.go:164-169）与 `RegistryConfig`（171-185）改为：

```go
// Registry resolves a harness.Client per agent based on the agent's
// `_oma.harness` metadata. One Registry is constructed at process start
// and threaded through every session.Machine.
type Registry struct {
	defaultClient  Client
	hermesClient   Client
	openclawClient Client
	deepseekClient Client
	fakeClient     Client
	forceClient    Client // if non-nil, returned for every agent (env-var override)
}

// RegistryConfig holds the per-kind clients. The gateway kinds are
// stateless HTTP clients constructed once at process start (the former
// ManagedFactory indirection existed for the never-implemented daemon
// pool and was removed by the 2026-08 flattening).
type RegistryConfig struct {
	// Default is the client returned for KindDefaultLoop (and for agents
	// with no _oma.harness set, since "" normalizes to default-loop).
	Default Client
	// Hermes is the client for KindHermes. Nil falls back to the
	// ManagedClient stub.
	Hermes Client
	// OpenClaw is the client for KindOpenClaw. Nil falls back to the
	// ManagedClient stub.
	OpenClaw Client
	// DeepSeek is the client for KindDeepSeek. Nil falls back to the
	// ManagedClient stub.
	DeepSeek Client
	// Fake is the client returned for KindFake. Defaults to &FakeClient{}
	// when nil.
	Fake Client
	// Force overrides all dispatch when set. Used by OMA_FAKE_HARNESS to
	// keep the legacy "every session uses this test client" behavior.
	Force Client
}
```

(c) `NewRegistry`（registry.go:190-207）改为：

```go
// NewRegistry builds a Registry from cfg.
func NewRegistry(cfg RegistryConfig) *Registry {
	fake := cfg.Fake
	if fake == nil {
		fake = &FakeClient{}
	}
	return &Registry{
		defaultClient:  cfg.Default,
		hermesClient:   cfg.Hermes,
		openclawClient: cfg.OpenClaw,
		deepseekClient: cfg.DeepSeek,
		fakeClient:     fake,
		forceClient:    cfg.Force,
	}
}
```

(d) `DefaultOnly`（registry.go:213-221）改为：

```go
// DefaultOnly builds a Registry that returns defaultClient for every agent
// regardless of _oma.harness. Use this to migrate existing tests with a
// 1-line change: `Harness: c` → `HarnessRegistry: DefaultOnly(c)`.
// Gateway kinds fall back to the ManagedClient stub.
func DefaultOnly(defaultClient Client) *Registry {
	return &Registry{
		defaultClient: defaultClient,
		fakeClient:    &FakeClient{},
	}
}
```

(e) 新增 `normalizeKind`，并重写 `ClientFor`（registry.go:223-252）：

```go
// normalizeKind maps an agent's harness fields onto a flat Kind.
// Legacy two-layer agents (harness="managed" + runtime_binding.agent) are
// normalized here at dispatch time, the same pattern as the "pipy" alias
// for default-loop — no data migration needed.
func normalizeKind(agent store.AgentConfig) (Kind, error) {
	kind := Kind(agent.Harness)
	switch kind {
	case "":
		return KindDefaultLoop, nil
	case "pipy":
		return KindDefaultLoop, nil
	case KindDefaultLoop, KindHermes, KindOpenClaw, KindDeepSeek, KindFake:
		return kind, nil
	case KindManaged:
		b, err := ParseManagedBinding(agent.RuntimeBinding)
		if err != nil {
			return "", err
		}
		switch b.Agent {
		case "hermes":
			return KindHermes, nil
		case "openclaw":
			return KindOpenClaw, nil
		default:
			// claude-acp / codex-acp / anything else keeps the
			// pre-flattening pass-through to the OpenClaw client
			// (model "openclaw/<agent>" — see openclawModel).
			return KindOpenClaw, nil
		}
	}
	return "", fmt.Errorf("harness registry: unknown kind %q", kind)
}

// ClientFor resolves the harness.Client for the given agent config.
func (r *Registry) ClientFor(agent store.AgentConfig) (Client, error) {
	if r.forceClient != nil {
		return r.forceClient, nil
	}
	kind, err := normalizeKind(agent)
	if err != nil {
		return nil, err
	}
	switch kind {
	case KindDefaultLoop:
		if r.defaultClient == nil {
			return nil, fmt.Errorf(
				"harness registry: default-loop client not configured")
		}
		return r.defaultClient, nil
	case KindHermes:
		if r.hermesClient == nil {
			return ManagedClient{}, nil
		}
		return r.hermesClient, nil
	case KindOpenClaw:
		if r.openclawClient == nil {
			return ManagedClient{}, nil
		}
		return r.openclawClient, nil
	case KindDeepSeek:
		if r.deepseekClient == nil {
			return ManagedClient{}, nil
		}
		return r.deepseekClient, nil
	case KindFake:
		return r.fakeClient, nil
	}
	return nil, fmt.Errorf("harness registry: unknown kind %q", kind)
}
```

(f) 删除 `NewManagedFactory`（registry.go:150-159）。`NewOpenClawFactory`/`NewHermesFactory` 暂保留（Task 3 移除）。

- [ ] **Step 4: 运行测试确认新测试通过**

Run: `go test ./internal/harness/ -run 'TestNormalizeKind|TestClientFor_Hermes|TestClientFor_OpenClaw|TestClientFor_LegacyManaged|TestClientFor_DefaultLoop|TestClientFor_Fake|TestClientFor_Unknown|TestClientFor_Force|TestDefaultOnly|TestParseManagedBinding|TestManagedClient' -v`
Expected: PASS（上述所有测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/harness/registry.go internal/harness/registry_test.go
git commit -m "refactor(harness): flatten registry to one-level kinds with legacy normalization"
```

---

### Task 3: 移除工厂层，OpenClaw 模型 id 计算保留

**Files:**
- Modify: `internal/harness/registry.go`
- Modify: `internal/harness/registry_test.go`

**Interfaces:**
- Consumes: Task 2 的平铺 `ClientFor`
- Produces: `openclawModel(bindingAgent string) string`（main.go 装配 OpenClawClient 时使用）；工厂测试全部替换为归一化/平铺测试。此后 `internal/harness` 不再导出任何 factory 构造函数

背景：legacy `managed`+其他 agent 的 OpenClaw pass-through 行为（模型 id `openclaw/<agent>`）必须保留。但平铺后 `OpenClawClient` 是进程级单例，无法按 binding 变模型。现状下除 openclaw 外的 binding（claude-acp/codex-acp）从不走 Go turn 分发，故单例使用 `openclaw/default` 模型不产生行为差异；`openclawModel` 函数保留该映射逻辑供 Task 4 装配与未来使用。

- [ ] **Step 1: 改写工厂测试**

在 `internal/harness/registry_test.go` 中删除以下测试函数（它们引用即将移除的符号）：
`TestClientFor_Managed_CallsFactory`、`TestClientFor_Managed_MissingBinding`、`TestClientFor_Managed_MissingAgentField`、`TestClientFor_Managed_FactoryNotConfigured_ReturnsStub`、`TestNewOpenClawFactory_EmptyGatewayURL_ReturnsStub`、`TestNewOpenClawFactory_ReturnsOpenClawClient`、`TestNewOpenClawFactory_NonOpenclawAgent_PassThrough`、`TestRegistry_WithOpenClawFactory`、`TestNewHermesFactory_EmptyGatewayURL_ReturnsStub`、`TestNewHermesFactory_ReturnsHermesClient`、`TestNewManagedFactory_DispatchesByAgent`、`TestNewManagedFactory_BothEmpty_ReturnsStub`、`TestNewManagedFactory_Disabled_OverridesURL`、`TestNewManagedFactory_OneDisabled_RoutesCorrectly`。

其中 `TestClientFor_Managed_MissingBinding` / `TestClientFor_Managed_MissingAgentField` 的语义（错误输入报错）用下面的归一化错误测试替代，追加到文件末尾：

```go
func TestNormalizeKind_Managed_Errors(t *testing.T) {
	cases := []struct {
		name    string
		binding json.RawMessage
	}{
		{"missing binding", nil},
		{"empty object", json.RawMessage(`{}`)},
		{"malformed json", json.RawMessage(`{not json`)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := normalizeKind(store.AgentConfig{
				Harness:        "managed",
				RuntimeBinding: c.binding,
			})
			if err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestOpenclawModel(t *testing.T) {
	cases := []struct{ binding, want string }{
		{"openclaw", "openclaw/default"},
		{"hermes", "openclaw/hermes"},
		{"claude-acp", "openclaw/claude-acp"},
		{"coding", "openclaw/coding"},
	}
	for _, c := range cases {
		if got := openclawModel(c.binding); got != c.want {
			t.Errorf("openclawModel(%q) = %q, want %q",
				c.binding, got, c.want)
		}
	}
}
```

同时把 `TestClientFor_Force_OverridesEverything` 中的 kind 列表更新为包含平铺 Kind：

```go
	for _, kind := range []string{
		"", "default-loop", "pipy", "managed",
		"hermes", "openclaw", "fake", "nonsense",
	} {
```

把 `TestDefaultOnly_AlwaysReturnsDefault` 末尾的 managed 段保持不变（归一化后仍返回 stub——DefaultOnly 的 openclawClient 为 nil）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/harness/ -run 'TestOpenclawModel|TestNormalizeKind_Managed_Errors' -v`
Expected: 编译失败——`openclawModel` 未定义。

- [ ] **Step 3: 实现——移除工厂、新增 openclawModel**

在 `registry.go` 中删除 `NewOpenClawFactory`（registry.go:100-125）与 `NewHermesFactory`（127-144），在原位置加入：

```go
// OpenclawModel maps a legacy runtime_binding.agent value to the
// OpenClaw model id. Pre-flattening behavior (registry_test history):
// "openclaw" becomes "openclaw/default", anything else becomes
// "openclaw/<agent>". Exported so cmd/oma-server can assemble the
// single client (Task 4).
func OpenclawModel(bindingAgent string) string {
	if bindingAgent == "openclaw" {
		return "openclaw/default"
	}
	return "openclaw/" + bindingAgent
}
```

同步把 Step 1 中的测试改为调用导出名：

```go
func TestOpenclawModel(t *testing.T) {
	cases := []struct{ binding, want string }{
		{"openclaw", "openclaw/default"},
		{"hermes", "openclaw/hermes"},
		{"claude-acp", "openclaw/claude-acp"},
		{"coding", "openclaw/coding"},
	}
	for _, c := range cases {
		if got := OpenclawModel(c.binding); got != c.want {
			t.Errorf("OpenclawModel(%q) = %q, want %q",
				c.binding, got, c.want)
		}
	}
}
```

- [ ] **Step 4: 运行 harness 包全部测试**

Run: `go test ./internal/harness/ -v`
Expected: PASS（含 Task 2 的全部测试；hermes_client_test.go / openclaw_client_test.go 不受影响）。

- [ ] **Step 5: Commit**

```bash
git add internal/harness/registry.go internal/harness/registry_test.go
git commit -m "refactor(harness): remove managed factory layer, keep openclaw model mapping"
```

---

### Task 4: main.go 平铺装配 + 启用状态扩展

**Files:**
- Modify: `cmd/oma-server/main.go:172-186,374`
- Modify: `internal/harness/registry.go`（`ManagedHarnessState`/`ManagedState`）
- Modify: `internal/api/harness_config_test.go`
- Test: `go build ./...`

**Interfaces:**
- Consumes: Task 2 的 `RegistryConfig.Hermes`/`OpenClaw` 字段、Task 3 的 `openclawModel`
- Produces: `HarnessState` 结构体（含 DeepSeek 字段——Task 13 前端消费）、`HarnessAvailability(oc, hc, ds)` 函数。`Deps.ManagedHarness` 字段名不变（router.go:72），仅类型演进

- [ ] **Step 1: 写失败测试（状态计算新签名）**

替换 `internal/api/harness_config_test.go` 中的 `TestManagedState_ReflectsDisabledAndURL` 为：

```go
func TestHarnessAvailability(t *testing.T) {
	cases := []struct {
		name                   string
		oc                     harness.OpenClawConfig
		hc                     harness.HermesConfig
		ds                     harness.DeepSeekConfig
		wantOC, wantHC, wantDS bool
	}{
		{
			name:   "disabled overrides URL",
			oc:     harness.OpenClawConfig{GatewayURL: "http://x", Disabled: true},
			hc:     harness.HermesConfig{GatewayURL: "http://y"},
			ds:     harness.DeepSeekConfig{GatewayURL: "http://z"},
			wantOC: false, wantHC: true, wantDS: true,
		},
		{
			name:   "empty URL counts as disabled",
			oc:     harness.OpenClawConfig{},
			hc:     harness.HermesConfig{},
			ds:     harness.DeepSeekConfig{},
			wantOC: false, wantHC: false, wantDS: false,
		},
		{
			name:   "URL without disabled flag is enabled",
			oc:     harness.OpenClawConfig{GatewayURL: "http://x"},
			hc:     harness.HermesConfig{GatewayURL: "http://y"},
			ds:     harness.DeepSeekConfig{GatewayURL: "http://z"},
			wantOC: true, wantHC: true, wantDS: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := harness.HarnessAvailability(c.oc, c.hc, c.ds)
			if got.OpenClaw != c.wantOC || got.Hermes != c.wantHC ||
				got.DeepSeek != c.wantDS {
				t.Errorf("HarnessAvailability = %+v, want OC=%v HC=%v DS=%v",
					got, c.wantOC, c.wantHC, c.wantDS)
			}
		})
	}
}
```

同文件其余三个端点测试（`TestHarnessConfigEndpoint_AllEnabled` 等）把 `harness.ManagedHarnessState{...}` 字面量改为 `harness.HarnessState{...}`（字段名不变，仅类型名换）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/api/ -run 'TestHarnessAvailability|TestHarnessConfigEndpoint' -v`
Expected: 编译失败——`DeepSeekConfig`/`HarnessAvailability`/`HarnessState` 未定义。

- [ ] **Step 3: 实现状态结构体演进**

在 `registry.go` 中把 `ManagedHarnessState`/`ManagedState`（registry.go:269-287）替换为：

```go
// HarnessState describes which gateway harnesses are currently enabled.
// Surfaced via the /v1/config/harnesses endpoint so the console UI can
// grey out disabled options in the Harness dropdown.
type HarnessState struct {
	OpenClaw bool `json:"openclaw"`
	Hermes   bool `json:"hermes"`
	DeepSeek bool `json:"deepseek"`
}

// HarnessAvailability returns the on/off state of each gateway harness
// based on the configs used to build the clients. A harness is considered
// enabled when it is not Disabled AND its GatewayURL is configured.
func HarnessAvailability(
	oc OpenClawConfig,
	hc HermesConfig,
	ds DeepSeekConfig,
) HarnessState {
	return HarnessState{
		OpenClaw: !oc.Disabled && oc.GatewayURL != "",
		Hermes:   !hc.Disabled && hc.GatewayURL != "",
		DeepSeek: !ds.Disabled && ds.GatewayURL != "",
	}
}
```

`DeepSeekConfig` 在 Task 12 才定义——为使本任务可编译，先在 `registry.go` 的 `HermesConfig` 之后加入占位定义（Task 12 直接复用，不重复定义）：

```go
// DeepSeekConfig holds the configuration for the DeepSeek harness (dsh
// web) gateway integration. When GatewayURL is empty or Disabled the
// registry falls back to the ManagedClient stub and the console greys
// out the DeepSeek option.
type DeepSeekConfig struct {
	// GatewayURL is the dsh web gateway base URL, e.g.
	// "http://dsh:3080". No trailing slash.
	GatewayURL string
	// Token is the optional bearer token. dsh web ships without auth;
	// the field exists for future upstream support and reverse proxies.
	Token string
	// Disabled toggles the DeepSeek harness off.
	Disabled bool
}
```

- [ ] **Step 4: 更新 router Deps 字段类型**

`internal/api/router.go:69-72` 的 `ManagedHarness harness.ManagedHarnessState` 改为 `ManagedHarness harness.HarnessState`；`internal/api/harness_config.go:16` 的 `mountHarnessConfigRoutes(r chi.Router, state harness.ManagedHarnessState)` 形参类型同步改为 `harness.HarnessState`，并把文件头注释中的 "(OpenClaw / Hermes)" 改为 "(OpenClaw / Hermes / DeepSeek)"。

- [ ] **Step 5: 重写 main.go 装配段**

`cmd/oma-server/main.go:172-190` 替换为：

```go
	openclawCfg := harness.OpenClawConfig{
		GatewayURL: os.Getenv("OMA_OPENCLAW_GATEWAY_URL"),
		Token:      os.Getenv("OMA_OPENCLAW_TOKEN"),
		Disabled:   envDisabled("OMA_OPENCLAW_ENABLED"),
	}
	hermesCfg := harness.HermesConfig{
		GatewayURL: os.Getenv("OMA_HERMES_GATEWAY_URL"),
		Token:      os.Getenv("OMA_HERMES_API_KEY"),
		Disabled:   envDisabled("OMA_HERMES_ENABLED"),
	}
	deepseekCfg := harness.DeepSeekConfig{
		GatewayURL: os.Getenv("OMA_DEEPSEEK_GATEWAY_URL"),
		Token:      os.Getenv("OMA_DEEPSEEK_TOKEN"),
		Disabled:   envDisabled("OMA_DEEPSEEK_ENABLED"),
	}
	harnessRegistry := harness.NewRegistry(harness.RegistryConfig{
		Default:  harnessClient,
		Force:    harnessForceOverride,
		Hermes:   harnessGatewayClient(hermesCfg.Disabled, hermesCfg.GatewayURL,
			func() harness.Client {
				return &harness.HermesClient{
					GatewayURL: hermesCfg.GatewayURL,
					Token:      hermesCfg.Token,
					Model:      "hermes-agent",
				}
			}),
		OpenClaw: harnessGatewayClient(openclawCfg.Disabled, openclawCfg.GatewayURL,
			func() harness.Client {
				return &harness.OpenClawClient{
					GatewayURL: openclawCfg.GatewayURL,
					Token:      openclawCfg.Token,
					Agent:      harness.OpenclawModel("openclaw"),
				}
			}),
	})
```

并在 main.go 文件内（`envDisabled` 附近，main.go:419 前后）加入：

```go
// harnessGatewayClient returns nil when the gateway is disabled or
// unconfigured — the harness.Registry substitutes the ManagedClient stub,
// preserving pre-flattening fallback behavior.
func harnessGatewayClient(
	disabled bool,
	gatewayURL string,
	build func() harness.Client,
) harness.Client {
	if disabled || gatewayURL == "" {
		return nil
	}
	return build()
}
```

注意：`openclawModel` 需要导出供 main.go 调用——在 Task 3 已定义的小写版本基础上，把 `registry.go` 中的 `openclawModel` 改名为导出函数 `OpenclawModel`（同步更新 registry_test.go 中 `TestOpenclawModel` 的调用）。

`main.go:374` 改为：

```go
		ManagedHarness: harness.HarnessAvailability(
			openclawCfg, hermesCfg, deepseekCfg),
```

（`deepseekCfg` 此时已声明；DeepSeek 客户端装配在 Task 12 加入同一 `RegistryConfig` 字面量。）

- [ ] **Step 6: 运行全部后端测试**

Run: `go build ./... && go test ./...`
Expected: PASS。重点确认 `internal/api`（harness_config、agents_managed）、`internal/harness`、`internal/session`（大量 `DefaultOnly` 用户）全绿。

- [ ] **Step 7: Commit**

```bash
git add cmd/oma-server/main.go internal/harness/registry.go internal/harness/registry_test.go internal/api/router.go internal/api/harness_config.go internal/api/harness_config_test.go
git commit -m "refactor(harness): assemble flat gateway clients in main, extend availability state"
```

---

### Task 5: API 校验 — 一级 Kind 写入 + legacy 兼容

**Files:**
- Modify: `internal/api/agents.go:202-230`
- Test: `internal/api/agents_managed_test.go`（保留）、`internal/api/agents_kinds_test.go`（新建）

**Interfaces:**
- Consumes: Task 2 的 Kind 集合（校验值域必须与 `normalizeKind` 一致）
- Produces: `validateHarnessBinding(harnessKind string, runtimeBinding json.RawMessage) string`（替代 `validateManagedBinding`）。创建路径（agents.go:112）与更新路径（agents.go:202-206）的校验入口

- [ ] **Step 1: 写失败测试（新格式写入）**

新建 `internal/api/agents_kinds_test.go`：

```go
package api_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentCreate_FlatKinds(t *testing.T) {
	for _, kind := range []string{"hermes", "openclaw", "deepseek"} {
		t.Run(kind, func(t *testing.T) {
			handler := testRouter(t)
			body := `{
				"name":"flat-` + kind + `",
				"_oma":{"harness":"` + kind + `"}
			}`
			req := httptest.NewRequest(
				http.MethodPost, "/v1/agents", bytes.NewBufferString(body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("kind=%q status=%d body=%s",
					kind, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestAgentCreate_UnknownKind_Rejected(t *testing.T) {
	handler := testRouter(t)
	body := `{"name":"bad-kind","_oma":{"harness":"nonsense"}}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got status=%d body=%s",
			rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown harness") {
		t.Fatalf("expected 'unknown harness' error, got body=%s",
			rec.Body.String())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/api/ -run 'TestAgentCreate_FlatKinds|TestAgentCreate_UnknownKind_Rejected' -v`
Expected: FAIL——`deepseek` 不在校验白名单（400），`nonsense` 当前被接受（201，因为非 managed 的任意 harness 字符串都放行）。

- [ ] **Step 3: 实现校验演进**

把 `internal/api/agents.go` 的 `validateManagedBinding`（agents.go:210-230）替换为：

```go
// flatHarnessKinds lists the one-level harness kinds the Agent API
// accepts on write. Must stay in sync with harness.normalizeKind.
var flatHarnessKinds = map[string]bool{
	"default-loop": true,
	"pipy":         true, // legacy alias, normalized at dispatch
	"hermes":       true,
	"openclaw":     true,
	"deepseek":     true,
	"fake":         true,
	"acp-proxy":    true, // ACP overlay path — validation lives elsewhere
	"managed":      true, // legacy two-layer form, validated below
}

// validateHarnessBinding returns a non-empty error string when the
// harness kind is unknown, or a "managed" kind carries an invalid
// runtime_binding. Empty string means OK. Accepts both the flat forms
// (harness="hermes") and the legacy two-layer form
// (harness="managed" + runtime_binding.agent) so pre-flattening data
// and external writers keep working.
func validateHarnessBinding(
	harnessKind string,
	runtimeBinding json.RawMessage,
) string {
	if harnessKind == "" {
		return ""
	}
	if !flatHarnessKinds[harnessKind] {
		return "unknown harness kind " + harnessKind
	}
	if harnessKind != "managed" {
		return ""
	}
	b, err := harness.ParseManagedBinding(runtimeBinding)
	if err != nil {
		return err.Error()
	}
	if !harness.IsKnownAgent(b.Agent) {
		return "managed runtime_binding.agent must be one of " +
			joinKnownAgents() + ", got " + b.Agent
	}
	return ""
}
```

调用点两处：

- 创建路径 agents.go:112：`if msg := validateManagedBinding(input.Harness, input.RuntimeBinding); msg != "" {` 改为 `if msg := validateHarnessBinding(input.Harness, input.RuntimeBinding); msg != "" {`
- 更新路径 agents.go:202-206：

```go
	if patch.Harness != nil && *patch.Harness == "managed" {
		if msg := validateManagedBinding(*patch.Harness, patch.RuntimeBinding); msg != "" {
			return store.UpdateAgentInput{}, msg
		}
	}
```

改为（对任何非空 harness 都校验）：

```go
	if patch.Harness != nil {
		if msg := validateHarnessBinding(*patch.Harness, patch.RuntimeBinding); msg != "" {
			return store.UpdateAgentInput{}, msg
		}
	}
```

- [ ] **Step 4: 运行 API 测试（新旧格式全过）**

Run: `go test ./internal/api/ -run 'TestAgentCreate_FlatKinds|TestAgentCreate_UnknownKind_Rejected|TestAgentCreate_Managed|TestAgentUpdate_FlipToManaged' -v`
Expected: PASS——新格式 201、未知 Kind 400、既有 `agents_managed_test.go` 的 legacy 用例（managed+hermes 201、managed+bogus 400、缺 binding 400、claude-acp/codex-acp 201）全部保持原结果。

- [ ] **Step 5: 全量回归**

Run: `go test ./...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/api/agents.go internal/api/agents_kinds_test.go
git commit -m "feat(api): validate flat harness kinds, keep legacy managed writes"
```

---

### Task 6: 前端表单扁平化 + 文档决策记录

**Files:**
- Modify: `console/src/pages/agents/AgentFormDialog.tsx:118,332-353,448-467,492-505,553,960-984,1110-1195`
- Modify: `console/src/types/agent.ts:32-44`
- Modify: `docs/plans/pluggable-harness-zh.md`（实施状态段后追加决策记录）

**Interfaces:**
- Consumes: Task 5 的写入格式（`_oma.harness` 直接为 Kind）；Task 4 的状态端点（响应新增 `deepseek` 字段，本任务读取但 Task 13 才用它置灰）
- Produces: 扁平化后的 wire 体生成（create/formToConfig）与回显解析（switchMode code→form、编辑预填）

- [ ] **Step 1: 更新类型定义**

`console/src/types/agent.ts:32-44` 替换为：

```ts
  /** Console-only enrichment from the OMA control plane: scratch/aux model
   *  selection, harness binding, appendable prompt presets. Not on the
   *  wire-format AgentConfig (those fields live in OMA-private storage).
   *
   *  Flat harness kinds (2026-08 flattening): "hermes" | "openclaw" |
   *  "deepseek" select a gateway harness with no runtime_binding.
   *  "acp-proxy" keeps `runtime_binding.runtime_id + acp_agent_id`.
   *  Legacy rows may still carry `harness: "managed"` +
   *  `runtime_binding.agent` — normalized by the server at dispatch.
   */
  _oma?: {
    aux_model?: { id: string; speed?: string };
    harness?:
      | "acp-proxy"
      | "managed"
      | "hermes"
      | "openclaw"
      | "deepseek"
      | (string & {});
    runtime_binding?:
      | { runtime_id: string; acp_agent_id: string; local_skill_blocklist?: string[] }
      | { agent: string };
    appendable_prompts?: string[];
  };
```

- [ ] **Step 2: wire 体生成改为一级 Kind**

`AgentFormDialog.tsx:332-353`（create 内）替换为：

```tsx
      // Harness binding — exactly one of two shapes reaches the wire:
      //   1. runtimeId + acpAgentId → acp-proxy (user's own daemon)
      //   2. managedAgent           → flat gateway kind (hermes/openclaw/
      //      deepseek) as _oma.harness directly
      //   3. neither                → default cloud loop (no _oma block)
      // The harness dropdown enforces mutual exclusion between (1) and (2).
      if (form.runtimeId && form.acpAgentId) {
        payload._oma = {
          harness: "acp-proxy",
          runtime_binding: {
            runtime_id: form.runtimeId,
            acp_agent_id: form.acpAgentId,
            ...(form.localSkillBlocklist.length > 0
              ? { local_skill_blocklist: form.localSkillBlocklist }
              : {}),
          },
        };
      } else if (form.managedAgent) {
        payload._oma = { harness: form.managedAgent };
      }
```

`AgentFormDialog.tsx:448-467`（formToConfig 内）同样把 `else if (form.managedAgent)` 分支替换为：

```tsx
    } else if (form.managedAgent) {
      config._oma = { harness: form.managedAgent };
    }
```

- [ ] **Step 3: code→form 回显解析兼容新旧格式**

`AgentFormDialog.tsx:492-505`（switchMode 内）的 harness/rb/managedAgent 解析替换为：

```tsx
        const oma = (parsed._oma ?? parsed) as Record<string, unknown>;
        const harness = typeof oma.harness === "string" ? oma.harness : "";
        const rb = oma.runtime_binding as
          | {
              runtime_id?: string;
              acp_agent_id?: string;
              local_skill_blocklist?: string[];
              agent?: string;
            }
          | undefined;
        // Flat kinds select the gateway directly; legacy rows carry
        // harness="managed" + runtime_binding.agent and are normalized
        // here so old configs still edit correctly.
        const flatKinds = ["hermes", "openclaw", "deepseek"];
        const managedAgent = flatKinds.includes(harness)
          ? harness
          : harness === "managed" &&
              (rb?.agent === "hermes" || rb?.agent === "openclaw")
            ? rb.agent
            : "";
```

`AgentFormDialog.tsx:553` 的类型断言同步扩宽：

```tsx
          managedAgent: managedAgent as "" | "hermes" | "openclaw" | "deepseek",
```

编辑对话框的预填逻辑（搜索 `INITIAL_FORM` 之外的 `setForm` 编辑入口——与 switchMode 同构的那处 `rb?.agent` 解析）应用相同的 `flatKinds` 归一化。若编辑入口直接复用 switchMode 的解析则跳过。

- [ ] **Step 4: 下拉与状态类型**

`AgentFormDialog.tsx:960-963` 状态类型扩展：

```tsx
  const [managedHarness, setManagedHarness] = useState<{
    openclaw: boolean;
    hermes: boolean;
    deepseek: boolean;
  }>({ openclaw: true, hermes: true, deepseek: true });
```

`AgentFormDialog.tsx:966-973` 的 fetch 处理改为：

```tsx
    api<{ openclaw?: boolean; hermes?: boolean; deepseek?: boolean }>(
      "/v1/config/harnesses",
    )
      .then((res) => {
        if (cancelled) return;
        const openclaw = res?.openclaw !== false;
        const hermes = res?.hermes !== false;
        const deepseek = res?.deepseek !== false;
        // eslint-disable-next-line no-console
        console.info(
          "[harness-config] openclaw=%s hermes=%s deepseek=%s raw=%o",
          openclaw, hermes, deepseek, res,
        );
        setManagedHarness({ openclaw, hermes, deepseek });
      })
```

下拉的 value 计算与 onChange（`AgentFormDialog.tsx:1123-1159`）：哨兵值保持 `__managed_<agent>__` 形态不变（仅选项集变化），但 `AgentFormDialog.tsx:1140` 的断言扩宽：

```tsx
              const agent = v.slice("__managed_".length, -2) as
                | "hermes" | "openclaw" | "deepseek";
```

选项区（`AgentFormDialog.tsx:1163-1173`）在 OpenClaw 选项后追加：

```tsx
            <SelectOption value="__managed_deepseek__" disabled={!managedHarness.deepseek}>
              DeepSeek
              {!managedHarness.deepseek ? " — disabled" : ""}
            </SelectOption>
```

模型提示（`AgentFormDialog.tsx:1055-1061`）与选中说明（1189-1195）中的三元 `form.managedAgent === "hermes" ? "Hermes" : "OpenClaw"` 改为映射：

```tsx
            {({ hermes: "Hermes", openclaw: "OpenClaw", deepseek: "DeepSeek" } as const)[
              form.managedAgent
            ]}
```

（两处同样处理；`INITIAL_FORM` 的 `managedAgent` 类型（118 行）同步扩宽为 `"" | "hermes" | "openclaw" | "deepseek"`。）

- [ ] **Step 5: 前端类型检查与测试**

Run（console 目录）: `npm run typecheck && npm test`
Expected: PASS。若既有测试断言旧 wire 体（`runtime_binding: { agent }`），按新格式 `{ harness }` 更新断言。

- [ ] **Step 6: 补决策记录**

在 `docs/plans/pluggable-harness-zh.md` 的"实施状态"段（第 8 行之后）追加：

```markdown
## 决策变更记录

### 2026-08-20：扁平化——推翻原决策 #1（两种类型，多个 Agent）

原决策 #1 将 openclaw/hermes/claude 定为 managed 下的 agent 而非独立 Kind，理由是
对齐 open-ma 模型并为 Phase 4 per-tenant daemon 池预留二级维度。实际情况：

- Phase 4 daemon 池从未实施（`system_runtimes` 表仅有 schema 无代码）
- hermes/openclaw 均为直连共享网关的无状态 HTTP 客户端，与 piPy 的
  `HTTPClient` 完全同构——两层结构的设计前提已不存在
- 二级分发（`ManagedFactory`）仅剩一个 switch 薄壳，"managed"命名名不副实

变更：hermes/openclaw/deepseek 升为一级 Kind（`_oma.harness` 直接取值）；
存量 `managed` + `runtime_binding` 数据经 `ClientFor` 分发期归一化兼容，
无数据库迁移。claude-acp/codex-acp 保持 legacy 路径不动。
详见 `docs/superpowers/specs/2026-08-20-deepseek-harness-integration-design.md`。
```

- [ ] **Step 7: Commit**

```bash
git add console/src/pages/agents/AgentFormDialog.tsx console/src/types/agent.ts docs/plans/pluggable-harness-zh.md
git commit -m "refactor(console): flatten harness dropdown to one-level kinds"
```

---

### Task 7: 扁平化阶段验收（回归门禁）

**Files:** 无新增

- [ ] **Step 1: 后端全量测试**

Run: `go test ./...`
Expected: 全 PASS。

- [ ] **Step 2: 前端全量测试 + 类型检查**

Run（console 目录）: `npm run typecheck && npm test`
Expected: PASS。

- [ ] **Step 3: 手工冒烟（本地 dev）**

启动后端与 console（`./start-console.sh` 或既有流程），验证：

1. 创建 hermes agent（新格式）→ 保存成功，详情页回显正确
2. 既有 managed+hermes agent（若库中有）编辑回显不丢 harness
3. `curl http://127.0.0.1:8787/v1/config/harnesses`（带鉴权头）返回 `{openclaw, hermes, deepseek}` 三字段

记录结果；全部通过则扁平化阶段关闭。

---

### Task 8: DeepSeekClient — RPC 骨架

**Files:**
- Create: `internal/harness/deepseek_client.go`
- Test: `internal/harness/deepseek_client_test.go`

**Interfaces:**
- Consumes: Task 1 spike 报告的端点映射；`TurnRequest`/`TurnResponse`（client.go:43-75）；`extractLastUserMessage`/`agentMessageEvent`/`randomOCID`（openclaw_client.go:492-533）；`logTurn`（turn_telemetry.go:82）
- Produces: `DeepSeekClient` 结构体（实现 `Client` + `StreamingClient`）。RPC 信封类型 `dshRpcResponse`。本任务只实现非流式 `RunTurn`；WS 流式在 Task 9

**重要**：下面的端点名 `session/open`、`session/prompt`、args 字段名（`sessionId`/`content`/`mode`）是依据源码线索的占位形态——实现时**必须以 Task 1 spike 报告确认的确切报文为准**，仅信封结构（`{args}` 请求、`{ok,value}`/`{ok:false,error}` 响应）是已确认的。

- [ ] **Step 1: 写失败测试（RPC 往返）**

新建 `internal/harness/deepseek_client_test.go`：

```go
package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeDshGateway answers dsh-web-style RPC calls.
func fakeDshGateway(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session/prompt", func(
		w http.ResponseWriter, r *http.Request,
	) {
		var body struct {
			Args map[string]any `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode prompt body: %v", err)
		}
		if body.Args["sessionId"] == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": false,
				"error": map[string]any{
					"code": "input-invalid", "message": "sessionId required",
				},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "value": map[string]any{"accepted": true},
		})
	})
	mux.HandleFunc("POST /api/session/wait", func(
		w http.ResponseWriter, r *http.Request,
	) {
		// Test double for the terminal-event poll used by RunTurn:
		// return one assistant/message event with usage.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "value": map[string]any{
				"events": []map[string]any{
					{
						"type": "assistant/message", "seq": 1,
						"data": map[string]any{
							"message": map[string]any{
								"content": []map[string]any{
									{"type": "text", "text": "hello from dsh"},
								},
							},
							"usage": map[string]any{
								"inputTokens": 10, "outputTokens": 5,
							},
						},
					},
					{
						"type": "turn/end", "seq": 2,
						"data": map[string]any{
							"reason": map[string]any{"kind": "completed"},
						},
					},
				},
			},
		})
	})
	return httptest.NewServer(mux)
}

func TestDeepSeekClient_RunTurn(t *testing.T) {
	srv := fakeDshGateway(t)
	defer srv.Close()

	c := &DeepSeekClient{GatewayURL: srv.URL}
	resp, err := c.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-1",
		Events: []json.RawMessage{json.RawMessage(
			`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`)},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Fatalf("expected at least one mapped event")
	}
	var msg struct {
		Type    string `json:"type"`
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	_ = json.Unmarshal(resp.Events[0], &msg)
	if msg.Type != "agent.message" {
		t.Fatalf("first event type = %q, want agent.message", msg.Type)
	}
	if msg.Content[0].Text != "hello from dsh" {
		t.Fatalf("text = %q", msg.Content[0].Text)
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 10 ||
		resp.Usage.OutputTokens != 5 {
		t.Fatalf("usage = %+v", resp.Usage)
	}
}

func TestDeepSeekClient_RunTurn_RpcError(t *testing.T) {
	srv := fakeDshGateway(t)
	defer srv.Close()

	c := &DeepSeekClient{GatewayURL: srv.URL}
	_, err := c.RunTurn(context.Background(), TurnRequest{
		SessionID: "", // forces input-invalid from the fake gateway
		Events:    []json.RawMessage{},
	})
	if err == nil || !strings.Contains(err.Error(), "sessionId required") {
		t.Fatalf("expected rpc error surfaced, got %v", err)
	}
}

func TestDeepSeekClient_RunTurn_GatewayDown(t *testing.T) {
	c := &DeepSeekClient{GatewayURL: "http://127.0.0.1:1"}
	_, err := c.RunTurn(context.Background(), TurnRequest{SessionID: "s"})
	if err == nil {
		t.Fatalf("expected connection error")
	}
}
```

注：`session/wait` 是测试双——真实实现按 spike 结果选择"轮询事件 RPC"或"阻塞式等待端点"；若 spike 确认只有 WS 下行，则 `RunTurn` 改为内部复用 Task 9 的 WS 收集器（本测试的 fake 相应改为 WS 端点）。

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/harness/ -run TestDeepSeekClient -v`
Expected: 编译失败——`DeepSeekClient` 未定义。

- [ ] **Step 3: 实现 DeepSeekClient RPC 骨架**

新建 `internal/harness/deepseek_client.go`：

```go
package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DeepSeekClient calls the DeepSeek harness (dsh) web gateway: Typert
// RPC at POST /api/<namespace>/<method> with {args} request bodies and
// {ok,value}/{ok:false,error} responses, plus a session event feed
// (see RunTurnStream). All dsh protocol knowledge is confined to this
// file — the upstream is developer preview and the wire format may
// drift between versions (image build pins the dsh commit).
type DeepSeekClient struct {
	// GatewayURL is the dsh web base URL, e.g. "http://dsh:3080".
	GatewayURL string
	// Token is an optional bearer token (dsh ships without auth).
	Token string
	// HTTP overrides the transport; nil uses a 10-minute timeout client.
	HTTP *http.Client
}

// dshRpcResponse is the Typert gateway response envelope.
type dshRpcResponse struct {
	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value,omitempty"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// rpc posts one Typert RPC call and decodes the envelope.
func (c *DeepSeekClient) rpc(
	ctx context.Context,
	endpoint string,
	args map[string]any,
	out any,
) error {
	body, err := json.Marshal(map[string]any{"args": args})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.GatewayURL+"/api/"+endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("deepseek rpc %s status=%d: %s",
			endpoint, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var env dshRpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return fmt.Errorf("deepseek rpc %s decode: %w", endpoint, err)
	}
	if !env.OK {
		msg := "unknown error"
		if env.Error != nil {
			msg = env.Error.Code + ": " + env.Error.Message
		}
		return fmt.Errorf("deepseek rpc %s: %s", endpoint, msg)
	}
	if out != nil {
		return json.Unmarshal(env.Value, out)
	}
	return nil
}

func (c *DeepSeekClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 10 * time.Minute}
}

// RunTurn implements Client. It prompts the dsh session and collects
// events until turn/end, mapping them onto the oma session vocabulary.
func (c *DeepSeekClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	start := time.Now()
	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	if err := c.rpc(ctx, "session/prompt", map[string]any{
		"sessionId": req.SessionID,
		"content":   []map[string]any{{"type": "text", "text": userText}},
		"mode":      "queue",
	}, nil); err != nil {
		logTurn("backend", "deepseek", "session", req.SessionID,
			"duration_ms", time.Since(start).Milliseconds(), "error", err)
		return TurnResponse{}, err
	}

	events, usage, err := c.waitTurn(ctx, req.SessionID)
	if err != nil {
		logTurn("backend", "deepseek", "session", req.SessionID,
			"duration_ms", time.Since(start).Milliseconds(), "error", err)
		return TurnResponse{}, err
	}
	logTurn("backend", "deepseek", "session", req.SessionID,
		"duration_ms", time.Since(start).Milliseconds())
	return TurnResponse{Events: events, Usage: usage}, nil
}
```

`waitTurn` 与事件映射在 Step 4 实现。

- [ ] **Step 4: 实现 waitTurn 与 SessionEvent → oma 映射**

在 `deepseek_client.go` 追加（映射目标与 hermes_client.go:127 的 doRun 对齐；dsh 事件类型来自 `known-event-types.ts`）：

```go
// dshSessionEvent is the dsh SessionEvent envelope
// (packages/core/session/src/types.ts): { type, seq, time, data }.
type dshSessionEvent struct {
	Type string          `json:"type"`
	Seq  int             `json:"seq"`
	Data json.RawMessage `json:"data"`
}

// waitTurn blocks until the dsh session reaches turn/end for the
// prompted turn and returns mapped oma events plus token usage.
// Endpoint shape follows the Task 1 spike report.
func (c *DeepSeekClient) waitTurn(
	ctx context.Context,
	sessionID string,
) ([]json.RawMessage, *TurnUsage, error) {
	var payload struct {
		Events []dshSessionEvent `json:"events"`
	}
	if err := c.rpc(ctx, "session/wait", map[string]any{
		"sessionId": sessionID,
	}, &payload); err != nil {
		return nil, nil, err
	}

	var events []json.RawMessage
	var usage *TurnUsage
	msgID := randomOCID()
	emitted := false

	for _, ev := range payload.Events {
		switch ev.Type {
		case "tool/call":
			var d struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			mapped, err := agentToolUseEvent(d.Name, "")
			if err != nil {
				return events, usage, err
			}
			events = append(events, mapped)

		case "tool/result":
			var d struct {
				Error *struct {
					Name string `json:"name"`
				} `json:"error"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			content := "(completed)"
			if d.Error != nil {
				content = "(failed: " + d.Error.Name + ")"
			}
			mapped, err := agentToolResultEvent("", content)
			if err != nil {
				return events, usage, err
			}
			events = append(events, mapped)

		case "assistant/message":
			var d struct {
				Message struct {
					Content []struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"message"`
				Usage *struct {
					InputTokens  int `json:"inputTokens"`
					OutputTokens int `json:"outputTokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			var sb strings.Builder
			for _, part := range d.Message.Content {
				if part.Type == "text" {
					sb.WriteString(part.Text)
				}
			}
			mapped, err := agentMessageEvent(msgID, sb.String())
			if err != nil {
				return events, usage, err
			}
			events = append(events, mapped)
			emitted = true
			if d.Usage != nil {
				usage = &TurnUsage{
					InputTokens:  d.Usage.InputTokens,
					OutputTokens: d.Usage.OutputTokens,
				}
			}

		case "turn/end":
			var d struct {
				Reason struct {
					Kind string `json:"kind"`
				} `json:"reason"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			if d.Reason.Kind == "error" {
				return events, usage, fmt.Errorf(
					"deepseek turn ended with error")
			}
		}
	}

	if !emitted {
		mapped, err := agentMessageEvent(msgID, "")
		if err != nil {
			return events, usage, err
		}
		events = append(events, mapped)
	}
	return events, usage, nil
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `go test ./internal/harness/ -run TestDeepSeekClient -v`
Expected: PASS（三个测试）。

- [ ] **Step 6: Commit**

```bash
git add internal/harness/deepseek_client.go internal/harness/deepseek_client_test.go
git commit -m "feat(harness): add DeepSeekClient RPC skeleton with event mapping"
```

---

### Task 9: DeepSeekClient — WebSocket 流式

**Files:**
- Modify: `internal/harness/deepseek_client.go`
- Modify: `internal/harness/deepseek_client_test.go`

**Interfaces:**
- Consumes: Task 8 的 `rpc`/事件映射；Task 1 spike 报告的 mux-frame 解码方式
- Produces: `DeepSeekClient.RunTurnStream`（`StreamingClient`）。`oma-server` 的 session machine 经 `harness.RunTurnStreaming`（client.go:133-152）自动优先走流式

**重要**：mux-frame 的帧编码（长度前缀/裸 JSON）以 spike 报告为准；下面按"裸 JSON 消息 + 帧内 sessionId 过滤"形态实现，若 spike 发现二进制分帧需替换 `readFrames`。

- [ ] **Step 1: 写失败测试（WS 事件流）**

在 `deepseek_client_test.go` 追加：

```go
func TestDeepSeekClient_RunTurnStream(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/session/prompt", func(
		w http.ResponseWriter, r *http.Request,
	) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "value": map[string]any{"accepted": true},
		})
	})
	mux.HandleFunc("/api/events.mux", func(
		w http.ResponseWriter, r *http.Request,
	) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		frames := []string{
			`{"sessionId":"sess-9","event":{"type":"assistant/chunk","seq":1,"data":{"chunk":{"type":"text-delta","text":"hel"}}}}`,
			`{"sessionId":"sess-9","event":{"type":"assistant/chunk","seq":2,"data":{"chunk":{"type":"text-delta","text":"lo"}}}}`,
			`{"sessionId":"other","event":{"type":"assistant/chunk","seq":1,"data":{}}}`,
			`{"sessionId":"sess-9","event":{"type":"assistant/message","seq":3,"data":{"message":{"content":[{"type":"text","text":"hello"}]},"usage":{"inputTokens":3,"outputTokens":2}}}}`,
			`{"sessionId":"sess-9","event":{"type":"turn/end","seq":4,"data":{"reason":{"kind":"completed"}}}}`,
		}
		for _, f := range frames {
			_ = conn.WriteMessage(1, []byte(f))
		}
		// Hold the connection until the client disconnects.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := &DeepSeekClient{GatewayURL: srv.URL}
	var types []string
	err := c.RunTurnStream(context.Background(), TurnRequest{
		SessionID: "sess-9",
		Events: []json.RawMessage{json.RawMessage(
			`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`)},
	}, func(ev json.RawMessage) error {
		var meta struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(ev, &meta)
		types = append(types, meta.Type)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	want := []string{"agent.message", "agent.message", "agent.message"}
	if len(types) != len(want) {
		t.Fatalf("events = %v, want %v", types, want)
	}
}
```

文件顶部追加 import 与测试用 upgrader：

```go
import (
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/harness/ -run TestDeepSeekClient_RunTurnStream -v`
Expected: 编译失败——`RunTurnStream` 未定义。

- [ ] **Step 3: 实现 RunTurnStream**

在 `deepseek_client.go` 追加（import 增加 `"net/url"`、`github.com/gorilla/websocket`）：

```go
// RunTurnStream implements StreamingClient. It prompts the dsh session
// over RPC, then consumes /api/events.mux until turn/end, mapping each
// SessionEvent onto the oma vocabulary as it arrives.
func (c *DeepSeekClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	start := time.Now()
	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}
	if err := c.rpc(ctx, "session/prompt", map[string]any{
		"sessionId": req.SessionID,
		"content":   []map[string]any{{"type": "text", "text": userText}},
		"mode":      "queue",
	}, nil); err != nil {
		return err
	}

	wsURL := strings.Replace(c.GatewayURL, "http", "ws", 1) +
		"/api/events.mux"
	header := http.Header{}
	if c.Token != "" {
		header.Set("Authorization", "Bearer "+c.Token)
	}
	dialer := websocket.DefaultDialer
	conn, _, err := dialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return fmt.Errorf("deepseek events dial: %w", err)
	}
	defer conn.Close()

	msgID := randomOCID()
	var accumulated strings.Builder
	emitted := false

	for {
		frame, err := readFrame(ctx, conn)
		if err != nil {
			return err
		}
		if frame.SessionID != req.SessionID {
			continue
		}
		ev := frame.Event
		switch ev.Type {
		case "assistant/chunk":
			var d struct {
				Chunk struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"chunk"`
			}
			if json.Unmarshal(ev.Data, &d) != nil ||
				d.Chunk.Type != "text-delta" || d.Chunk.Text == "" {
				continue
			}
			accumulated.WriteString(d.Chunk.Text)
			mapped, mapErr := agentMessageEvent(msgID, accumulated.String())
			if mapErr != nil {
				return mapErr
			}
			if err := onEvent(mapped); err != nil {
				return err
			}
			emitted = true

		case "tool/call":
			var d struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			mapped, mapErr := agentToolUseEvent(d.Name, "")
			if mapErr != nil {
				return mapErr
			}
			if err := onEvent(mapped); err != nil {
				return err
			}

		case "tool/result":
			var d struct {
				Error *struct {
					Name string `json:"name"`
				} `json:"error"`
			}
			if json.Unmarshal(ev.Data, &d) != nil {
				continue
			}
			content := "(completed)"
			if d.Error != nil {
				content = "(failed: " + d.Error.Name + ")"
			}
			mapped, mapErr := agentToolResultEvent("", content)
			if mapErr != nil {
				return mapErr
			}
			if err := onEvent(mapped); err != nil {
				return err
			}

		case "turn/end":
			var d struct {
				Reason struct {
					Kind string `json:"kind"`
				} `json:"reason"`
			}
			_ = json.Unmarshal(ev.Data, &d)
			if !emitted {
				mapped, mapErr := agentMessageEvent(msgID, accumulated.String())
				if mapErr != nil {
					return mapErr
				}
				if err := onEvent(mapped); err != nil {
					return err
				}
			}
			logTurn("backend", "deepseek", "session", req.SessionID,
				"stream", true,
				"duration_ms", time.Since(start).Milliseconds(),
				"chars", accumulated.Len())
			if d.Reason.Kind == "error" {
				return fmt.Errorf("deepseek turn ended with error")
			}
			return nil
		}
	}
}

// dshMuxFrame is one /api/events.mux message (envelope shape per the
// Task 1 spike report — adjust if the wire carries binary framing).
type dshMuxFrame struct {
	SessionID string        `json:"sessionId"`
	Event     dshSessionEvent `json:"event"`
}

// readFrame reads one mux frame, honoring ctx cancellation.
func readFrame(
	ctx context.Context,
	conn *websocket.Conn,
) (dshMuxFrame, error) {
	type result struct {
		frame dshMuxFrame
		err   error
	}
	ch := make(chan result, 1)
	go func() {
		var f dshMuxFrame
		err := conn.ReadJSON(&f)
		ch <- result{f, err}
	}()
	select {
	case <-ctx.Done():
		_ = conn.Close()
		return dshMuxFrame{}, ctx.Err()
	case r := <-ch:
		return r.frame, r.err
	}
}
```

删除 `deepseek_client.go` 中未使用的 `net/url` import（若未用到）。

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/harness/ -run TestDeepSeekClient -v -count=1`
Expected: PASS（含 Task 8 的三个测试）。

- [ ] **Step 5: 全量回归**

Run: `go test ./...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/harness/deepseek_client.go internal/harness/deepseek_client_test.go
git commit -m "feat(harness): stream DeepSeek turn events over websocket mux feed"
```

---

### Task 10: deepseek Kind 接线（registry + main + env）

**Files:**
- Modify: `internal/harness/registry.go`（`ClientFor` 的 KindDeepSeek 分支已在 Task 2 就位——本任务确认）
- Modify: `cmd/oma-server/main.go`（`RegistryConfig` 加 DeepSeek 字段）
- Modify: `.env.example`
- Test: `internal/harness/registry_test.go` 追加 deepseek 分发测试

**Interfaces:**
- Consumes: Task 4 的 `deepseekCfg` 装配、Task 8/9 的 `DeepSeekClient`
- Produces: 完整的 `deepseek` Kind 分发链路。此后 API 创建 `{"_oma":{"harness":"deepseek"}}` 的 agent 即可跑 turn（对 mock 网关）

- [ ] **Step 1: 写失败测试**

在 `registry_test.go` 追加：

```go
func TestClientFor_DeepSeekKind(t *testing.T) {
	ds := &stubClient{name: "deepseek"}
	r := NewRegistry(RegistryConfig{Default: &stubClient{}, DeepSeek: ds})
	got, err := r.ClientFor(store.AgentConfig{Harness: "deepseek"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != ds {
		t.Fatalf("expected deepseek client")
	}
}

func TestClientFor_DeepSeekKind_NotConfigured_ReturnsStub(t *testing.T) {
	r := NewRegistry(RegistryConfig{Default: &stubClient{}})
	got, err := r.ClientFor(store.AgentConfig{Harness: "deepseek"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.(ManagedClient); !ok {
		t.Fatalf("expected ManagedClient stub, got %T", got)
	}
}
```

- [ ] **Step 2: 运行确认现状**

Run: `go test ./internal/harness/ -run TestClientFor_DeepSeekKind -v`
Expected: PASS（Task 2 已实现分发）——若通过则本步是接线确认，继续 Step 3。

- [ ] **Step 3: main.go 接线**

`cmd/oma-server/main.go` 的 `harnessRegistry` 字面量（Task 4 版本）追加字段：

```go
		DeepSeek: harnessGatewayClient(deepseekCfg.Disabled, deepseekCfg.GatewayURL,
			func() harness.Client {
				return &harness.DeepSeekClient{
					GatewayURL: deepseekCfg.GatewayURL,
					Token:      deepseekCfg.Token,
				}
			}),
```

- [ ] **Step 4: .env.example 追加配置段**

在 `OMA_HERMES_ENABLED` 段（.env.example:151）之后插入：

```bash
# --- Gateway harness: DeepSeek (dsh web) ---
# DeepSeek official agent harness running as a shared web gateway
# (`dsh web`, default port 3080). Agents with `_oma.harness: "deepseek"`
# are routed to it. No DB migration needed — flat kind since 2026-08.
OMA_DEEPSEEK_GATEWAY_URL=
# dsh web ships without auth; token reserved for future upstream support.
OMA_DEEPSEEK_TOKEN=
# Master switch for the DeepSeek harness. Set to 0/false/no/off to
# disable (registry falls back to the stub client) and grey out the
# DeepSeek option in the Harness dropdown. Defaults to enabled when unset.
OMA_DEEPSEEK_ENABLED=0
```

- [ ] **Step 5: 全量回归**

Run: `go build ./... && go test ./...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add cmd/oma-server/main.go internal/harness/registry_test.go .env.example
git commit -m "feat(harness): wire deepseek kind into registry, main and env"
```

---

### Task 11: docker 部署 — Dockerfile.dsh + compose 服务

**Files:**
- Create: `deploy/Dockerfile.dsh`
- Modify: `deploy/docker-compose.yml`
- Create: `start-deepseek.sh`（本地开发用，仿 `start-harness.sh`）

**Interfaces:**
- Consumes: Task 1 spike 报告的监听地址配置项（Step 2 使用）
- Produces: compose 内 `dsh` 服务，`oma-platform` 经 `http://dsh:3080` 访问；宿主不暴露 dsh 端口

- [ ] **Step 1: 编写 Dockerfile.dsh**

新建 `deploy/Dockerfile.dsh`（dsh 源码在平台仓库外——用 build arg 指向本地 checkout 路径，或改为 COPY；按部署环境选择，下面用本地路径挂载式构建）：

```dockerfile
# DeepSeek harness (dsh) web gateway image.
# Version pin: the dsh checkout is developer preview — pin the exact
# commit in DSH_SOURCE to keep the wire format stable (spec §2.3).
FROM node:22-bookworm

ARG NPM_REGISTRY=https://registry.npmmirror.com

RUN npm config set registry ${NPM_REGISTRY} && \
    npm install -g pnpm@11

WORKDIR /app

# Copy the pinned dsh source (build context = deepseek-harness-master).
COPY . /app/

RUN pnpm install --frozen-lockfile && pnpm run build

ENV DSH_HOME=/data/dsh \
    DSH_SESSION_ROOT=/data/dsh-sessions

EXPOSE 3080

# Host/port binding per the spike report (Task 1, Step 2). If dsh has
# no bind flag, wrap with socat:
#   CMD ["sh", "-c", "socat TCP-LISTEN:3080,fork,reuseaddr TCP:127.0.0.1:3080 & pnpm dsh web"]
CMD ["pnpm", "dsh", "web"]
```

- [ ] **Step 2: compose 服务**

在 `deploy/docker-compose.yml` 的 `oma-harness-lb` 服务之后追加：

```yaml
  # DeepSeek harness (dsh) web gateway. Internal-only: no host port
  # mapping — dsh web has no auth, so it must stay unreachable from
  # outside the compose network (spec risk #4).
  dsh:
    build:
      context: ${DSH_SOURCE:-../../deepseek-harness-master}
      dockerfile: Dockerfile.dsh
    environment:
      DEEPSEEK_API_KEY: ${DEEPSEEK_API_KEY}
      DEEPSEEK_BASE_URL: ${DEEPSEEK_BASE_URL:-}
    volumes:
      - dsh-data:/data
    healthcheck:
      test: ["CMD", "node", "-e", "fetch('http://127.0.0.1:3080/').then((r)=>process.exit(r.status<500?0:1)).catch(()=>process.exit(1))"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 30s
    restart: unless-stopped
```

`volumes:` 顶层段（docker-compose.yml:168-169）追加 `dsh-data:`。

`oma-platform` 服务的 `environment:` 段追加（docker-compose.yml:65-86）：

```yaml
      # DeepSeek harness gateway (dsh web) — in-network URL.
      OMA_DEEPSEEK_GATEWAY_URL: http://dsh:3080
```

`oma-platform` 的 `depends_on:` 追加：

```yaml
      dsh:
        condition: service_started
```

注：用 `service_started` 而非 `service_healthy`，避免 dsh 构建/启动慢时阻塞整个栈；网关不可用时 registry 回退 stub，不影响其他 harness。

- [ ] **Step 3: 本地开发脚本 start-deepseek.sh**

新建 `start-deepseek.sh`（仓库根，仿 `start-harness.sh` 的风格）：

```bash
#!/usr/bin/env bash
# Start the DeepSeek harness (dsh) web gateway for local development.
# Requires: Node ^22.19 || >=24, pnpm 11, DEEPSEEK_API_KEY in .env.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DSH_DIR="${DSH_DIR:-${ROOT_DIR}/../deepseek-harness-master}"
DSH_PORT="${DSH_PORT:-3080}"

# Load .env (DEEPSEEK_API_KEY etc.)
if [[ -f "${ROOT_DIR}/.env" ]]; then
  set -a
  # shellcheck disable=SC1091
  source "${ROOT_DIR}/.env"
  set +a
fi

if [[ -z "${DEEPSEEK_API_KEY:-}" ]]; then
  echo "[start-deepseek] DEEPSEEK_API_KEY not set — dsh cannot call the model." >&2
  exit 1
fi

cd "${DSH_DIR}"
if [[ ! -d node_modules ]]; then
  pnpm install
fi
if [[ ! -d packages/core/session/dist ]]; then
  pnpm run build
fi

echo "[start-deepseek] dsh web on 127.0.0.1:${DSH_PORT}"
echo "[start-deepseek] point OMA_DEEPSEEK_GATEWAY_URL=http://127.0.0.1:${DSH_PORT}"
exec pnpm dsh web
```

- [ ] **Step 4: 验证构建（不启动全栈）**

Run: `docker compose -f deploy/docker-compose.yml build dsh`
Expected: 构建成功。若 dsh 默认绑定 127.0.0.1 导致容器内 oma-platform 连不上，按 Task 1 Step 2 的结论调整 CMD（bind flag 或 socat）。

- [ ] **Step 5: Commit**

```bash
git add deploy/Dockerfile.dsh deploy/docker-compose.yml start-deepseek.sh
git commit -m "chore(deploy): add dsh web gateway image and compose service"
```

---

### Task 12: E2E 脚本 — smoke-deepseek-managed-e2e.sh

**Files:**
- Create: `scripts/e2e/smoke-deepseek-managed-e2e.sh`

**Interfaces:**
- Consumes: Task 10 的完整分发链路 + Task 11 的 dsh 部署（或本地 start-deepseek.sh）
- Produces: 真实链路回归脚本。结构仿 `scripts/e2e/smoke-hermes-managed-e2e.sh`（已读全文），无 `OMA_DEEPSEEK_GATEWAY_URL` 时自动跳过

- [ ] **Step 1: 编写脚本**

新建 `scripts/e2e/smoke-deepseek-managed-e2e.sh`：

```bash
#!/usr/bin/env bash
# End-to-end test: DeepSeek agent via the full stack.
#
# Verifies that an agent with _oma.harness="deepseek":
#   1. Accepts a user message through the API
#   2. Drives the dsh web gateway via DeepSeekClient
#   3. Emits the oma session event vocabulary (agent.message at minimum;
#      tool_use/tool_result when the model calls tools)
#   4. Feeds /v1/cost_report when usage is reported
#
# Expects a running oma-server at ${PLATFORM_URL:-http://127.0.0.1:8787}
# with OMA_DEEPSEEK_GATEWAY_URL configured and a dsh web instance up.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
SMOKE_UTILS="${ROOT_DIR}/scripts/e2e/smoke_utils.py"

PLATFORM_URL="${PLATFORM_URL:-http://127.0.0.1:8787}"
OMA_API_KEY="${OMA_API_KEY:-dev-key}"
DEEPSEEK_GATEWAY="${DEEPSEEK_GATEWAY:-http://127.0.0.1:3080}"
AGENT_NAME="${AGENT_NAME:-deepseek-e2e-$(date +%s)}"
SESSION_TIMEOUT_SEC="${SESSION_TIMEOUT_SEC:-180}"

if [[ "${DEEPSEEK_E2E_SKIP:-0}" == "1" ]]; then
  echo "[deepseek-e2e] DEEPSEEK_E2E_SKIP=1 — skipping"
  exit 0
fi

log() { echo "[deepseek-e2e] $*"; }
fail() { echo "[deepseek-e2e] FAIL: $*" >&2; exit 1; }

# dsh web must be reachable, otherwise skip (no API key in CI etc.).
if ! curl -sf -o /dev/null --max-time 5 "${DEEPSEEK_GATEWAY}/"; then
  log "dsh gateway not reachable at ${DEEPSEEK_GATEWAY} — SKIP"
  exit 0
fi
curl -sf "${PLATFORM_URL}/health" >/dev/null ||
  fail "oma-server not reachable at ${PLATFORM_URL}"
log "platform healthy, dsh gateway reachable"

H_COMMON=(
  -H "x-api-key: ${OMA_API_KEY}"
  -H "x-user-id: deepseek-e2e"
  -H "x-tenant-id: default"
  -H "Content-Type: application/json"
)

cleanup() {
  set +e
  if [[ -n "${AGENT_ID:-}" ]]; then
    curl -sf -X POST "${H_COMMON[@]}" \
      "${PLATFORM_URL}/v1/agents/${AGENT_ID}/archive" >/dev/null || true
  fi
}
trap cleanup EXIT

json_field() {
  python3 "${SMOKE_UTILS}" json-field "$1"
}

# ── 1. Create a deepseek agent (flat kind) ─────────────────────────────

create_agent() {
  log "create agent (_oma.harness=deepseek) name=${AGENT_NAME}"
  local resp
  resp=$(curl -s -o /tmp/deepseek-agent-body -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/agents" \
    -d @- <<JSON
{
  "name": "${AGENT_NAME}",
  "model": {"id": "deepseek-v4-flash", "speed": "standard"},
  "description": "E2E DeepSeek agent via dsh web gateway",
  "system": "You are a helpful assistant.",
  "_oma": {"harness": "deepseek"}
}
JSON
  )
  if [[ "${resp}" != "201" ]]; then
    fail "create agent status=${resp}: $(cat /tmp/deepseek-agent-body)"
  fi
  AGENT_ID=$(python3 "${SMOKE_UTILS}" json-field "id" </tmp/deepseek-agent-body)
  log "agent created — id=${AGENT_ID}"
}

# ── 2. Create a session ────────────────────────────────────────────────

create_session() {
  local envs env_id
  envs=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/environments?limit=5")
  env_id=$(echo "${envs}" | python3 -c "import sys,json; d=json.load(sys.stdin)['data']; print(d[0]['id'] if d else '')")
  [[ -n "${env_id}" ]] || fail "no environments available"

  local resp
  resp=$(curl -s -o /tmp/deepseek-session-body -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions" \
    -d "{\"agent\":\"${AGENT_ID}\",\"environment_id\":\"${env_id}\"}")
  if [[ "${resp}" != "201" ]]; then
    fail "create session status=${resp}: $(cat /tmp/deepseek-session-body)"
  fi
  SESSION_ID=$(python3 "${SMOKE_UTILS}" json-field "id" </tmp/deepseek-session-body)
  log "session created — id=${SESSION_ID}"
}

# ── 3. Send a message and wait for completion ──────────────────────────

send_message() {
  local msg="$1"
  local resp
  resp=$(curl -s -o /tmp/deepseek-post-events -w "%{http_code}" \
    -X POST "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events" \
    -d "{\"events\":[{\"type\":\"user.message\",\"content\":[{\"type\":\"text\",\"text\":\"${msg}\"}]}]}")
  if [[ "${resp}" != "202" && "${resp}" != "200" && "${resp}" != "201" ]]; then
    fail "post events status=${resp}: $(cat /tmp/deepseek-post-events)"
  fi
  log "message accepted"
}

wait_for_agent_reply() {
  local deadline=$((SECONDS + SESSION_TIMEOUT_SEC))
  local events terminal_seen last_type=""
  while [[ ${SECONDS} -lt ${deadline} ]]; do
    events=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc")
    terminal_seen=$(echo "${events}" | python3 -c "
import sys, json
raw = json.load(sys.stdin)['data']
last_user_idx = -1
for i, e in enumerate(raw):
    if e.get('type') == 'user.message':
        last_user_idx = i
tail = raw[last_user_idx + 1:] if last_user_idx >= 0 else raw
types = [e['type'] for e in tail]
if 'session.status_idle' in types:
    print('idle')
elif 'span.model_request_end' in types:
    print('span')
else:
    print(types[-1] if types else 'empty')
")
    if [[ "${terminal_seen}" == "idle" || "${terminal_seen}" == "span" ]]; then
      log "turn completed after $((SECONDS))s (signal=${terminal_seen})"
      return 0
    fi
    last_type="${terminal_seen}"
    sleep 2
  done
  fail "timed out waiting for turn completion (last type: ${last_type:-?})"
}

# ── 4. Verify an agent.message arrived ────────────────────────────────

verify_message() {
  local events has_msg
  events=$(curl -sf "${H_COMMON[@]}" "${PLATFORM_URL}/v1/sessions/${SESSION_ID}/events?order=asc")
  echo "${events}" > /tmp/deepseek-events.json
  has_msg=$(python3 -c "
import json
data = json.load(open('/tmp/deepseek-events.json'))['data']
inner = [e.get('data', e) if 'data' in e else e for e in data]
print('yes' if any(e.get('type') == 'agent.message' for e in inner) else 'no')
")
  [[ "${has_msg}" == "yes" ]] || fail "no agent.message event in stream"
  log "agent.message present"
}

main() {
  create_agent
  create_session
  send_message "Reply with exactly: hello-from-deepseek-e2e"
  wait_for_agent_reply
  verify_message
  log "DONE"
}

main "$@"
```

- [ ] **Step 2: 本地运行验证**

前置：`./start-deepseek.sh` 起 dsh，后端配 `OMA_DEEPSEEK_GATEWAY_URL=http://127.0.0.1:3080` 与 `OMA_DEEPSEEK_ENABLED=1` 并启动。

Run: `bash scripts/e2e/smoke-deepseek-managed-e2e.sh`
Expected: `[deepseek-e2e] DONE`。无 dsh 时验证跳过路径：`DEEPSEEK_GATEWAY=http://127.0.0.1:1 bash scripts/e2e/smoke-deepseek-managed-e2e.sh` → `SKIP`。

- [ ] **Step 3: Commit**

```bash
git add scripts/e2e/smoke-deepseek-managed-e2e.sh
git commit -m "test(e2e): add deepseek managed e2e smoke script"
```

---

### Task 13: 前端最终验收 + 文档收尾

**Files:**
- Modify: `docs/plans/pluggable-harness-zh.md`（实施状态更新）
- Verify: console 全流程

**Interfaces:**
- Consumes: 全部前序任务
- Produces: spec §8 验收标准的逐项核对结果

- [ ] **Step 1: 前端测试与类型检查**

Run（console 目录）: `npm run typecheck && npm test`
Expected: PASS。

- [ ] **Step 2: 手工验收 spec §8 六条标准**

起全栈（compose 或本地脚本），逐项核对：

1. 存量 managed+hermes/openclaw agent 的 turn 分发正常（对真实网关或 stub）
2. console 创建 agent：Cloud / Hermes / OpenClaw / DeepSeek / Bring-your-own-daemon 选项齐全；保存 DeepSeek agent 后 `_oma.harness="deepseek"` 无 runtime_binding
3. DeepSeek agent 会话流式渲染（WS 路径）；若 spike 降级为非流式，确认非流式结果正常
4. `OMA_DEEPSEEK_ENABLED=0` 重启后端 → DeepSeek 选项置灰；hermes/openclaw/piPy 行为不变
5. `docker compose up` 后 `docker compose ps` 显示 dsh healthy；`docker compose exec oma-platform wget -qO- http://dsh:3080/ | head -c 200` 连通
6. `go test ./...` 与 `npm test` 全绿

- [ ] **Step 3: 更新实施状态文档**

`docs/plans/pluggable-harness-zh.md` 实施状态段（第 8 行起）追加一行：

```markdown
- 2026-08-20：harness 分发扁平化完成（hermes/openclaw/deepseek 一级 Kind，
  legacy managed 数据分发期归一化）；DeepSeek harness（dsh web 网关）接入完成。
  详见 docs/superpowers/specs/2026-08-20-deepseek-harness-integration-design.md。
```

- [ ] **Step 4: Commit**

```bash
git add docs/plans/pluggable-harness-zh.md
git commit -m "docs: record harness flattening and deepseek integration status"
```

---

## Self-Review 结果（计划作者已执行）

1. **Spec 覆盖**：Phase 0 → Task 1；Phase 0.5 步骤 A/B/C/D/E/F/G → Task 2/5/4/4/6/2-5/6；Phase 1 → Task 8/9/10；Phase 2 → Task 6 Step 4 + Task 13；Phase 3 → Task 11；Phase 4 → Task 12 + Task 9 Step 3（取消经 ctx → WS 断连，readFrame 已实现）；Phase 5 为 P2 不在本计划。✓
2. **占位符扫描**：Task 8 的 `session/wait`、Task 9 的 mux 帧形态为显式标注的 spike 依赖项（含替换指引），非遗漏。✓
3. **类型一致性**：`RegistryConfig{Hermes,OpenClaw,DeepSeek}`（Task 2/4/10）、`HarnessState{OpenClaw,Hermes,DeepSeek}` + `HarnessAvailability(oc,hc,ds)`（Task 4）、`DeepSeekConfig{GatewayURL,Token,Disabled}`（Task 4 Step 3 定义，Task 10 复用）、`openclawModel` → `OpenclawModel`（Task 3 定义，Task 4 Step 5 导出改名）、`validateHarnessBinding`（Task 5，调用点同步更新两处）、前端 `managedAgent` 联合类型四处扩宽一致（Task 6 Step 3/4）。✓
