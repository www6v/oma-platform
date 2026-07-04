# Remember user preferences migration

> **Source:** Anthropic `managed_agents/CMA_remember_user_preferences.ipynb`  
> **Reference:** `@open-managed-agents` memory mount + FUSE sync pattern  
> **Status:** ✅ Phase RP1–RP3 (2026-07-04)

---

## Cookbook summary

Attach a `memory_store` resource with optional per-resource `instructions`. Session 1: agent writes preferences under `/mnt/memory/<store_name>/` via file tools. Session 2: a new session with the same store recalls persisted memories.

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **RP1** | `memory_store` resource → harness mount | ✅ | `resource_mounter.py` + `resolveMemoryStore` |
| **RP2** | Workdir → API memory sync after turn | ✅ | `harness/memory_sync.go` + `machine.go` |
| **RP3** | Cross-session recall Go CI | ✅ | `TestRememberCookbookPreferences` |
| **RP4** | Platform reminders + `instructions` | ✅ | `platform_guidance.py` + `resources.go` |
| **RP5** | SDK example6 + pytest | ✅ | `example6/remember_preferences.py` |
| **RP6** | Live LLM soak (optional) | ✅ | `OMA_RUN_LIVE_REMEMBER=1` pytest |

---

## OMA mapping (open-managed-agents parity)

| AMA | OMA |
|-----|-----|
| `/mnt/memory/<store_name>/` mount | `resource_mounter._mount_memory_store` |
| Agent uses read/write (not memory_* tools) | `agent_toolset_20260401` |
| FUSE/R2 write → memory API | `SyncMemoryStoresFromWorkdir` after harness turn |
| Per-resource `instructions` in system prompt | `memory_platform_reminders()` |

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestRememberCookbook -count=1 -v
pytest sdk/tests/test_remember_cookbook.py -v -k "not live"
```

Live soak::

    OMA_RUN_LIVE_REMEMBER=1 pytest sdk/tests/test_remember_cookbook.py::test_remember_preferences_live_soak -v -s

---

## Related

- [managed-agents-cookbook-roadmap.md](./managed-agents-cookbook-roadmap.md)
- open-managed-agents: `apps/agent/src/runtime/session-do.ts` (memory platformReminders)
