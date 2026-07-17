"""Tests for OMA workflow bootstrap helpers.

These exercise pure helpers (``collect_agent_steps``,
``build_multiagent_payload``) that live on the OMA side of the
``pi_dynamic_workflows`` / ``oma_adapter`` boundary. Lifecycle tests for
``OmaWorkflowBootstrap.setup``/``teardown`` would require mocking the
entire OMA SDK and SubAgentRuntime; those are covered by the
``test_subagent_live_harness.py`` / integration tests.
"""

from __future__ import annotations

from oma_adapter.workflow_bootstrap import (
    build_multiagent_payload,
    collect_agent_steps,
)


class TestCollectAgentSteps:
    def test_dedupes_by_name_action_model(self):
        spec = {
            "steps": [
                {
                    "name": "analyze",
                    "action": "llm_execute",
                    "params": {"prompt": "a", "model": "qwen3.7-plus"},
                },
                {
                    "name": "analyze",
                    "action": "llm_execute",
                    "params": {"prompt": "b", "model": "qwen3.7-plus"},
                },
                {
                    "name": "fetch",
                    "action": "web_search",
                    "params": {"query": "x"},
                },
                {
                    "name": "noop",
                    "action": "http_request",
                    "params": {"url": "https://example.com"},
                },
            ],
        }
        steps = collect_agent_steps(spec)
        names = [s["name"] for s in steps]
        assert names == ["analyze", "fetch"]

    def test_empty_when_no_agent_steps(self):
        spec = {"steps": [{"name": "x", "action": "parallel", "params": {}}]}
        assert collect_agent_steps(spec) == []


class TestBuildMultiagentPayload:
    def test_includes_versions(self):
        payload = build_multiagent_payload(
            ["agt_a", "agt_b"],
            versions={"agt_a": 2, "agt_b": 1},
        )
        assert payload["type"] == "coordinator"
        assert payload["agents"] == [
            {"type": "agent", "id": "agt_a", "version": 2},
            {"type": "agent", "id": "agt_b", "version": 1},
        ]
