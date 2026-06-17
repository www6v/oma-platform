"""End-to-end harness tests for sub-agent delegation."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from oma_adapter.tools import session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot, CallableAgentRef
from pi_subagent.delegate import delegate_to_agent
from pi_subagent.runtime import SubAgentRuntime, configure_subagent_runtime
from pi_subagent.types import CallableAgentRef as SubCallableAgentRef
from pi_subagent.types import SubAgentSnapshot, SubTurnResult


def test_subagent_harness_e2e_delegate_pipeline() -> None:
    """Full delegate pipeline: thread lifecycle events + sub-turn + text result."""
    emitted: list[dict[str, Any]] = []
    sub_turn_calls: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def fake_sub_turn(**kwargs: Any) -> SubTurnResult:
        sub_turn_calls.append(kwargs)
        thread_id = kwargs["thread_id"]
        on_event = kwargs.get("on_event")
        if on_event is not None:
            await on_event(
                {
                    "type": "agent.message",
                    "session_thread_id": thread_id,
                    "content": [{"type": "text", "text": "worker pipeline ok"}],
                }
            )
        return SubTurnResult(
            events=[
                {
                    "type": "agent.message",
                    "session_thread_id": thread_id,
                    "content": [{"type": "text", "text": "worker pipeline ok"}],
                }
            ]
        )

    parent = SubAgentSnapshot(
        id="agt_coord",
        name="Coordinator",
        model="faux/test",
        callable_agents=[SubCallableAgentRef(id="agt_worker")],
    )
    worker = SubAgentSnapshot(
        id="agt_worker",
        name="Worker",
        model="faux/test",
        system_prompt="worker",
    )
    configure_subagent_runtime(
        SubAgentRuntime(
            session_id="sess_e2e",
            workdir="/tmp/oma-e2e",
            parent_agent=parent,
            run_sub_turn=fake_sub_turn,
            sub_agents={"agt_worker": worker},
            emit_event=fake_emit,
        )
    )

    async def run() -> str:
        return await delegate_to_agent("agt_worker", "run delegated task")

    result = asyncio.run(run())
    assert result == "worker pipeline ok"

    types = [ev["type"] for ev in emitted]
    assert types[0] == "session.thread_created"
    assert types[-1] == "session.thread_idle"
    assert "agent.message" in types

    created = emitted[0]
    thread_id = created["session_thread_id"]
    assert thread_id.startswith("sthr_")
    assert created["agent_id"] == "agt_worker"
    assert created["parent_thread_id"] == "sthr_primary"

    assert len(sub_turn_calls) == 1
    call = sub_turn_calls[0]
    assert call["thread_id"] == thread_id
    assert call["agent"].id == "agt_worker"
    assert call["message"] == "run delegated task"
    assert call["depth"] == 1


def test_subagent_harness_e2e_tool_wiring() -> None:
    """Coordinator with callable_agents loads subagent extension."""
    agent = AgentSnapshot(
        id="agt_coord",
        name="Coordinator",
        model="faux/test",
        callable_agents=[CallableAgentRef(id="agt_worker")],
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("subagent_extension.py" in path for path in cfg.extension_paths)
