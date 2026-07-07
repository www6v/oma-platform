# Skill harness injection migration

> **Source:** open-managed-agents `apps/agent/src/harness/skills.ts` + `session-do.ts`  
> **Blocks:** `sre_incident_responder` and other Skill-based cookbooks  
> **Status:** ✅ Phase SK1–SK4 (2026-07-04)

---

## Summary

Agent `skills[]` configs now resolve at turn start, inline `SKILL.md` into the system prompt (platform reminders), and mount skill files under `home/user/.skills/<name>/` in the harness workdir — matching AMA progressive disclosure.

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **SK1** | Go `ResolveSkillsForTurn` (custom + builtin) | ✅ | `internal/harness/skills_resolver.go` |
| **SK2** | `TurnRequest.skills` wired in `machine.go` | ✅ | per-turn resolve from `agent.skills` |
| **SK3** | Harness mount + `skill_platform_reminders` | ✅ | `skill_mounter.py` + `turn.py` |
| **SK4** | Go CI probe | ✅ | `TestSkillHarnessInjection` |

---

## AMA → OMA mapping

| open-managed-agents | OMA |
|---------------------|-----|
| `resolveCustomSkills` + inline `<skill>` | `ResolveSkillsForTurn` → `system_prompt_addition` |
| `getSkillFiles` → `/home/user/.skills/` | `mount_skills()` → `home/user/.skills/` |
| `platformReminders` source `skill:*` | `compose_system_prompt` + `skill_platform_reminders` |
| Built-in anthropic skills | `store.BuiltinSkillByID` metadata fallback |

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/harness/ ./internal/api/ ./test/integration/ -run 'TestResolveSkills|TestSkillHarness' -count=1 -v
pytest harness/tests/test_skill_mounter.py -v
```

---

## Next

- `sre_incident_responder` cookbook (custom tools + skill + webhook stub)
- Optional: `/home/user/.skills` path rewrite in `sandbox_paths.py` for bash
