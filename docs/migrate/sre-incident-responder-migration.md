# SRE incident responder migration

> **Source:** Anthropic `managed_agents/sre_incident_responder.ipynb`  
> **Reference:** `@open-managed-agents` skills + resources + custom tools HITL  
> **Status:** ✅ Phase SRE1–SRE5 (2026-07-04)

---

## Cookbook summary

Upload an `incident-runbooks` skill, create an agent with `open_pull_request`, `request_approval`, and `merge_pull_request` custom tools, mount logs/manifest/runbook files, then handle a PagerDuty webhook alert. The agent triages OOMKilled checkout-svc, opens a PR, pauses on `request_approval`, and merges after human approval.

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **SRE1** | Fixtures (alert, logs, manifest, runbook) | ✅ | `example9/sre/` + `testdata/sre/` |
| **SRE2** | `SreSimulatingClient` + skill/resource validation | ✅ | `sre_sim_client.go` |
| **SRE3** | HITL flow open_pr → approval → complete Go CI | ✅ | `TestSreCookbookIncidentResponder` |
| **SRE4** | SDK example9 + pytest | ✅ | `sre_incident_responder.py` |
| **SRE5** | Live LLM soak (optional) | ✅ | `OMA_RUN_LIVE_SRE=1` pytest |
| **SRE6** | PagerDuty webhook handler (`beta.webhooks`) | defer | inline alert message in CI |

---

## OMA mapping

| Cookbook | OMA |
|----------|-----|
| `incident-runbooks/SKILL.md` | skills CRUD + harness turn inject (SK1–SK4) |
| Three `file` resources | session `resources[]` with mount paths |
| PagerDuty webhook | `user.message` with `alert.json` (webhook deferred) |
| `open_pull_request` / `merge_pull_request` | app answers inline via `on_custom_tool` |
| `request_approval` HITL | `requires_action` + `stream_hitl_until_end_turn` |

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestSreCookbook -count=1 -v
pytest sdk/tests/test_sre_cookbook.py -v -k "not live"
```

Live soak::

    OMA_RUN_LIVE_SRE=1 pytest sdk/tests/test_sre_cookbook.py::test_sre_incident_responder_live_soak -v -s

---

## Related

- [managed-agents-cookbook-roadmap.md](./managed-agents-cookbook-roadmap.md)
- [skill-harness-injection-migration.md](./skill-harness-injection-migration.md)
- [gate-hitl-gt1-gt3-migration.md](./gate-hitl-gt1-gt3-migration.md)
