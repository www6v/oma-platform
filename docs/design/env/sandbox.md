# Sandbox 使用场景与生命周期

> 系统里 sandbox 的完整调用面：哪些地方会触发 sandbox、配置如何解析、容器何时创建/销毁。
> 配套设计文档：`docs/design/opensandbox-environment.md`、`docs/design/environment-sandbox-binding.md`。

## 哪些地方会用到 sandbox

整个系统里触发 sandbox 的点只有 4 处（按调用频率排序）：

| 触发点 | 位置 | 作用 |
|---|---|---|
| `Machine.RunTurn` | `internal/session/machine.go` | 每次 turn 开始时，按 session 绑定的 Environment 解析配置，调用 `AcquireWith`。整个 turn 里的工具执行都跑在这个 sandbox 里。 |
| `POST /v1/sessions/:id/exec` | `internal/api/session_exec.go` | 直接对 session 跑一条命令（不进 LLM）。和 Machine 走同一套解析逻辑。 |
| `POST /v1/sessions/:id/files/promote` | `internal/api/session_files.go` | 从 sandbox 里读文件出来（期望 sandbox 已经 acquired）。 |
| `DELETE /v1/sessions/:id` | `internal/api/sessions.go` | session 删除时触发 `Sandbox.Release`，销毁远端容器。 |

`local` provider 不算真正的 sandbox —— 它直接用 host 的 workdir 跑命令，没有任何远端调用。默认 environment（`{"type":"local"}`）走的就是这条路径。

## Sandbox 的生命周期

```
     Session 第一次需要跑命令
              │
              ▼
    ┌──────────────────────────┐
    │ Resolver.Resolve(env)    │  ← 读 session 绑定的 Environment.config
    │   合并 env 配置 + 全局   │     没配的字段继承全局
    │   得到 sandbox.Config    │
    └──────────────────────────┘
              │
              ▼ cfg.IsRemote()?
        ┌─────┴─────┐
        │ no        │ yes
        ▼           ▼
     Local      Registry.AcquireWith(ctx, cfg, opts)
     Executor         │
                      ▼
                 cache key = sessionID|provider
                 cache miss? ──► HTTP POST /v1/sandboxes
                                   (OpenSandbox / Litebox)
                                      │
                                      ▼
                                 GET /endpoints/:port  (拿 execd URL)
                                      │
                                      ▼
                                 GET /ping 轮询直到 ready
                                      │
                                      ▼
                                 mkdir /workspace
                                      │
                                      ▼
                                 缓存到 Registry, 返回 Executor
              │
              ▼
     Executor.Exec / ReadFile  （SSE stream / HTTP download）
              │
              ▼
     Session 被 DELETE
              │
              ▼
     Registry.Release(ctx, sessionID)
        │
        ▼
     HTTP DELETE /v1/sandboxes/:id
     同时按 sessionID|* 前缀清理缓存
```

## 几个值得知道的设计点

1. **缓存 key 带 provider 后缀**（`sessionID|provider`）—— 同一个 session 在不同 provider 下各自独立缓存。避免 env 配置切换 provider 后拿到错的手柄。

2. **Acquire 是 lazy 的** —— 只有真正要跑命令时才会创建容器。创建 session 本身不会触发任何 sandbox 调用。

3. **Fallback 是单向的** —— env 配置无效/未知类型/未知 provider 时，Resolver 返回全局配置 + `UsedFallback=true`。不会报错，不会阻塞 session。

4. **Release 用前缀匹配** —— `Release(ctx, sessionID)` 会清掉 `sessionID|*` 所有 provider 的缓存项，因为 session 删除时不知道它历史上用过哪些 provider。

5. **OpenSandbox 的两层 HTTP** —— Lifecycle server（`:18090`）管容器创建销毁；execd（`:44772`）管命令执行，通过 server proxy 暴露。所以 `Acquire` 一个 OpenSandbox 至少 3 次 HTTP 调用：create → endpoint → ping。

## Provider 一览

| Provider | 类型 | Per-env 绑定 | 备注 |
|---|---|---|---|
| `local` | 本地 | ✓ | 默认 environment。Host workdir 直接执行。 |
| `opensandbox` | 远端 | ✓ | Lifecycle + execd 两层 API。默认镜像 `python:3.12-slim`。 |
| `litebox` | 远端 | 已知但未接 | 走 bridge 协议。 |
| `e2b` | 远端 | 已知但未接 | 作为 forward-compat 占位。 |
| `daytona` | 远端 | 已知但未接 | 作为 forward-compat 占位。 |
| `boxrun` | 远端 | 已知但未接 | 作为 forward-compat 占位。 |

"已知但未接"意味着 env 配置里写这些 provider 不会报错（API 接受），但 resolve 时会 fallback 到全局配置，等后续实现。

## 相关代码入口

| 文件 | 角色 |
|---|---|
| `internal/sandbox/resolver.go` | `Resolver.Resolve` + `ValidateConfigJSON` + env config schema |
| `internal/sandbox/registry.go` | `Registry.Acquire / AcquireWith / Get / Release` |
| `internal/sandbox/opensandbox.go` | OpenSandbox lifecycle + execd HTTP 客户端 |
| `internal/sandbox/litebox.go` | Litebox bridge 客户端 |
| `internal/sandbox/provider.go` | `Config` struct + `LoadConfigFromEnv` |
| `internal/session/machine.go:loadEnvironmentView` | turn 开始时从 DB 加载 env 投影 |
| `internal/api/session_exec.go:loadEnvViewForExec` | exec 端点的同一套投影（避免 api 依赖 session 包） |
| `internal/api/environments.go` | `/v1/environments` CRUD，写前调 `ValidateConfigJSON` |

## 测试覆盖

- `internal/sandbox/resolver_test.go` —— Resolver fallback/override/inherit 的全矩阵
- `internal/sandbox/registry_test.go` —— 缓存 key、Release 前缀匹配
- `internal/api/environments_test.go` —— API 层的 schema 验证
- `internal/session/machine_environment_test.go` —— Machine + fake lifecycle 集成
- `scripts/e2e/smoke-environment-sandbox-binding-e2e.sh` —— 端到端 smoke（mock lifecycle），覆盖四个不变式：
  1. env 覆盖镜像 → sandbox create 用 env 的镜像（`/exec` 路径）
  2. 默认 env（local） → 不创建 sandbox
  3. env 没配镜像 → 继承全局镜像
  4. `POST /messages` 触发完整 `Machine.RunTurn` → 异步 worker 走同一套解析，sandbox create 用 env 镜像
