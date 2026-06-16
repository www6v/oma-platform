"""ContextVar isolation for nested sub-agent turns."""

from __future__ import annotations

import asyncio

import pytest

from oma_adapter.call_agent.runtime import (
    CallAgentRuntime,
    configure_call_agent,
    get_call_agent_runtime,
    reset_call_agent_runtime,
)
from oma_adapter.types import AgentSnapshot


def test_nested_runtime_restores_parent_context() -> None:
    parent = AgentSnapshot(id="parent", name="Parent", model="faux/test")
    parent_token = configure_call_agent(
        CallAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            parent_thread_id="sthr_primary",
        )
    )

    async def nested() -> str:
        child_token = configure_call_agent(
            CallAgentRuntime(
                session_id="sess_1",
                workdir="/tmp",
                parent_agent=parent,
                parent_thread_id="sthr_child",
            )
        )
        try:
            runtime = get_call_agent_runtime()
            assert runtime is not None
            assert runtime.parent_thread_id == "sthr_child"
            return runtime.parent_thread_id
        finally:
            reset_call_agent_runtime(child_token)

    child_id = asyncio.run(nested())
    assert child_id == "sthr_child"
    runtime = get_call_agent_runtime()
    assert runtime is not None
    assert runtime.parent_thread_id == "sthr_primary"
    reset_call_agent_runtime(parent_token)
