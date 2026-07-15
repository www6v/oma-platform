# Memory Store 架构

本文说明 OMA（Open Managed Agents）系统中 **Memory Store** 的职责、数据模型、存储分层，以及与 Agent 运行时的集成方式。

## 一句话总结

**Memory Store 是租户级的持久化键值记忆库**：按 `path` 存文本内容，支持版本审计、乐观并发与内容脱敏；小对象内联在 SQLite，大对象 offload 到本地文件 blob。

## 使用场景

Memory 解决的是「**跨 Session、跨 Agent 仍要保留的结构化上下文**」，而不是单次对话里的 turn 历史。典型场景如下：

| 场景 | 说明 | 常见 path 示例 |
|------|------|----------------|
| **团队知识库** | 产品规范、Runbook、FAQ，多个 Agent / Session 共用 | `/docs/oncall.md` |
| **用户 / 租户偏好** | 语言、输出格式、禁用项 | `/prefs/style.md` |
| **项目长期上下文** | 研究结论、决策记录、迭代笔记 | `/project/decisions.md` |
| **Environment 预置资料** | 创建 Session 时通过 Environment 挂载 store，Agent 在沙箱里当文件读 | `/notes/*.md` |
| **合规与审计** | 谁在何时改了哪条 memory（`memory_versions`），必要时 redact | — |

不适合放进 Memory 的内容：单次 Session 内的工具输出、流式对话原文、大体积二进制（应用 Files API）、需要全文检索的索引型数据（当前无 search）。

### 与 Session 事件的区别

| 维度 | Session Events | Memory Store |
|------|----------------|--------------|
| 粒度 | 对话回合、工具调用 | 按 path 组织的文档/片段 |
| 生命周期 | 绑定 Session | 独立 store，可跨 Session 引用 |
| 主要写入方 | Harness 回合（事件流） | REST API（Console / 自动化） |
| Agent 回合内可见性 | 投影进 prompt / 事件历史 | 挂载到 workdir 文件树后由工具读取 |
| 审计 | `session_events` | `memory_versions` 追加日志 |

## 何时写入

持久化写入 **只发生在平台侧**（SQLite + 可选 blob），入口是 `/v1/memory_stores/.../memories` 相关 API。Agent 在沙箱里改挂载文件 **不会自动写回** store。

| 时机 | 触发方 | API / 代码路径 | 是否持久化到 store |
|------|--------|----------------|-------------------|
| **运维 / Console 手工维护** | 用户 | `POST .../memories`（按 path upsert） | ✅ |
| **外部系统集成** | 脚本、CI、Webhook 处理器 | 同上；可带 `precondition` 做 CAS | ✅ |
| **更新 / 删除单条** | API 客户端 | `PATCH .../memories/{id}`、`DELETE ...` | ✅ |
| **删除整个 store** | API 客户端 | `DELETE /v1/memory_stores/{id}`（级联 memories + blob） | ✅ |
| **Agent 回合内编辑挂载文件** | piPy 工具（写 workdir） | `resource_mounter` 仅 **回合开始** 从 store → workdir | ❌（仅 Session 沙箱内有效） |
| **Agent 专用 memory 工具** | — | 未实现 | ❌（规划中） |

每次成功 create / update / delete 都会在 `memory_versions` 追加一条审计记录（actor 为 `user/{id}` 或 `api_key/api`）。

写入 body 示例：

```json
POST /v1/memory_stores/{storeId}/memories
{
  "path": "/notes/topic.md",
  "content": "用户偏好：回复用中文",
  "precondition": { "if_absent": true }
}
```

## 何时读取

读取分 **管理面 API** 与 **Agent 执行面挂载** 两条路径。

| 时机 | 触发方 | 代码路径 | 读到什么 |
|------|--------|----------|----------|
| **Console / 集成查询** | API 客户端 | `GET .../memories`（列表，无 content） | metadata |
| **查看单条正文** | API 客户端 | `GET .../memories/{memoryId}` | 全文（blob 会 hydrate） |
| **审计 / 回溯** | 合规、排障 | `GET .../memory_versions` | 历史快照或 hash |
| **每个 Session 回合开始** | Session Machine | `ResourceResolver.ResolveForTurn` → `TurnRequest.resources` | store 内全部 memory 正文 |
| **Harness 回合内** | `turn.py` | `mount_resources(workdir, resources)` | 写入沙箱文件树 |
| **Agent 使用工具读文件** | piPy（bash / read 等） | 读 `{workdir}/mnt/memory/{store_name}/{path}` | 挂载副本，非直连 API |

回合内的时序：

```text
用户 message → Session Machine 排队 turn
    → ResolveForTurn：从 DB 拉 store + hydrate blob
    → POST harness /internal/turn（带 resources[]）
    → mount_resources：workdir/mnt/memory/{store_name}/...
    → Agent 工具读/写 workdir 内文件
    → turn 结束（workdir 变更不自动 sync 回 Memory Store）
```

`access: read_only` 时，mounter **不会覆盖** workdir 里已存在的同名文件（便于同一 Session 多回合在本地累积草稿）；非 read_only 时每次回合会用 store 内容覆盖挂载路径。

## Memory 是什么（数据模型概览）

## 核心概念

### Memory Store（容器）

一条 **Memory Store** 是租户下的命名空间，类似「文件夹根」：

```go
// internal/store/memory_stores.go
type MemoryStoreRow struct {
    ID          string
    TenantID    string
    Name        string
    Description sql.NullString
    CreatedAt   int64
    UpdatedAt   sql.NullInt64
    ArchivedAt  sql.NullInt64
}
```

- ID 前缀：`memstore-`
- 支持 **archive**（软归档，`archived_at` 非空）与 **delete**（级联删除 memories / versions，并清理 blob 文件）
- 列表过滤：`status=active|archived|any`，以及 `created_after` / `created_before`

### Memory（条目）

每条 **Memory** 在一个 store 内由 **`path` 唯一** 标识（类似文件路径，如 `/notes/topic.md`）：

```go
type MemoryRow struct {
    ID            string
    StoreID       string
    Path          string
    Content       string   // 小对象 inline；大对象为空，读时 hydrate
    BlobKey       string   // 大对象在文件系统的 key
    ContentSHA256 string   // 内容哈希，用于 CAS
    ETag          string   // MVP 与 content_sha256 相同
    SizeBytes     int64
    CreatedAt     int64
    UpdatedAt     int64
}
```

- ID 前缀：`mem-`
- `WriteMemory(path, content)`：**按 path upsert**——不存在则 create，存在则 update
- `UpdateMemory(id, ...)`：按 id 更新 path 和/或 content
- `DeleteMemory(id)`：删除前写入 `delete` 版本记录

### Memory Version（审计日志）

每次 create / update / delete 追加一条 **只增不改** 的版本行（除非 redact）：

```go
type MemoryVersionRow struct {
    ID            string
    MemoryID      string
    StoreID       string
    Operation     string   // create | update | delete
    Path          sql.NullString
    Content       sql.NullString   // 快照；超大内容省略
    ContentSHA256 sql.NullString
    SizeBytes     sql.NullInt64
    ActorType     string   // user | api_key
    ActorID       string
    CreatedAt     int64
    Redacted      bool
}
```

- ID 前缀：`memver-`
- **Redact**：`POST .../memory_versions/{id}/redact` 将 `content` 置 NULL、`redacted=1`（合规脱敏）
- 版本内容快照上限 **100 KiB**；超过则版本行只保留 hash / size，不存全文

## 数据模型（SQLite）

迁移文件：

- `internal/store/migrations/010_memory_evals.sql` — 建表
- `internal/store/migrations/013_memory_blobs.sql` — `memories.blob_key` + 版本时间索引

```text
memory_stores (1)
    │
    ├── memories (N)          UNIQUE(store_id, path)
    │       └── blob_key → 文件系统 (可选)
    │
    └── memory_versions (N)   按 memory_id / store_id 索引
```

表关系要点：

- `memories.store_id` → `memory_stores.id` **ON DELETE CASCADE**
- `memory_versions.store_id` → `memory_stores.id` **ON DELETE CASCADE**
- `(store_id, path)` 唯一约束保证同 path 只有一条当前 memory

## 存储分层：Inline vs Blob

实现位于 `internal/store/memory_blob_helpers.go` 与 `internal/memoryblob/store.go`。

### 大小阈值

| 常量 | 值 | 含义 |
|------|-----|------|
| `maxMemoryInlineBytes` | 4 KiB | 有 blob store 时，≤4 KiB 仍 inline |
| `maxMemoryInlineOnlyBytes` | 100 KiB | **无** blob store 时的硬上限 |
| `maxMemoryBlobBytes` | 10 MiB | 有 blob store 时的硬上限 |
| `maxMemoryVersionInlineBytes` | 100 KiB | 版本快照 inline 上限 |

### 决策逻辑

```text
content 写入
    │
    ├─ blobs == nil 且 size > 100 KiB → ErrMemoryContentTooLarge
    ├─ blobs == nil 或 size ≤ 4 KiB    → SQLite memories.content 存全文
    └─ blobs != nil 且 size > 4 KiB    → 文件 blob + content 字段留空
                                              blob_key = t/{tenant}/memory/{storeID}/{memoryID}
```

读取时 `hydrateMemory`：若 `blob_key` 非空，从 `MEMORY_DATA_DIR` 读文件并填充 `Content`。

Blob 存储复用 `internal/fileblob` 的 `WriteKey` / `ReadKey` / `DeleteKey`，与 Files 上传共用同一套本地文件抽象，路径前缀不同：

```go
// internal/memoryblob/store.go
func Key(tenantID, storeID, memoryID string) string {
    return filepath.Join("t", tenant, "memory", storeID, memoryID)
}
```

更新 content 时会删除旧 blob key（若 key 变化）；删除 store / memory 时同步清理 blob 文件。

### 磁盘布局

| 环境变量 | 默认 | 用途 |
|----------|------|------|
| `DATABASE_PATH` | `./data/oma.db` | SQLite（metadata + 小 inline content） |
| `MEMORY_DATA_DIR` | `./data/memory` | 大 memory 对象 blob 根目录 |

启动时 `cmd/oma-server/main.go` 创建目录并注入：

```go
memoryBlobs := memoryblob.NewStore(memoryDataDir)
memoryStores := store.NewMemoryStoreRepo(db, memoryBlobs)
```

## HTTP API

路由挂载：`internal/api/memory_stores.go` → `mountMemoryStoreRoutes`，前缀 `/v1/memory_stores`。

鉴权与其它 `/v1/*` 一致（API Key / Session Cookie / `AUTH_DISABLED` 开发模式）。

### Store 生命周期

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/memory_stores` | 创建 store |
| `GET` | `/v1/memory_stores` | 列表（status / 时间过滤） |
| `GET` | `/v1/memory_stores/{id}` | 详情 |
| `POST` / `PUT` | `/v1/memory_stores/{id}` | 更新 name / description |
| `POST` | `/v1/memory_stores/{id}/archive` | 归档 |
| `DELETE` | `/v1/memory_stores/{id}` | 删除 store 及全部 memories |

### Memory CRUD

| 方法 | 路径 | 说明 |
|------|------|------|
| `POST` | `/v1/memory_stores/{id}/memories` | 按 path 写入（upsert） |
| `GET` | `/v1/memory_stores/{id}/memories` | 列表；**不含** `content`（仅 metadata） |
| `GET` | `/v1/memory_stores/{id}/memories/{memoryId}` | 单条，含 `content`（blob 会 hydrate） |
| `PATCH` / `POST` | `.../memories/{memoryId}` | 更新 path / content |
| `DELETE` | `.../memories/{memoryId}` | 删除；可选 `?expected_content_sha256=` |

列表支持 `path_prefix` / `prefix` 与 `depth`（在 prefix 下按 path 段数过滤）。

### 版本与脱敏

| 方法 | 路径 | 说明 |
|------|------|------|
| `GET` | `/v1/memory_stores/{id}/memory_versions` | 列表；可选 `?memory_id=` |
| `GET` | `/v1/memory_stores/{id}/memory_versions/{versionId}` | 详情（含 content 快照） |
| `POST` | `.../memory_versions/{versionId}/redact` | 脱敏版本内容 |

### 乐观并发（Precondition）

写入 body 可带 `precondition`：

```json
{
  "path": "/notes/topic.md",
  "content": "updated",
  "precondition": {
    "if_absent": true,
    "content_sha256": "abc123..."
  }
}
```

| 字段 | 行为 |
|------|------|
| `if_absent: true` | 仅当 path 不存在时创建；已存在 → `409 precondition_failed` |
| `content_sha256` | 仅当当前内容与 hash 一致时更新；不一致 → `409` |

删除时可用 query `expected_content_sha256` 做 CAS。

Actor  attribution：有用户 Session 时为 `user/{user_id}`，否则 `api_key/api`。

## 版本保留（Retention Worker）

`internal/memory/retention.go` 中的 **RetentionWorker** 定期清理旧版本行：

- 默认保留 **30 天**（`RetentionDays`）
- 每天 **UTC 03:00** 触发（`SweepHourUTC` / `SweepMinuteUTC`）
- 后台 goroutine 每分钟 `Tick` 一次，仅在整点匹配时执行
- 环境变量 `OMA_MEMORY_RETENTION_DISABLED=1` 可关闭

清理 SQL（`PruneVersionsOlderThan`）：

- 删除 `created_at < cutoff` 的版本
- **保留**每个 `memory_id` 的**最新一条**版本（即使早于 cutoff）

当前 memory **正文**不受 retention 影响，仅 pruning `memory_versions` 审计表。

## 与 Agent 运行时集成

### Environment Resource 声明

Environment snapshot 的 `resources`（或 `config.resources`）可声明：

```json
{
  "type": "memory_store",
  "memory_store_id": "memstore-xxx",
  "access": "read_only"
}
```

### ResourceResolver + Mounter（已接入回合）

**平台侧**（`internal/session/machine.go`）：每个 turn 在调用 Harness 前执行 `Resources.ResolveForTurn`，把 Environment 里的 resource spec 展开为带正文的 payload。

**Harness 侧**（`harness/oma_adapter/turn.py`）：在投影 prompt 之前调用 `mount_resources(workdir, resources)`，将 memory 落到沙箱：

```text
{workdir}/mnt/memory/{store_name}/{path}
```

`internal/harness/resources.go` 中的 **ResourceResolver** 负责把 resource spec 解析为 Harness 可消费的 JSON：

```json
{
  "type": "memory_store",
  "store_id": "memstore-xxx",
  "store_name": "research-notes",
  "read_only": true,
  "memories": [
    { "path": "/notes/topic.md", "content": "..." }
  ]
}
```

解析流程：

1. `GetStore` 校验 store 存在
2. `ListMemories` 拉取全部 path
3. 对 blob offload 条目调用 `GetMemory` hydrate 全文
4. 按 `access: read_only` 设置 `read_only` 标志

`harness/oma_adapter/resource_mounter.py` 中 `_mount_memory_store` 将每条 `{path, content}` 写成 UTF-8 文件；挂载失败时 **best-effort 跳过**，不阻断回合。

### 与 Session 事件 / piPy 内存的区别

- **Session 事件**：对话与工具轨迹，存在 `session_events`，每 turn 作为 `events` 传给 Harness
- **Memory Store**：独立持久层；回合内仅通过 **文件挂载** 进入 Agent 上下文
- **`turn.py` 的 `in_memory=True`**：piPy `create_agent_session` 的进程内会话对象，与 Memory Store **无关**

## 架构总览

```text
                    ┌─────────────────────────────────────┐
                    │           Console / API Client       │
                    └─────────────────┬───────────────────┘
                                      │ REST /v1/memory_stores/*
                                      ▼
                    ┌─────────────────────────────────────┐
                    │  internal/api/memory_stores.go       │
                    │  serialize · precondition · actor    │
                    └─────────────────┬───────────────────┘
                                      │
                                      ▼
                    ┌─────────────────────────────────────┐
                    │  internal/store/memory_stores.go     │
                    │  MemoryStoreRepo                     │
                    │  · CRUD store / memory / version     │
                    │  · persistContent / hydrateMemory    │
                    └──────────┬──────────────┬─────────────┘
                               │              │
              SQLite           │              │  blob > 4 KiB
         (oma.db)              │              ▼
                               │   ┌──────────────────────────┐
                               │   │ internal/memoryblob      │
                               │   │ MEMORY_DATA_DIR          │
                               │   │ t/{tenant}/memory/...    │
                               │   └──────────────────────────┘
                               │
                               ▼
                    ┌─────────────────────────────────────┐
                    │  memory_versions (audit)             │
                    │  ◄── RetentionWorker (daily prune)   │
                    └─────────────────────────────────────┘

  Environment.resources ──► ResourceResolver.ResolveForTurn
                                      │
                                      ▼ TurnRequest.resources
                              turn.py → mount_resources
                                      │
                                      ▼
                         workdir/mnt/memory/{store_name}/...
                                      │
                                      ▼
                              Agent 文件类工具读取
```

## 与 Cloudflare 原版的差异

| 能力 | CF 参考实现 | oma-platform MVP |
|------|-------------|------------------|
| 元数据 | D1 + API | SQLite + `/v1/memory_stores` ✅ |
| 大对象 | R2 + FUSE | 本地 `MEMORY_DATA_DIR` ✅ |
| 写审计 | Queue → memory_versions | 同步 insert version ✅ |
| 版本 retention | Cron | `RetentionWorker` ✅ |
| Agent 挂载 | `resource-mounter.ts` | `ResourceResolver` + `resource_mounter.py` ✅ |
| 回合写回 store | FUSE / queue | 无；仅 API 写入 🟡 |
| 索引 / 搜索 | R2 event → queue | ❌ 未实现 |
| 多 replica 一致性 | Durable Object / R2 | 单进程 SQLite 🟡 |

## 关键源码索引

| 模块 | 路径 |
|------|------|
| 领域模型与仓储 | `internal/store/memory_stores.go` |
| Inline/blob 策略 | `internal/store/memory_blob_helpers.go` |
| 版本 prune | `internal/store/memory_retention.go` |
| 文件 blob | `internal/memoryblob/store.go` |
| Retention 定时任务 | `internal/memory/retention.go` |
| HTTP API | `internal/api/memory_stores.go` |
| Harness 资源解析 | `internal/harness/resources.go` |
| 回合资源解析调用 | `internal/session/machine.go` |
| Harness 挂载 | `harness/oma_adapter/resource_mounter.py` |
| 挂载测试 | `harness/tests/test_resource_mounter.py` |
| 服务启动 wiring | `cmd/oma-server/main.go` |
| 集成测试 | `internal/api/memory_evals_test.go` |
| Blob 单元测试 | `internal/store/memory_blobs_test.go` |

## 当前限制与后续工作

1. **无回合写回**：Agent 在 `workdir/mnt/memory/...` 的修改不会同步到 Memory Store；持久化更新仍需调用 REST API，或未来实现 memory 工具 / FUSE 写回队列。
2. **列表无 content / 无 search**：`GET .../memories` 仅 metadata；大批量 store 需要分页与检索。
3. **挂载为全量 snapshot**：`ResolveForTurn` 每次拉取 store 内全部 memory（大 store 有 latency / 体积成本）。
4. **单节点存储**：SQLite + 本地 blob 适合开发与小规模部署；生产多副本需外置 DB 与对象存储。
5. **Eval 联动**：Eval runs 与 memory store 同批迁移（`010_memory_evals.sql`），eval 背景 worker 见 [eval-run-background-worker.md](./eval-run-background-worker.md)。

---

*文档基于 oma-platform 当前代码库；若实现变更，以源码与迁移文件为准。*
