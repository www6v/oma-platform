"""Tests for pi_subagent delegation wiring."""

from __future__ import annotations

import asyncio
from typing import Any

import pytest

from oma_adapter.tools import session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot, CallableAgentRef
from pi_subagent.delegate import delegate_to_agent, resolve_sub_agent
from pi_subagent.runtime import (
    SubAgentRuntime,
    configure_subagent_runtime,
    get_subagent_runtime,
)
from pi_subagent.types import CallableAgentRef as SubCallableAgentRef
from pi_subagent.types import SubAgentSnapshot, SubTurnResult


async def _noop_sub_turn(**kwargs: Any) -> SubTurnResult:
    del kwargs
    return SubTurnResult()


def _runtime(**overrides: Any) -> SubAgentRuntime:
    parent = SubAgentSnapshot(id="parent", name="Parent", model="faux/test")
    defaults: dict[str, Any] = {
        "session_id": "sess_1",
        "workdir": "/tmp",
        "parent_agent": parent,
        "run_sub_turn": _noop_sub_turn,
        "sub_agents": {},
    }
    defaults.update(overrides)
    return SubAgentRuntime(**defaults)


def test_delegate_emits_thread_created() -> None:
    emitted: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def fake_sub_turn(**kwargs: Any) -> SubTurnResult:
        del kwargs
        return SubTurnResult(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "done"}],
                }
            ]
        )

    parent = SubAgentSnapshot(id="parent", name="Parent", model="faux/test")
    worker = SubAgentSnapshot(id="worker", name="Worker", model="faux/test")
    configure_subagent_runtime(
        SubAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            run_sub_turn=fake_sub_turn,
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


def test_session_tool_config_includes_subagent_extension() -> None:
    agent = AgentSnapshot(
        id="parent",
        name="Parent",
        model="faux/test",
        callable_agents=[CallableAgentRef(id="worker")],
    )
    cfg = session_tool_config_from_agent(agent)
    assert any("subagent_extension.py" in path for path in cfg.extension_paths)


def test_subagent_extension_registers_tools() -> None:
    pytest.importorskip("pi_agent")
    import importlib.util
    from pathlib import Path

    ext_path = (
        Path(__file__).resolve().parents[1]
        / "extensions"
        / "subagent_extension.py"
    )
    spec = importlib.util.spec_from_file_location("subagent_extension", ext_path)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)

    class FakePi:
        def __init__(self) -> None:
            self.tools: list[Any] = []

        def register_tool(self, tool: Any) -> None:
            self.tools.append(tool)

    parent = SubAgentSnapshot(
        id="parent",
        name="Parent",
        model="faux/test",
        callable_agents=[SubCallableAgentRef(id="worker")],
        metadata={"enable_general_subagent": True},
    )
    configure_subagent_runtime(
        SubAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            run_sub_turn=_noop_sub_turn,
            sub_agents={},
        )
    )
    pi = FakePi()
    mod.register(pi)
    names = {tool.name for tool in pi.tools}
    assert "call_agent_worker" in names
    assert "general_subagent" in names


def test_background_delegate_returns_task_id() -> None:
    emitted: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def slow_sub_turn(**kwargs: Any) -> SubTurnResult:
        runtime = get_subagent_runtime()
        assert runtime is not None
        assert runtime.parent_thread_id.startswith("sthr_")
        await asyncio.sleep(0.05)
        return SubTurnResult(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "async done"}],
                }
            ]
        )

    parent = SubAgentSnapshot(id="parent", name="Parent", model="faux/test")
    worker = SubAgentSnapshot(id="worker", name="Worker", model="faux/test")
    configure_subagent_runtime(
        SubAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            run_sub_turn=slow_sub_turn,
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
        runtime = get_subagent_runtime()
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


def test_parallel_delegates_use_distinct_threads() -> None:
    seen_thread_ids: list[str] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        del event

    async def recording_sub_turn(**kwargs: Any) -> SubTurnResult:
        seen_thread_ids.append(kwargs["thread_id"])
        agent = kwargs["agent"]
        await asyncio.sleep(0.02 if agent.id == "worker_a" else 0.01)
        return SubTurnResult(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": f"done-{agent.id}"}],
                }
            ]
        )

    parent = SubAgentSnapshot(id="parent", name="Parent", model="faux/test")
    configure_subagent_runtime(
        SubAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            run_sub_turn=recording_sub_turn,
            sub_agents={
                "worker_a": SubAgentSnapshot(
                    id="worker_a",
                    name="A",
                    model="faux/test",
                ),
                "worker_b": SubAgentSnapshot(
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
    from pi_subagent.roles import agent_snapshot_with_role, role_system_prompt

    assert role_system_prompt("explore") is not None
    assert role_system_prompt("unknown") is None
    worker = SubAgentSnapshot(id="w", name="W", model="faux/test")
    explore = agent_snapshot_with_role(worker, "explore")
    assert explore.system_prompt != worker.system_prompt


def test_resolve_sub_agent_applies_metadata_role() -> None:
    parent = SubAgentSnapshot(id="parent", name="Parent", model="faux/test")
    worker = SubAgentSnapshot(
        id="worker",
        name="Worker",
        model="faux/test",
        system_prompt="original",
        metadata={"subagent_role": "verify"},
    )
    runtime = _runtime(
        parent_agent=parent,
        sub_agents={"worker": worker},
    )
    resolved = resolve_sub_agent("worker", runtime)
    assert resolved is not None
    assert resolved.system_prompt != "original"
    assert "verification" in (resolved.system_prompt or "").lower()
