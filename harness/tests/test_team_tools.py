"""Tests for pi_team tools and extension wiring in oma harness."""

from __future__ import annotations

from oma_adapter.tools import resolve_teams_extension_path, session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot


def test_teams_extension_registered_when_enabled() -> None:
    agent = AgentSnapshot(
        id="agt-lead",
        name="lead",
        model="test-model",
        metadata={"enable_team_tools": True},
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("team_extension.py" in path for path in cfg.extension_paths)


def test_resolve_teams_extension_path_default() -> None:
    path = resolve_teams_extension_path()
    assert path.name == "team_extension.py"
    assert path.is_file()
    assert "team_extension.py" in str(path)
