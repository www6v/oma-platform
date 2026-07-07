# Explore unfamiliar codebase migration

> **Source:** Anthropic `managed_agents/CMA_explore_unfamiliar_codebase.ipynb`  
> **Reference:** `@open-managed-agents` session file mounts + mid-session resources  
> **Status:** ✅ Phase EX1–EX4 (2026-07-04)

---

## Cookbook summary

Mount an in-memory `repo.zip` with a stale `ARCHITECTURE.md` trap. Turn 1: agent explores and reports real `services/` microservices layout. Turn 2: read back `/tmp/NOTES.md`. Sidebar: `sessions.resources.add` for `DEPLOY_HISTORY.md`, follow-up turn, then `resources.delete`.

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **EX1** | `make_unfamiliar_repo_zip` fixture | ✅ | `example7/explore_fixtures.py` |
| **EX2** | `ExploreSimulatingClient` | ✅ | `demo/explore_sim_client.go` |
| **EX3** | Go integration (zip + mid-session add/delete) | ✅ | `TestExploreCookbookUnfamiliarCodebase` |
| **EX4** | SDK example7 + pytest | ✅ | `example7/explore_unfamiliar_codebase.py` |
| **EX5** | Live LLM soak (optional) | ✅ | `OMA_RUN_LIVE_EXPLORE=1` pytest |

---

## OMA mapping

| Cookbook | OMA |
|----------|-----|
| `repo.zip` at `mount_path: "repo.zip"` | `/mnt/session/uploads/repo.zip` via `resource_mounter._mount_file` |
| Agent unzips with bash | harness `agent_toolset_20260401` (live); sim validates zip bytes |
| `sessions.resources.add` mid-session | `POST /v1/sessions/{id}/resources` |
| `sessions.resources.delete` detach | `DELETE /v1/sessions/{id}/resources/{resource_id}` |
| Turn N sees new mounts | `machine.go` `ResolveForTurn` on each turn |

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestExploreCookbook -count=1 -v
pytest sdk/tests/test_explore_cookbook.py -v -k "not live"
```

Live soak::

    OMA_RUN_LIVE_EXPLORE=1 pytest sdk/tests/test_explore_cookbook.py::test_explore_unfamiliar_codebase_live_soak -v -s

---

## Related

- [managed-agents-cookbook-roadmap.md](./managed-agents-cookbook-roadmap.md)
