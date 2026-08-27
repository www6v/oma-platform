# Managed Agents Cookbook — 迁移调研与开新路线图

> **来源：** Anthropic [`managed_agents/`](https://github.com/anthropics/claude-cookbooks/tree/main/managed_agents)（本地路径：`claude-cookbooks-main/managed_agents/`）  
> **目标：** 梳理全部 cookbook，对齐 meta-harness 现状，按「补齐系统明显短板 / 大缺口」优先级决定开新顺序。  
> **关联：** `sdk/SDK-PLAN.md` § Cookbook parity；已落地迁移清单见本目录下专项文档。

---

## 1. Cookbook 全量清单

### 1.1 Guided tutorials（12 个 `.ipynb`）

| # | Notebook | 核心能力 |
|---|----------|----------|
| 1 | `CMA_iterate_fix_failing_tests.ipynb` | agent/env/session/resources + stream loop（**入门**） |
| 2 | `CMA_gate_human_in_the_loop.ipynb` | custom tool + `requires_action` + HITL round-trip |
| 3 | `CMA_orchestrate_issue_to_pr.ipynb` | 多轮 steering + mock `gh` + CI/review 失败恢复 |
| 4 | `CMA_explore_unfamiliar_codebase.ipynb` | zip mount + 探索 + **`sessions.resources.add`** |
| 5 | `CMA_prompt_versioning_and_rollback.ipynb` | agent 版本 + session pin + 批量评估 / 回滚 |
| 6 | `CMA_operate_in_production.ipynb` | vault MCP + **`session.status_idled` webhook** + 资源 CRUD |
| 7 | `CMA_remember_user_preferences.ipynb` | **memory_store** 跨 session 召回 + per-resource `instructions` |
| 8 | `CMA_coordinate_specialist_team.ipynb` | **multiagent** 协调 + 分角色 toolset + `thread_created` |
| 9 | `CMA_verify_with_outcome_grader.ipynb` | **`user.define_outcome`** + grade-revise 循环 + rubric |

### 1.2 Applied cookbooks（3 个）

| Notebook | 说明 |
|----------|------|
| `data_analyst_agent.ipynb` | CSV → HTML 报告；env + file mount + 流式 turn + Files 产物 |
| `slack_data_bot.ipynb` | 复用 analyst agent；Slack 线程续聊 + webhook 推送进度 |
| `sre_incident_responder.ipynb` | Skill + custom tools + alert webhook + HITL 合并（on-call 路径） |

### 1.3 配套应用（非 notebook）

| 目录 | 角色 |
|------|------|
| `slack/` | `@mention` → session + `session.status_idled` webhook → `chat.postMessage` |
| `linear/` | Linear webhook → CMA session |
| `sentry/` | `deployments.create` + vault MCP + 定时 triage |
| `self_hosted_sandboxes/` | Modal / Daytona / CF / Docker / Vercel webhook runners |
| `cma-mcp/` | MCP server 示例 |

Fixture 地图：`example_data/OVERVIEW.md`（iterate / orchestrate / gate 等离线数据）。

---

## 2. OMA 已覆盖（不必重复开新）

| Cookbook | 状态 | OMA 证据 |
|----------|------|----------|
| **data_analyst** | ✅ | `sdk/example/example1/`；Go `TestDataAnalystCookbook*`；E1 env pip；[gap analysis](../sdk/data-analyst-cookbook-gap-analysis.md) |
| **iterate** | ✅ | `sdk/example/example2/`；Go `TestIterateCookbookMultiTurn`；`oma_sdk/cookbook.py`；`tests/test_iterate_cookbook.py` |
| **gate HITL** | ✅ | `sdk/example/example3/`；Go `TestGateCookbook*`；Console HITL UI；[gate-hitl-gt1-gt3-migration.md](./gate-hitl-gt1-gt3-migration.md) |
| **outcome grader** | ✅（2026-07-04） | `sdk/example/example4/`；Go `TestOutcomeGraderCookbook`；[outcome-grader-migration.md](./outcome-grader-migration.md) |

上述四条构成递进验证链：**基础 loop → 多轮 → custom tool HITL → Outcome 监督**。继续投入的边际收益低于未覆盖 cookbook。

---

## 3. 全量 Parity 矩阵

图例：**✅** example + CI 闭环 / **🟡** API 有、runtime 或探针缺 / **❌** 平台大缺口

| Cookbook | 状态 | 主要缺口 | 开新优先级 | 开新的价值 |
|----------|------|----------|------------|------------|
| `data_analyst_agent` | ✅ | 可选：`client.beta.files` alias（T20） | defer | 已结项 |
| `CMA_iterate_fix_failing_tests` | ✅ | 同 files-path 细微差异 | defer | 已结项 |
| `CMA_gate_human_in_the_loop` | ✅ | GT6 Part B webhooks；live 12-receipt soak 可选 | defer | 已结项 |
| `CMA_verify_with_outcome_grader` | ✅ | OG4 file rubric ✅；rule-based verifier 可选 | defer | Phase A–D 已落地 |
| `CMA_coordinate_specialist_team` | ✅ | `example5/coordinate_team` + Go `TestCoordinateCookbook*` | defer | Phase CT1–CT3 landed |
| `CMA_remember_user_preferences` | ✅ | `example6/remember_preferences` + Go `TestRememberCookbook*` | defer | RP1–RP5 landed |
| `CMA_explore_unfamiliar_codebase` | ✅ | `example7/explore_unfamiliar_codebase` + Go `TestExploreCookbook*` | defer | EX1–EX4 landed |
| `CMA_orchestrate_issue_to_pr` | ✅ | `example8/orchestrate_issue_to_pr` + Go `TestOrchestrateCookbook*` | defer | OR1–OR4 landed |
| `sre_incident_responder` | ✅ | `example9/sre_incident_responder` + Go `TestSreCookbook*` | defer | SK1–SK4 + SRE probe landed |
| `CMA_prompt_versioning_and_rollback` | 🟡 | agent versioning API ✅；无批量评估 / 回滚 workflow 探针 | **P2** | 偏 workflow/DX，平台 API 已齐 |
| `CMA_operate_in_production` | ✅ | `example10/operate_in_production` + Go `TestOperateCookbook*`；**`beta.webhooks` out of scope** | defer | 见 [operate-in-production-migration.md](./operate-in-production-migration.md) |
| `slack_data_bot` | ❌ | 无 SDK example；Slack gateway 仅签名级测试 | **P2** | 集成层，依赖 analyst ✅ |

---

## 4. 缺口分层（开新应对准哪一层）

```
Layer A — 平台 runtime 缺口（开 cookbook 会暴露硬失败）
├── Skill → harness turn  injection                          ← sre ✅ SK1–SK4
├── github_repository 真实 clone（或 mock 等价）         ← orchestrate sidebar
└── （已关闭）Outcome grader 循环                       ← verify ✅ example4

Layer B — API 有、缺 cookbook 探针（开新 = 补 CI / 文档）
├── memory 跨 session 召回                             ← remember
├── multiagent 3 专家协调                              ← coordinate
├── ~~sessions.resources.add mid-session~~                 ← explore ✅
└── ~~长链 issue→PR mock gh~~                               ← orchestrate ✅

Layer C — 集成 / 生产模式（刻意 defer）
├── beta.webhooks / session.status_idled               ← operate, slack, gate Part B
├── 真实 Slack / Linear / PagerDuty                    ← applied bots
└── self_hosted_sandboxes                              ← cloud runtime 范畴
```

**原则：** 优先打 **Layer A + B**；避免在 webhook / 真实 SaaS 上过早投入。

---

## 5. 推荐开新顺序

### 5.1 阶段 1 — 补平台短板（P0）

| 顺序 | Cookbook | 建议 example | 先要补的平台 |
|------|----------|--------------|--------------|
| ~~1~~ | ~~`CMA_verify_with_outcome_grader`~~ | ~~`example4/outcome_grader`~~ | ✅ 见 [outcome-grader-migration.md](./outcome-grader-migration.md) |
| **1** | ~~`CMA_coordinate_specialist_team`~~ | ~~`example5/coordinate_team`~~ | ✅ 见 [coordinate-specialist-team-migration.md](./coordinate-specialist-team-migration.md) |
| **1** | ~~`CMA_remember_user_preferences`~~ | ~~`example6/remember_preferences`~~ | ✅ 见 [remember-user-preferences-migration.md](./remember-user-preferences-migration.md) |

### 5.2 阶段 2 — 扩能力边界（P1）

| Cookbook | 价值 |
|----------|------|
| ~~`CMA_explore_unfamiliar_codebase`~~ | ~~`example7/explore_unfamiliar_codebase`~~ | ✅ 见 [explore-unfamiliar-codebase-migration.md](./explore-unfamiliar-codebase-migration.md) |
| ~~`CMA_orchestrate_issue_to_pr`~~ | ~~`example8/orchestrate_issue_to_pr`~~ | ✅ 见 [orchestrate-issue-to-pr-migration.md](./orchestrate-issue-to-pr-migration.md) |

### 5.3 阶段 3 — Applied / 生产（P2，前置完成后）

| Cookbook | 前置依赖 |
|----------|----------|
| `sre_incident_responder` | Skill mount + custom tools（gate ✅）+ webhook stub |
| `slack_data_bot` | analyst ✅ + session 续聊 + 简易 webhook 模拟 |
| `CMA_operate_in_production` | webhooks 产品决策 + vault MCP E2E | ✅ 见 [operate-in-production-migration.md](./operate-in-production-migration.md) |
| `CMA_prompt_versioning_and_rollback` | agent version pin 已有，补 eval 脚本即可 |

---

## 6. Example 目录规划

```
sdk/example/
├── example1/  data_analyst              ✅
├── example2/  iterate                   ✅
├── example3/  gate HITL                 ✅
├── example4/  outcome_grader            ✅（含 ev_charging 变体 / soak）
├── example5/  coordinate_team           ✅
├── example6/  remember_preferences      ✅
├── example7/  explore_codebase          ✅
├── example8/  orchestrate_issue_pr      ✅
└── example10/ operate_in_production     ✅
```

每条遵循同一模板：

- `*.py` launcher + `*_main.py`
- fixtures under `exampleN/...`
- Go `*SimulatingClient` + `internal/integrationtest/*_flow.go`
- `Test*Cookbook*` in `internal/api/` + `test/integration/`
- SDK pytest + `SDK-PLAN.md` 章节 + 本目录专项 migration doc（可选）

---

## 7. 部分能力 — 已有实现 vs 缺探针

| 能力 | API / 代码现状 | Cookbook 探针 |
|------|----------------|---------------|
| Session resources CRUD | ✅ T16；`session_resources_handlers.go` | explore ✅；orchestrate ✅ |
| Memory stores | ✅ SDK `tests/test_memory_stores.py`；harness `resource_mounter.py` | remember 未开 |
| Multiagent / subagent | ✅ `tests/test_subagents.py`；Go `subagent_e2e_test.go` | coordinate 未开 |
| Agent versions | ✅ agents CRUD + versions | prompt_versioning 未开 |
| Vaults + integrations | ✅ vaults API；GitHub/Slack/Linear gateway 测试 | operate / slack 未开 |
| Skills | ✅ skills CRUD + **harness turn inject** | sre ✅ |
| Webhooks | ❌ `beta.webhooks` out of scope（SDK-PLAN） | operate / slack Part B |
| Outcome supervisor | ✅ example4 + `outcome_supervisor.go` | verify ✅ |

---

## 8. 下一个开新建议（2026-07-04 更新）

**Layer A 已关闭：** Skill harness 注入 — [skill-harness-injection-migration.md](./skill-harness-injection-migration.md)

**SRE incident responder ✅** — [sre-incident-responder-migration.md](./sre-incident-responder-migration.md)

~~explore / orchestrate / remember / coordinate / sre~~ ✅ — 见各 migration doc。

**下一步候选：** `slack_data_bot.ipynb` 或 `CMA_prompt_versioning_and_rollback.ipynb`（需 webhooks / eval workflow）。

**CMA_operate_in_production ✅** — [operate-in-production-migration.md](./operate-in-production-migration.md)

---

## 9. 相关文档

| 文档 | 内容 |
|------|------|
| [gate-hitl-gt1-gt3-migration.md](./gate-hitl-gt1-gt3-migration.md) | Gate HITL GT1–GT5 + Console HITL |
| [outcome-grader-migration.md](./outcome-grader-migration.md) | Outcome grader OG1–OG4 + example4 |
| [../sdk/data-analyst-cookbook-gap-analysis.md](../sdk/data-analyst-cookbook-gap-analysis.md) | Data analyst 专项 gap |
| [../../sdk/SDK-PLAN.md](../../sdk/SDK-PLAN.md) | SDK parity 总表 |
| Anthropic README | `claude-cookbooks-main/managed_agents/README.md` |

---

## 10. 变更记录

| 日期 | 说明 |
|------|------|
| 2026-07-09 | `CMA_operate_in_production` ✅：vault_ids + vault-scoped MCP + example10 + Go/pytest CI |
| 2026-07-04 | 初版：全量 cookbook 调研、parity 矩阵、分层缺口、开新路线图；同步 example4 outcome grader ✅ |
