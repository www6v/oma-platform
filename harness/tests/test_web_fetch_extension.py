from __future__ import annotations

import pytest

from oma_adapter.tools import WEB_FETCH_EXTENSION_PATH, session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot


@pytest.mark.asyncio
async def test_web_fetch_extension_registers_with_pipy_session() -> None:
    from pi_ai.providers.faux import (
        faux_assistant_message,
        faux_text,
        register_faux_provider,
    )
    from pi_coding_agent.sdk import CreateAgentSessionOptions, create_agent_session

    registration = register_faux_provider(
        models=[{"id": "web-fetch-ext", "name": "web-fetch-ext"}],
        handler=lambda _ctx: faux_assistant_message([faux_text("ok")]),
    )
    try:
        agent = AgentSnapshot(
            id="a",
            name="n",
            model="faux/web-fetch-ext",
            tools=[{"type": "agent_toolset_20260401"}],
        )
        cfg = session_tool_config_from_agent(agent)
        result = await create_agent_session(
            CreateAgentSessionOptions(
                model="faux/web-fetch-ext",
                tools=cfg.builtin_tools,
                extension_paths=cfg.extension_paths,
                in_memory=True,
            )
        )
        tool_names = {tool.name for tool in result.session._agent._tools}
        assert str(WEB_FETCH_EXTENSION_PATH) in cfg.extension_paths
        assert "web_fetch" in tool_names
    finally:
        registration.dispose()
