# OMA SDK cookbook parity examples

Thin launchers pick Python 3.11+ via `_run_with_python3.py`; main logic lives in
`*_main.py`.

| Cookbook | Launcher | Main | Fixtures |
|---|---|---|---|
| `data_analyst_agent.ipynb` | `example1/data_analyst_agent.py` | `example1/data_analyst_agent_main.py` | `example1/data/` |
| `CMA_iterate_fix_failing_tests.ipynb` | `example2/iterate_fix_failing_tests.py` | `example2/iterate_fix_failing_tests_main.py` | `example2/iterate/` |
| `CMA_gate_human_in_the_loop.ipynb` | `example3/gate_human_in_the_loop.py` | `example3/gate_human_in_the_loop_main.py` | `example3/gate/` |
| `CMA_coordinate_specialist_team.ipynb` | `example5/coordinate_team.py` | `example5/coordinate_team_main.py` | `example5/northstar/` |
| `CMA_remember_user_preferences.ipynb` | `example6/remember_preferences.py` | `example6/remember_preferences_main.py` | (inline fixtures) |
| `CMA_explore_unfamiliar_codebase.ipynb` | `example7/explore_unfamiliar_codebase.py` | `example7/explore_unfamiliar_codebase_main.py` | `example7/explore_fixtures.py` |
| `CMA_orchestrate_issue_to_pr.ipynb` | `example8/orchestrate_issue_to_pr.py` | `example8/orchestrate_issue_to_pr_main.py` | `example8/orchestrate/` |
| `sre_incident_responder.ipynb` | `example9/sre_incident_responder.py` | (inline in launcher) | `example9/sre/` |
| CT6 live soak | `example5/coordinate_team_live.py` | `example5/coordinate_team_live_main.py` | (same fixtures) |

Shared streaming helpers: `oma_sdk.cookbook` (`stream_until_end_turn`,
`stream_hitl_until_end_turn`, `wait_for_idle_status`).

Gate HITL CI: Go `TestGateCookbook*`; SDK `tests/test_gate_cookbook.py`; harness
`tests/test_custom_tools.py` (see `.github/workflows/ci.yml`).

Legacy workaround copy: `example1/v1/` (deprecated).
