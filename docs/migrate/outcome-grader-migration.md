# Outcome grader migration (example4)

> **Status (2026-07-04):** Phase A–D landed — in-session outcome supervisor,
> `OutcomeSimulatingClient`, Go CI `TestOutcomeGraderCookbook`, SDK
> `example/example4/outcome_grader.py`.
>
> **Gap ID:** `sdk/SDK-PLAN.md` § Cookbook parity — outcome grader (OG1–OG3)

## Goal

Port Anthropic cookbook **verify_with_outcome_grader** to oma-platform:

1. Accept `user.define_outcome` and persist active outcome state
2. After each harness turn, run LLM-as-judge (or test fake evaluator)
3. Emit `span.outcome_evaluation_{start,ongoing,end}`
4. On `needs_revision`, inject `<outcome_feedback>` `user.message` and re-run harness
5. Expose `outcome_evaluations[]` on GET `/v1/sessions/:id`

Reference implementation: `open-managed-agents/apps/agent/src/runtime/outcome-supervisor.ts`

## Phases

| Phase | Scope | Status |
|-------|-------|--------|
| A | Session metadata: `outcome`, `outcome_iteration`, `outcome_evaluations`; mint `outc_*` on define | ✅ |
| B | `RunOutcomeSupervisor` + hook in `Machine.RunTurn` after harness turn | ✅ |
| C | LLM judge via `harness.OutcomeEvaluator` / `POST /internal/evaluate-outcome` | ✅ (eval worker path reused) |
| D | `OutcomeSimulatingClient`, integration flow, SDK example4, CI | ✅ |

**Deferred (P2):** rule-based `verifier` RewardSpec path; trajectory verifiers; `span.outcome_evaluation_start` broadcast-only (we persist all spans for simpler replay).

**OG4 (2026-07-04):** `rubric: {type:"file",file_id}` resolves via Files API; cached as `outcome.rubric_content` in session metadata.

## Key files

| Area | Path |
|------|------|
| Outcome state | `internal/session/outcome_state.go` |
| Supervisor loop | `internal/session/outcome_supervisor.go` |
| Turn hook | `internal/session/machine.go` (`maybeRunOutcomeSupervisor`) |
| define_outcome activate | `internal/session/registry_enqueue.go` |
| API wire | `internal/api/sessionwire.go` |
| Rubric file resolve | `internal/session/resolve_rubric.go` |
| Integration flow | `internal/integrationtest/outcome_flow.go` |
| Go CI | `TestOutcomeGraderCookbook` in `internal/api/`, `test/integration/` |
| SDK probe (minimal) | `sdk/example/example4/outcome_grader.py` |
| SDK probe (OG5 EV) | `sdk/example/example4/outcome_grader_ev_charging.py` |

## Flow

```
user.define_outcome  → metadata.outcome (direct append)
user.message         → harness turn
turn_end             → outcome supervisor
  ├─ evaluate agent output (OutcomeEvaluator)
  ├─ emit span.outcome_evaluation_*
  ├─ needs_revision → user.message (<outcome_feedback>) → another harness turn
  └─ terminal (satisfied | max_iterations_reached | failed) → clear metadata.outcome
session.status_idle  → end_turn (no pending custom tools)
```

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestOutcomeGraderCookbook -count=1 -v
```

## OG gap matrix

| ID | Item | Status |
|----|------|--------|
| OG1 | `user.define_outcome` activates session outcome | ✅ |
| OG2 | Post-turn grade-revise loop + outcome spans | ✅ |
| OG3 | `outcome_evaluations` on GET session | ✅ |
| OG4 | Rubric file via Files API | ✅ |
| OG4b | RewardSpec verifier path | 🔲 P2 |
| OG5 | Live EV charging cookbook soak | ✅ opt-in |

## OG5 live soak

Full Anthropic ``CMA_verify_with_outcome_grader`` scenario:

```bash
# Manual
OMA_API_KEY=... OMA_BASE_URL=http://127.0.0.1:8787 \\
  python sdk/example/example4/outcome_grader_ev_charging.py

# Or wrapper script
./scripts/e2e/smoke-outcome-ev-charging-live-e2e.sh

# Pytest (opt-in)
OMA_RUN_LIVE_OUTCOME_EV=1 pytest sdk/tests/test_outcome_cookbook.py -v -s
```

Fixtures: `sdk/example/example4/ev_charging/{task,rubric,system_prompt}.*`

## Related

- Gate HITL migration: `docs/migrate/gate-hitl-gt1-gt3-migration.md`
- Eval-only outcome path: `docs/design/resource-mounter-and-outcome-evaluator.md`
- SDK plan: `sdk/SDK-PLAN.md`
