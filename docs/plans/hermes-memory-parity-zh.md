# Plan: Hermes Memory 体系（piPy Extension 形态）+ OpenViking 集成

**Status:** APPROVED — 已实现并验证（单测全绿 + 真实 OpenViking E2E 13/13 通过；见文末进度）
**Date:** 2026-08-11
**Branch:** `memory`
**Scope:** 新目录 `piPy-hermes-memory/`（piPy extension 包）+ `oma-platform` 薄接线（Go internal API + harness/oma_adapter）

> 本方案取代早期的 Go-centric 设计（internal/memoryprovider）。按最新决定：**memory 系统以 piPy extension 的形式集成**，全部记忆逻辑放在 `piPy-hermes-memory/`，布局对齐 `piPy-subagent/`、`piPy-teams/`。

## 一句话总结

以 piPy extension 包形态实现 Hermes Agent 的 memory 体系：内置记忆（MEMORY.md/USER.md）、MemoryProvider 抽象层、OpenViking 适配器全部落在 `piPy-hermes-memory/`；oma-platform 侧只做三件薄事——内置 store 自动创建 + 两个 internal 端点（持久化与审计）、oma_adapter 的 runtime 桥接与 extension 加载、harness 依赖声明。

## 关键技术依据（已验证）

- piPy extension 支持 `async register(api)`（`pi_coding_agent/resources/extensions.py` 的 `load_extensions` 会 await 协程返回值）。
- `api.on(event, handler)` 支持 Hermes 式生命周期事件：`turn_start` / `turn_end`（payload 含 assistant message + toolResults）/ `agent_end`（payload 含全部 messages）/ `session_before_compact` / `session_shutdown` 等（`SUPPORTED_EXTENSION_EVENTS`）。
- `{workdir}/.pi/APPEND_SYSTEM.md` 在显式 `custom_prompt` 下仍会被 `build_system_prompt` 追加进 system prompt；且 extension 的 `register()` 先于 prompt 解析执行——**快照注入可在 extension 内闭环**。
- OMA 每回合新建 piPy session（`in_memory=True`），回合结束即弃；oma_adapter 未使用过 APPEND_SYSTEM.md（无冲突）。
- extension 加载与 runtime 桥接沿用 subagent/teams 既有模式：`resolve_*_extension_path()`（env override → bundled → 兄弟仓库）+ ContextVar runtime（`subagent_bridge.py` / `team_bridge.py` 为模板）。

---

## 新包结构：`piPy-hermes-memory/`

```
piPy-hermes-memory/
├── pyproject.toml              # uv workspace root，镜像 piPy-subagent（tuna index、pi-agent/pi-ai gitee v0.3.0 sources、pytest 配置）
├── readme.md
├── extensions/memory_extension.py   # piPy extension 入口：async register(api)
└── packages/pi_memory/
    ├── pyproject.toml          # name=pi-memory；deps: pi-agent, pi-ai, httpx
    ├── src/pi_memory/
    │   ├── runtime.py          # MemoryRuntime dataclass + ContextVar + configure/get/clear
    │   ├── builtin.py          # BuiltinMemory：Hermes 语义
    │   ├── platform_client.py  # oma-server internal API httpx 客户端
    │   ├── inject.py           # APPEND_SYSTEM.md 幂等写入（marker 块）
    │   ├── tools.py            # MemoryTool + ProviderToolAdapter（AgentTool 协议）
    │   └── providers/          # base.py(ABC) / builtin.py(no-op) / openviking.py / __init__.py(get_provider 按 env 单选)
    └── tests/                  # pytest（httpx MockTransport）
```

`MemoryRuntime` 字段：`session_id`、`tenant_id`、`agent_id`、`workdir`、`platform_base`、`internal_secret`、`last_user_text`、`turn_uuid`。

---

## 内置记忆（Hermes parity，逻辑全部在 pi_memory）

- `BuiltinMemory`：两个 target `memory`/`user`，内容 = `§\n` 分隔条目；上限默认 2200/1375 字符（env `OMA_MEMORY_LIMIT_MEMORY` / `OMA_MEMORY_LIMIT_USER` 可覆盖）。
  - `add`：追加条目；超限返回错误（不静默丢弃，agent 自行腾空间重试）。
  - `replace` / `remove`：`old_text` 唯一子串匹配；未命中或命中多条均报错。
  - `render_snapshot()`：Hermes 格式冻结块，含用量头，如 `MEMORY (your personal notes) [67% — 1474/2200 chars]`。
- **持久化走平台 memory store**（复用版本审计 / retention / console 可见性）。Go 侧：
  - migration `024_agent_memory.sql`：`ALTER TABLE memory_stores ADD COLUMN kind TEXT NOT NULL DEFAULT 'standard'`。
  - `MemoryStoreRepo` 增加幂等 `EnsureStoreWithID`（确定性 ID `agentmem-{agent_id}`，kind=`agent_builtin`，name 如 `Agent Memory`）；`ListStores` 默认过滤 `agent_builtin`（`include_builtin=true` 例外）。
- 两个新 internal 端点（挂 `/v1/internal`，x-internal-secret 鉴权，tenant 走现有惯例）：
  - `GET /v1/internal/agent_memory?tenant_id=&agent_id=` → 懒创建内置 store，返回 `{store_id, contents: {"/MEMORY.md": "...", "/USER.md": "..."}}`（缺失为空串，blob hydrate）。
  - `POST /v1/internal/agent_memory/write` `{tenant_id, agent_id, path(仅限 /MEMORY.md|/USER.md), content, session_id}` → `WriteMemory`，actor `agent_session/{session_id}`。
- `MemoryTool`（工具名 `memory`，参数 `action: add|replace|remove`、`target: memory|user`、`content`、`old_text`）：
  - execute 时对内存中的 BuiltinMemory 应用语义，成功后把**整个文件内容**经 write 端点落库；
  - 返回 Hermes 风格结果文本（操作后的实时状态 / 错误原因）；
  - 写入成功触发 provider `on_memory_write`（fire-and-forget）。

---

## Extension 行为（`extensions/memory_extension.py`）

`async register(api)`，`get_memory_runtime()` 为 None 时直接返回：

1. 经 `platform_client` 拉取内置记忆 → 构建 BuiltinMemory；按 env `OMA_MEMORY_PROVIDER`（`builtin` 默认 | `openviking`，非法值降级 builtin 并告警）初始化 provider。
2. **注入**：渲染 MEMORY/USER 冻结块 + `provider.system_prompt_block()` + `provider.prefetch(runtime.last_user_text)` 召回块 → `inject.py` 以 marker 块幂等写入 `{workdir}/.pi/APPEND_SYSTEM.md`（重复加载替换不叠加）。
3. `api.register_tool(MemoryTool)` + provider 工具。
4. **生命周期钩子**：
   - `api.on("turn_end")` → payload 的 assistant message + runtime 的 user 文本 → `provider.sync_turn`（asyncio task，非阻塞）。
   - `api.on("session_before_compact")` → `provider.on_pre_compress`。
   - `api.on("agent_end")` → `provider.on_session_end`（见 OpenViking 小节的 v1 语义）。

---

## Provider 抽象层（`pi_memory/providers/`）

- `MemoryProvider` ABC（对齐 Hermes `MemoryProvider`，async 化）：`name`、`is_available()`、`initialize(runtime)`、`system_prompt_block()`、`prefetch(query)`、`sync_turn(user, assistant)`、`on_session_end(messages)`、`on_pre_compress(messages)`、`on_memory_write(action, target, content)`、`get_tool_schemas()`、`handle_tool_call(name, args)`。单选规则同 Hermes。
- `BuiltinProvider`：全 no-op、无工具。

### OpenVikingProvider

**配置（harness 进程 env）**：`OPENVIKING_ENDPOINT`（默认 `http://127.0.0.1:1933`）、`OPENVIKING_API_KEY`（可选，Bearer 头）、`OPENVIKING_ACCOUNT`（默认 `default`）、`OPENVIKING_USER`（默认 `default`）、`OPENVIKING_AGENT`（默认 `oma`）、`OPENVIKING_TIMEOUT_MS`（默认 5000）、`OPENVIKING_RECALL_MAX`（默认 5）。

**映射与钩子**：
- 身份映射：OV account = `OPENVIKING_ACCOUNT`；OV user = oma `tenant_id`（租户级命名空间）；peer/agent = `OPENVIKING_AGENT`；OV session = `{oma_session_id}-t{turn_uuid}`（每回合一个，规避 commit 后会话固化问题）。
- `prefetch`：`POST /api/v1/search/find`（query + top_k=RECALL_MAX）→ 召回块（uri + 摘要截断）。
- `sync_turn`：`POST /api/v1/sessions/{sid}/messages/batch`（user/assistant parts，assistant 带 `peer_id=OPENVIKING_AGENT`），随后立即 `POST /api/v1/sessions/{sid}/commit`（v1 回合级提取；文档记录与 Hermes 会话级 commit 的差异）。
- `on_memory_write`：`POST /api/v1/content/write` 镜像条目到 `viking://user/{tenant_id}/memories/{target}/`。
- 工具 4 个（`ProviderToolAdapter` 统一路由）：`memory_search`（find / search 深度模式）、`memory_read`（content/read|abstract|overview）、`memory_browse`（fs/ls|tree|stat）、`memory_write`（content/write）。
- 错误处理：非 2xx / HTML 响应脱敏为短错误文本（参考 Hermes `_sanitize_openviking_error_message`），不泄露堆栈；所有网络失败仅告警、不阻断回合。

---

## Host 接线（oma-platform，最小改动）

- `harness/oma_adapter/memory_bridge.py`：`build_memory_runtime(turn_request)` —— 从 TurnRequest 取 session_id / tenant_id / agent.id / workdir / platform_base / internal_secret，从 events 提取最后一条 user 文本，生成 turn_uuid；`turn.py` 在配置 subagent/team runtime 同处 configure，finally 里 clear。
- `harness/oma_adapter/tools.py`：`resolve_memory_extension_path()`（env `PIPY_MEMORY_EXTENSION` → 同目录 bundled → `parents[3]/"piPy-hermes-memory"/extensions/memory_extension.py`，与 subagent/teams 完全同构）；`_extension_paths_for_agent` 在 `OMA_MEMORY_ENABLED=1` 且 runtime 要素齐全时追加该路径。
- `harness/pyproject.toml`：新增 `pi-memory` 依赖，uv source 用本地 path `../../piPy-hermes-memory/packages/pi_memory`（editable）；发布后切 gitee pin，与其他 pi-* 一致。
- **不改** TurnRequest 结构、**不改** `compose_system_prompt`（注入走 APPEND_SYSTEM.md）。

---

## Test Plan

- **pi_memory 单元（pytest + httpx MockTransport）**：
  - builtin 语义：add 超限报错、replace/remove 唯一子串 / 歧义 / 未命中、快照渲染与用量百分比、§ 多行条目；
  - inject 幂等：两次 register 不叠加、marker 块替换；
  - platform_client：GET/write 路径、头、错误；
  - provider registry：默认 builtin、非法值降级；
  - openviking 客户端：batch/commit/find/content-write 请求形状、auth 头、非 2xx 脱敏；
  - MemoryTool execute 全链路（mock platform HTTP）。
- **extension 集成**：模拟 ExtensionAPI（记录 register_tool/on）验证 async register 全流程（runtime 缺失时不注册、快照文件写入、钩子绑定）。
- **oma 侧**：Go 单测（024 migration、EnsureStoreWithID 幂等、ListStores kind 过滤、两个 internal 端点鉴权 / path 白名单 / actor 记录）；`harness/tests` 对 memory_bridge 与 tools.py 路径解析。
- **E2E（真实 OpenViking）**：`pip install openviking` → `openviking-server init`（需模型 provider，可用 OpenAI 兼容 key 或本地 Ollama）→ 启动于 1933；harness 配 `OMA_MEMORY_ENABLED=1 OMA_MEMORY_PROVIDER=openviking OPENVIKING_*`；跑真实 session 验证：
  (a) system prompt 含冻结块与召回块；
  (b) `memory` 工具写入后下个回合可见；
  (c) OV 侧出现 batched/committed 会话与 `viking://user/{tenant}/memories/` 提取结果。
  无模型 key 时以 MockTransport 集成测试替代，并把真实实例步骤写入文档。

---

## Assumptions

- 内置记忆按 **(tenant, agent)** 维度；`USER.md` 指与该 agent 对话的用户画像，同 agent 跨 session 共享（对应 Hermes per-profile）。
- **默认关闭、env 开启**（`OMA_MEMORY_ENABLED=1`）：避免改变现有 e2e 的 system prompt；开启后所有 piPy session 获得 memory 工具与注入。
- OpenViking 采用**回合级** session + commit（v1）；「OMA session archive 时整段 commit」需要平台→harness 新 RPC，列为后续。
- Hermes 的 write_approval 审批门、后台自改进 review、skill 捕获不在本期。
- `register(api)` 先于 system prompt 解析执行、APPEND_SYSTEM.md 追加行为依赖当前 piPy v0.3.0；升级 piPy 时需回归这两点。
- 其他 harness（ACP/managed）不获得 memory 能力（v1 仅 piPy）。
- `§` 分隔符、2200/1375 字符上限、快照头格式按 Hermes 当前文档取值；OpenViking API 以 Hermes openviking 插件实际调用的 `/api/v1/*` 端点为准。

---

## 参考

- Hermes 持久记忆：`website/docs/user-guide/features/memory.md`
- Hermes Memory Providers：`website/docs/user-guide/features/memory-providers.md`
- Hermes Provider 插件开发指南：`website/docs/developer-guide/memory-provider-plugin.md`
- Hermes OpenViking 插件实现：`plugins/memory/openviking/__init__.py`
- OpenViking 仓库：`volcengine/OpenViking`（README / docs.openviking.ai）
- piPy extension 机制：`pi_coding_agent/resources/extensions.py`、`pi_coding_agent/resources/extension_bindings.py`、`pi_coding_agent/context/loader.py`

---

## 实现进度（2026-08-11 更新）

### 已完成

- **Go 侧（oma-platform）**
  - [x] `internal/store/migrations/024_agent_memory.sql`：`memory_stores` 增加 `kind`（开发库已同步 ALTER）
  - [x] `scripts/sql/platform_mysql.sql` 同步 `kind` 字段
  - [x] `MemoryStoreRepo`：`Kind` 字段、`IncludeBuiltin` 选项、幂等 `EnsureStoreWithID`（MySQL `INSERT IGNORE`，确定性 ID `agentmem-{agent_id}`）、`GetMemoryByPath`（blob hydrate）、列表默认过滤 `kind='standard'`
  - [x] internal 端点：`GET /v1/internal/agent_memory`（懒创建内置 store，返回 `/MEMORY.md` + `/USER.md`）、`POST /v1/internal/agent_memory/write`（path 白名单，actor `agent_session/{session_id}`）
  - [x] store 管理 API：`include_builtin` 查询参数、序列化器 `kind` 字段
  - [x] Go 测试：`internal/store/memory_agent_store_test.go`、`internal/api/agent_memory_test.go`（远程 MySQL 全绿）
- **piPy-hermes-memory（新兄弟仓库）**
  - [x] `packages/pi_memory`：`runtime`（ContextVar）/ `builtin`（§ 条目、2200/1375 上限、add/replace/remove）/ `platform_client` / `inject`（APPEND_SYSTEM.md marker 幂等写入）/ `tools`（MemoryTool 写后全文镜像落库）/ `providers`（base/builtin/openviking，env 单选）
  - [x] `extensions/memory_extension.py`：async register，快照+召回注入，turn_end/session_before_compact/agent_end 钩子
  - [x] 36 个单元测试全绿（httpx MockTransport）
- **harness（oma-platform/harness）**
  - [x] `oma_adapter/memory_bridge.py`：`OMA_MEMORY_ENABLED=1` 开关、按回合构建/配置/重置 MemoryRuntime、最后一条 user 文本提取、后台任务 drain
  - [x] `oma_adapter/turn.py`：subagent runtime 之后构建并 configure，返回前 drain，finally 中 reset
  - [x] `oma_adapter/tools.py`：`resolve_memory_extension_path()`（env → bundled → 兄弟仓库），enabled 时追加 memory extension
  - [x] `pyproject.toml`：`pi-memory` 依赖（本地 editable path，发布后切 gitee pin）
  - [x] `tests/test_memory_bridge.py`：18 个测试全绿

### 验证结果

- `go build ./...`、`go vet ./internal/...` 通过；store/api memory 相关 Go 测试对远程 MySQL 全绿。
- pi_memory 36 单测全绿；harness 非 live 套件 154 passed（含新增 18 个 memory 测试）。
- 已知**既有**失败（与本方案无关，基线可复现）：
  - `test_outbound.py::test_wire_outbound_bash_proxy_sets_bash_operations`
  - `test_subagent_live_harness.py::test_subagent_real_harness_model_calls_call_agent`
  - `test_workflow_subagent_bridge.py::test_oma_run_sub_turn_emits_agent_message_on_sub_thread`
  - `test_web_search.py` 两个用例（`test_turn_stream.py` 导入 `oma_adapter.main` → `load_dotenv(.env)` 泄漏 `TAVILY_API_KEY` 的测试隔离问题）
  - `internal/api/memory_evals_test.go`（硬编码 `:memory:` SQLite DSN，与 MySQL-only `Open()` 不兼容）

### E2E 结果（2026-08-11，真实 OpenViking + 真实 oma-server）

- openviking 0.4.13 本地启动（1933）：vlm 走 OpenAI 兼容网关；embedding 用
  `provider: "local"` + bge-small-zh-v1.5 GGUF（macOS 13 Intel 需关闭 Metal
  源码编译 llama-cpp-python，详见 piPy-hermes-memory/readme.md）
- 平台侧用当前分支新构建的 oma-server 实例（internal 端点生效）
- 13/13 断言通过：extension 注册（memory + 4 个 provider 工具 + 3 个钩子）、
  APPEND_SYSTEM.md 注入、memory 工具写入落库（`agentmem-{agent_id}` 幂等 ID）、
  OV 镜像 `viking://user/{tenant}/memories/memory.md`、turn_end batch+commit、
  search/find 语义召回（score 0.65）、memory_browse/memory_search 工具实测

### 待办 / 后续

- [x] E2E：真实 OpenViking 实例（2026-08-11 通过，记录见上）
- [ ] `piPy-hermes-memory` 发布到 gitee 后，harness 依赖由本地 path 切换为 git pin
- [ ] （可选）`oma_adapter/extensions/memory_extension.py` bundled 副本，供 Docker/生产部署（与 subagent/teams 同构）
