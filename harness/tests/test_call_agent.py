"""Tests for call_agent delegation wiring."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from oma_adapter.call_agent.delegate import delegate_to_agent
from oma_adapter.call_agent.runtime import CallAgentRuntime, configure_call_agent
from oma_adapter.tools import session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot, CallableAgentRef


def test_delegate_emits_thread_created(monkeypatch: pytest.MonkeyPatch) -> None:
    emitted: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def fake_sub_turn(**kwargs: Any) -> Any:
        del kwargs
        from oma_adapter.types import TurnResponse

        return TurnResponse(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "done"}],
                }
            ]
        )

    monkeypatch.setattr(
        "oma_adapter.call_agent.delegate.run_sub_agent_turn",
        fake_sub_turn,
    )

    parent = AgentSnapshot(id="parent", name="Parent", model="faux/test")
    worker = AgentSnapshot(id="worker", name="Worker", model="faux/test")
    configure_call_agent(
        CallAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            sub_agents={"worker": worker},
            emit_event=fake_emit,
        )
    )

    result = asyncio.run(delegate_to_agent("worker", "scan repo"))
    assert result == "done"
    types = [ev["type"] for ev in emitted]
    assert "session.thread_created" in types
    assert "session.thread_idle" in types
    created = next(ev for ev in emitted if ev["type"] == "session.thread_created")
    assert created["agent_id"] == "worker"
    assert created["parent_thread_id"] == "sthr_primary"


def test_session_tool_config_includes_call_agent_extension() -> None:
    agent = AgentSnapshot(
        id="parent",
        name="Parent",
        model="faux/test",
        callable_agents=[CallableAgentRef(id="worker")],
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("call_agent.py" in path for path in cfg.extension_paths)


def test_call_agent_extension_registers_tools() -> None:
    pytest.importorskip("pi_agent")
    from oma_adapter.extensions import call_agent as call_agent_ext

    class FakePi:
        def __init__(self) -> None:
            self.tools: list[Any] = []

        def register_tool(self, tool: Any) -> None:
            self.tools.append(tool)

    parent = AgentSnapshot(
        id="parent",
        name="Parent",
        model="faux/test",
        callable_agents=[CallableAgentRef(id="worker")],
        metadata={"enable_general_subagent": True},
    )
    configure_call_agent(
        CallAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            sub_agents={},
        )
    )
    pi = FakePi()
    call_agent_ext.register(pi)
    names = {tool.name for tool in pi.tools}
    assert "call_agent_worker" in names
    assert "general_subagent" in names
