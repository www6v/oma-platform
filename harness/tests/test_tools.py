import pytest

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
    assert pypi_tools_from_agent(agent) == [
        "read",
        "web_fetch",
        "web_search",
        "schedule",
    ]


def test_session_tool_config_loads_web_fetch_extension() -> None:
    from oma_adapter.tools import (
        SCHEDULE_EXTENSION_PATH,
        WEB_FETCH_EXTENSION_PATH,
        WEB_SEARCH_EXTENSION_PATH,
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
    assert "web_search" in pypi_tools_from_agent(agent)
    assert "schedule" in pypi_tools_from_agent(agent)
    assert cfg.extension_paths == [
        str(WEB_FETCH_EXTENSION_PATH),
        str(WEB_SEARCH_EXTENSION_PATH),
        str(SCHEDULE_EXTENSION_PATH),
    ]
    assert "bash" in cfg.builtin_tools


def test_web_search_tavily_type_enables_tool() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[
            {
                "type": "agent_toolset_20260401",
                "default_config": {"enabled": False},
                "configs": [],
            },
            {"type": "web_search_tavily"},
        ],
    )
    assert "web_search" in pypi_tools_from_agent(agent)


def test_legacy_name_item() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[{"name": "edit"}, {"name": "browser"}],
    )
    assert pypi_tools_from_agent(agent) == ["edit"]


def test_gate_custom_tools_load_extension() -> None:
    from oma_adapter.tools import (
        CUSTOM_TOOLS_EXTENSION_PATH,
        session_tool_config_from_agent,
    )

    agent = AgentSnapshot(
        id="a",
        name="gate",
        model="m",
        tools=[
            {"type": "agent_toolset_20260401"},
            {
                "type": "custom",
                "name": "decide",
                "description": "approve/reject",
                "input_schema": {"type": "object"},
            },
            {
                "type": "custom",
                "name": "escalate",
                "description": "human review",
                "input_schema": {},
            },
        ],
    )
    cfg = session_tool_config_from_agent(agent)
    assert str(CUSTOM_TOOLS_EXTENSION_PATH) in cfg.extension_paths


@pytest.mark.asyncio
async def test_custom_tools_register_with_pipy_session() -> None:
    pytest.importorskip("pi_coding_agent")
    pytest.importorskip("pi_agent")
    from pi_ai.providers.faux import (
        faux_assistant_message,
        faux_text,
        register_faux_provider,
    )
    from pi_coding_agent.sdk import CreateAgentSessionOptions, create_agent_session

    from oma_adapter.custom_tools import (
        CustomToolDef,
        register_custom_tools_on_session,
    )
    from oma_adapter.tools import session_tool_config_from_agent

    registration = register_faux_provider(
        models=[{"id": "gate-custom", "name": "gate-custom"}],
        handler=lambda _ctx: faux_assistant_message([faux_text("ok")]),
    )
    try:
        agent = AgentSnapshot(
            id="a",
            name="gate",
            model="faux/gate-custom",
            tools=[
                {"type": "agent_toolset_20260401"},
                {
                    "type": "custom",
                    "name": "decide",
                    "description": "approve/reject",
                    "input_schema": {"type": "object"},
                },
                {
                    "type": "custom",
                    "name": "escalate",
                    "description": "human review",
                    "input_schema": {},
                },
            ],
        )
        cfg = session_tool_config_from_agent(agent)
        result = await create_agent_session(
            CreateAgentSessionOptions(
                model="faux/gate-custom",
                tools=cfg.builtin_tools,
                extension_paths=cfg.extension_paths,
                in_memory=True,
            )
        )
        register_custom_tools_on_session(
            result.session,
            [
                CustomToolDef(
                    name="decide",
                    description="approve/reject",
                    input_schema={"type": "object"},
                ),
                CustomToolDef(
                    name="escalate",
                    description="human review",
                    input_schema={},
                ),
            ],
        )
        tool_names = {tool.name for tool in result.session._agent._tools}
        assert "decide" in tool_names
        assert "escalate" in tool_names
    finally:
        registration.dispose()
