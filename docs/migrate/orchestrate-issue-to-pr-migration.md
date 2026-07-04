# Orchestrate issue to PR migration

> **Source:** Anthropic `managed_agents/CMA_orchestrate_issue_to_pr.ipynb`  
> **Reference:** `@open-managed-agents` session mounts + multi-turn steering  
> **Status:** ✅ Phase OR1–OR4 (2026-07-04)

---

## Cookbook summary

Mount a zip of `example_data/orchestrate/` (mock `gh-mock`, planted Unicode bug in `url_utils.py`, pytest suite). Turn 1: agent runs the full issue→fix→PR→CI→review→merge chain with mid-chain recovery. Turn 2: verify persisted PR state in `/mnt/user/.gh-state/pr_101.json`. Sidebar: swap file mount for `github_repository` (deferred — harness skips real clone in CI).

---

## Phase checklist

| ID | Item | Status | Evidence |
|----|------|--------|----------|
| **OR1** | Orchestrate zip fixture (`gh-mock`, issue, src, tests) | ✅ | `example8/orchestrate/` + `testdata/orchestrate/` |
| **OR2** | Env `packages.pip: [pytest]` + `allow_package_managers` | ✅ | `env_packages.py` + sim env validation |
| **OR3** | Multi-turn chain + verify Go CI | ✅ | `TestOrchestrateCookbookIssueToPR` |
| **OR4** | SDK example8 + pytest | ✅ | `orchestrate_issue_to_pr.py` |
| **OR5** | Live LLM soak (optional) | ✅ | `OMA_RUN_LIVE_ORCHESTRATE=1` pytest |
| **OR6** | `github_repository` sidebar | defer | `resolveOne` stub; live token clone optional |

---

## OMA mapping

| Cookbook | OMA |
|----------|-----|
| Zip mount at `repo.zip` | `/mnt/session/uploads/repo.zip` |
| Work in `/mnt/user` | harness workdir `mnt/user/` (bash path rewrite) |
| `gh-mock` state in `.gh-state/` | session filesystem persists across turns |
| Env pre-install pytest | `environment.config.packages.pip` → `ensure_environment_packages` |
| Multi-turn verification | same session, two `user.message` turns (MT1 pattern) |

---

## CI

```bash
source scripts/go-env.sh
go test ./internal/api/ ./test/integration/ -run TestOrchestrateCookbook -count=1 -v
pytest sdk/tests/test_orchestrate_cookbook.py -v -k "not live"
```

Live soak::

    OMA_RUN_LIVE_ORCHESTRATE=1 pytest sdk/tests/test_orchestrate_cookbook.py::test_orchestrate_issue_to_pr_live_soak -v -s

---

## Related

- [managed-agents-cookbook-roadmap.md](./managed-agents-cookbook-roadmap.md)
- [explore-unfamiliar-codebase-migration.md](./explore-unfamiliar-codebase-migration.md) (zip mount pattern)
