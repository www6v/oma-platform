"""Tests for call_agent delegation wiring."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from oma_adapter.call_agent.delegate import delegate_to_agent
from oma_adapter.call_agent.runtime import (
    CallAgentRuntime,
    configure_call_agent,
    get_call_agent_runtime,
)
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


def test_background_delegate_returns_task_id(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    emitted: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def slow_sub_turn(**kwargs: Any) -> Any:
        from oma_adapter.call_agent.runtime import get_call_agent_runtime
        from oma_adapter.types import TurnResponse

        runtime = get_call_agent_runtime()
        assert runtime is not None
        assert runtime.parent_thread_id.startswith("sthr_")
        await asyncio.sleep(0.05)
        return TurnResponse(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "async done"}],
                }
            ]
        )

    monkeypatch.setattr(
        "oma_adapter.call_agent.delegate.run_sub_agent_turn",
        slow_sub_turn,
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

    async def run() -> str:
        result = await delegate_to_agent(
            "worker",
            "scan repo",
            run_in_background=True,
        )
        runtime = get_call_agent_runtime()
        assert runtime is not None
        assert runtime.background_tasks
        await asyncio.gather(*runtime.background_tasks)
        return result

    result = asyncio.run(run())
    assert result.startswith("Background sub-agent started.")
    assert "task_id=sbtask_" in result
    types = [ev["type"] for ev in emitted]
    assert "session.sub_agent_started" in types
    assert "session.sub_agent_completed" in types
    assert "session.thread_idle" in types


def test_parallel_delegates_use_distinct_threads(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    seen_thread_ids: list[str] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        del event

    async def recording_sub_turn(**kwargs: Any) -> Any:
        from oma_adapter.types import TurnResponse

        seen_thread_ids.append(kwargs["thread_id"])
        agent = kwargs["agent"]
        await asyncio.sleep(0.02 if agent.id == "worker_a" else 0.01)
        return TurnResponse(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": f"done-{agent.id}"}],
                }
            ]
        )

    monkeypatch.setattr(
        "oma_adapter.call_agent.delegate.run_sub_agent_turn",
        recording_sub_turn,
    )

    parent = AgentSnapshot(id="parent", name="Parent", model="faux/test")
    configure_call_agent(
        CallAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            sub_agents={
                "worker_a": AgentSnapshot(
                    id="worker_a",
                    name="A",
                    model="faux/test",
                ),
                "worker_b": AgentSnapshot(
                    id="worker_b",
                    name="B",
                    model="faux/test",
                ),
            },
            emit_event=fake_emit,
            parent_thread_id="sthr_primary",
        )
    )

    async def run_parallel() -> tuple[str, str]:
        return await asyncio.gather(
            delegate_to_agent("worker_a", "task a"),
            delegate_to_agent("worker_b", "task b"),
        )

    a_result, b_result = asyncio.run(run_parallel())
    assert a_result == "done-worker_a"
    assert b_result == "done-worker_b"
    assert len(seen_thread_ids) == 2
    assert seen_thread_ids[0] != seen_thread_ids[1]
    assert all(tid.startswith("sthr_") for tid in seen_thread_ids)


def test_role_system_prompts() -> None:
    from oma_adapter.call_agent.roles import (
        agent_snapshot_with_role,
        role_system_prompt,
    )

    assert role_system_prompt("explore") is not None
    assert role_system_prompt("unknown") is None
    worker = AgentSnapshot(id="w", name="W", model="faux/test")
    explore = agent_snapshot_with_role(worker, "explore")
    assert explore.system_prompt != worker.system_prompt


def test_resolve_sub_agent_applies_metadata_role() -> None:
    from oma_adapter.call_agent.delegate import _resolve_sub_agent

    parent = AgentSnapshot(id="parent", name="Parent", model="faux/test")
    worker = AgentSnapshot(
        id="worker",
        name="Worker",
        model="faux/test",
        system_prompt="original",
        metadata={"subagent_role": "verify"},
    )
    configure_call_agent(
        CallAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            sub_agents={"worker": worker},
        )
    )
    resolved = _resolve_sub_agent("worker", get_call_agent_runtime())
    assert resolved is not None
    assert resolved.system_prompt != "original"
    assert "verification" in (resolved.system_prompt or "").lower()
