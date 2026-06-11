from oma_adapter.tools import DEFAULT_PIPY_TOOLS, pypi_tools_from_agent
from oma_adapter.types import AgentSnapshot


def test_default_tools_when_missing() -> None:
    agent = AgentSnapshot(id="a", name="n", model="m")
    assert pypi_tools_from_agent(agent) == DEFAULT_PIPY_TOOLS


def test_agent_toolset_maps_to_pipy_tools() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[{"type": "agent_toolset_20260401"}],
    )
    assert pypi_tools_from_agent(agent) == DEFAULT_PIPY_TOOLS


def test_glob_maps_to_find() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[
            {
                "type": "agent_toolset_20260401",
                "default_config": {"enabled": False},
                "configs": [{"name": "glob", "enabled": True}],
            }
        ],
    )
    assert pypi_tools_from_agent(agent) == ["find"]


def test_selective_bash_and_grep_only() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[
            {
                "type": "agent_toolset_20260401",
                "default_config": {"enabled": False},
                "configs": [
                    {"name": "bash", "enabled": True},
                    {"name": "grep", "enabled": True},
                ],
            }
        ],
    )
    assert pypi_tools_from_agent(agent) == ["bash", "grep"]


def test_default_config_disabled_with_empty_configs() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[
            {
                "type": "agent_toolset_20260401",
                "default_config": {"enabled": False},
                "configs": [],
            }
        ],
    )
    assert pypi_tools_from_agent(agent) == []


def test_unsupported_oma_tools_are_skipped() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[
            {
                "type": "agent_toolset_20260401",
                "default_config": {"enabled": False},
                "configs": [
                    {"name": "web_fetch", "enabled": True},
                    {"name": "web_search", "enabled": True},
                    {"name": "schedule", "enabled": True},
                    {"name": "read", "enabled": True},
                ],
            }
        ],
    )
    assert pypi_tools_from_agent(agent) == ["read", "web_fetch"]


def test_session_tool_config_loads_web_fetch_extension() -> None:
    from oma_adapter.tools import (
        WEB_FETCH_EXTENSION_PATH,
        session_tool_config_from_agent,
    )

    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[{"type": "agent_toolset_20260401"}],
    )
    cfg = session_tool_config_from_agent(agent)
    assert "web_fetch" in pypi_tools_from_agent(agent)
    assert cfg.extension_paths == [str(WEB_FETCH_EXTENSION_PATH)]
    assert "bash" in cfg.builtin_tools


def test_legacy_name_item() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[{"name": "edit"}, {"name": "browser"}],
    )
    assert pypi_tools_from_agent(agent) == ["edit"]
