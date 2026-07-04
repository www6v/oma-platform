# Coordinate specialist team migration

> **Source:** Anthropic `managed_agents/CMA_coordinate_specialist_team.ipynb`  
> **Target:** `sdk/example/example5/` + Go `TestCoordinateCookbook*`  
> **Status:** ✅ Phase CT1–CT6 (2026-07-04)

---

## Cookbook summary

Three specialist agents (`prospect_researcher`, `case_study_picker`, `pricing_modeler`) are wired into a coordinator via `multiagent`. Session resources mount product one-pager, pricing rules, and case studies under `/mnt/user-data/`. The coordinator delegates in parallel (researcher + pricer) and sequential (librarian after research), then writes `proposal.md`.

Events to validate:

- `session.thread_created` (×3)
- `agent.thread_message_received` (×3)
- Coordinator final output with proposal marker

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **CT1** | `multiagent` → `callable_agents` on agent create | ✅ | Existing `agentwire.go`; flow uses `multiagent` JSON |
| **CT2** | Harness `SubAgents` (3 workers) on turn | ✅ | `ResolveSubAgents` + `CoordinateSimulatingClient` validation |
| **CT3** | Thread events + Go CI | ✅ | `RunCoordinateSpecialistTeamFlow`; `TestCoordinateCookbookSpecialistTeam` |
| **CT4** | SDK example5 launcher | ✅ | `example5/coordinate_team.py` |
| **CT5** | SDK pytest (fixtures + helpers) | ✅ | `tests/test_coordinate_cookbook.py` |
| **CT6** | Live LLM soak | ✅ | `coordinate_team_live.py` + `OMA_RUN_LIVE_COORDINATE=1` pytest |

---

## OMA mapping

| Cookbook step | OMA |
|---------------|-----|
| Create specialists | `POST /v1/agents` |
| Coordinator roster | `multiagent: {type: coordinator, agents: [...]}` |
| Upload fixtures | `POST /v1/files` |
| Session resources | `POST /v1/sessions` with `resources[]` |
| Drive turn | `POST /v1/sessions/{id}/events` |
| Observe threads | `GET .../events`, `GET .../threads` |

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestCoordinateCookbook -count=1 -v
pytest sdk/tests/test_coordinate_cookbook.py -v

Live soak (CT6, not CI)::

    python sdk/example/example5/coordinate_team_live.py
    OMA_RUN_LIVE_COORDINATE=1 pytest sdk/tests/test_coordinate_cookbook.py::test_coordinate_team_live_soak -v -s
```

Sim client: `internal/harness/demo/coordinate_sim_client.go`  
Flow: `internal/integrationtest/coordinate_flow.go`

---

## Related

- [managed-agents-cookbook-roadmap.md](./managed-agents-cookbook-roadmap.md)
- [subagent design](../design/subagent.md)
- SDK: `oma_sdk/subagent.py` (`build_multiagent`, thread counters)
