# Operate in production migration

> **Source:** Anthropic `managed_agents/CMA_operate_in_production.ipynb`  
> **Reference:** `@open-managed-agents` vault + MCP injection (no outbound `beta.webhooks`)  
> **Status:** ✅ Phase OP1–OP5 (2026-07-09)

---

## Cookbook summary

Create a per-user vault and `static_bearer` credential for GitHub MCP, bind `vault_ids` on session create, run an MCP tool turn, document async HITL via `session.status_idled` (deferred), and exercise agent resource lifecycle (§6 via existing agent versions API).

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **OP1** | `vault_ids` column + session create/list wire | ✅ | migration `021_sessions_vault_ids.sql`, `sessions.go` |
| **OP2** | Vault-scoped MCP `FindActiveByMcpURLInVaults` + fallback | ✅ | `credentials.go`, `mcpproxy/target.go` |
| **OP3** | SDK `sessions.create(vault_ids=...)` | ✅ | `oma_sdk/api/sessions.py`, `test_sessions.py` |
| **OP4** | Go CI: vault MCP auth + two-vault isolation | ✅ | `TestMcpProxyVault*`, `TestOperateCookbook*` |
| **OP5** | example10 + pytest | ✅ | `example10/operate_in_production.py` |
| **OP6** | Webhooks (`beta.webhooks` / `session.status_idled`) | defer | SSE `wait_for_idle_status` — see below |
| **OP7** | Live soak (optional) | ✅ | `OMA_RUN_LIVE_OPERATE=1` pytest |

---

## OMA mapping

| Cookbook | OMA |
|----------|-----|
| `vaults.create` + `credentials.create` | `/v1/vaults` + `/v1/vaults/{id}/credentials` |
| `sessions.create(vault_ids=[...])` | `POST /v1/sessions` + `vault_ids` field |
| Agent `mcp_servers` + `mcp_toolset` (no inline token) | agents API + MCP proxy injection |
| Vault-backed MCP turn | `/v1/mcp-proxy/{sid}/{server}` + `mcpproxy.Resolver` |
| `session.status_idled` webhook → async HITL | **Not implemented** — neither OMA nor open-managed-agents ships outbound webhooks; use SSE poll / `wait_for_idle_status` (same defer pattern as SRE6 / gate GT6) |
| Session resources CRUD | ✅ explore example7 — referenced, not duplicated |
| Agent versions §6 | ✅ `TestAgentVersionsAPI` — light probe optional |

---

## Webhooks defer (OP6)

The cookbook §5 shows a FastAPI handler for `session.status_idled` that inspects pending custom tools. That pattern is **documentation-only** in the upstream notebook.

For OMA self-hosted:

1. **Today:** Poll `GET /v1/sessions/{id}` or tail SSE until `session.status_idle` + desired `stop_reason`.
2. **SDK:** `wait_for_idle_status()` in `oma_sdk/cookbook.py`.
3. **Not in scope:** `client.beta.webhooks.create` — see `sdk/SDK-PLAN.md` (out of scope; client-side unwrap only).

Gate example3 Part B points here: `gate_human_in_the_loop_main.py`.

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestOperateCookbook -count=1 -v
go test ./internal/api/ -run TestMcpProxyVault -count=1 -v
pytest sdk/tests/test_operate_cookbook.py sdk/tests/test_sessions.py::test_sessions_vault_ids -v -k "not live"
```

Live soak:

```bash
OMA_RUN_LIVE_OPERATE=1 GITHUB_TOKEN=... pytest sdk/tests/test_operate_cookbook.py -v -s
```

---

## Related

- [managed-agents-cookbook-roadmap.md](./managed-agents-cookbook-roadmap.md)
- [gate-hitl-gt1-gt3-migration.md](./gate-hitl-gt1-gt3-migration.md) (GT6 webhooks defer)
- [../design/vault-and-credentials.md](../design/vault-and-credentials.md)
