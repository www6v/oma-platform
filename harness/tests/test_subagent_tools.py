"""OMA harness wiring tests for sub-agent extension loading."""

from __future__ import annotations

from oma_adapter.tools import session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot, CallableAgentRef


def test_session_tool_config_includes_subagent_extension() -> None:
    agent = AgentSnapshot(
        id="parent",
        name="Parent",
        model="faux/test",
        callable_agents=[CallableAgentRef(id="worker")],
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("subagent_extension.py" in path for path in cfg.extension_paths)


def test_session_tool_config_includes_roles_only_subagent_extension() -> None:
    agent = AgentSnapshot(
        id="parent",
        name="Parent",
        model="faux/test",
        metadata={"default_subagent_roles": {"explore": "agt_worker"}},
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("subagent_extension.py" in path for path in cfg.extension_paths)


def test_subagent_harness_e2e_tool_wiring() -> None:
    agent = AgentSnapshot(
        id="agt_coord",
        name="Coordinator",
        model="faux/test",
        callable_agents=[CallableAgentRef(id="agt_worker")],
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("subagent_extension.py" in path for path in cfg.extension_paths)
