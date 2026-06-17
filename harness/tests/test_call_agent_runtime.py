"""ContextVar isolation for nested sub-agent turns."""

from __future__ import annotations

import asyncio
from typing import Any

from pi_subagent.runtime import (
    SubAgentRuntime,
    configure_subagent_runtime,
    get_subagent_runtime,
    reset_subagent_runtime,
)
from pi_subagent.types import SubAgentSnapshot, SubTurnResult


async def _noop_sub_turn(**kwargs: Any) -> SubTurnResult:
    del kwargs
    return SubTurnResult()


def test_nested_runtime_restores_parent_context() -> None:
    parent = SubAgentSnapshot(id="parent", name="Parent", model="faux/test")
    parent_token = configure_subagent_runtime(
        SubAgentRuntime(
            session_id="sess_1",
            workdir="/tmp",
            parent_agent=parent,
            run_sub_turn=_noop_sub_turn,
            parent_thread_id="sthr_primary",
        )
    )

    async def nested() -> str:
        child_token = configure_subagent_runtime(
            SubAgentRuntime(
                session_id="sess_1",
                workdir="/tmp",
                parent_agent=parent,
                run_sub_turn=_noop_sub_turn,
                parent_thread_id="sthr_child",
            )
        )
        try:
            runtime = get_subagent_runtime()
            assert runtime is not None
            assert runtime.parent_thread_id == "sthr_child"
            return runtime.parent_thread_id
        finally:
            reset_subagent_runtime(child_token)

    child_id = asyncio.run(nested())
    assert child_id == "sthr_child"
    runtime = get_subagent_runtime()
    assert runtime is not None
    assert runtime.parent_thread_id == "sthr_primary"
    reset_subagent_runtime(parent_token)
