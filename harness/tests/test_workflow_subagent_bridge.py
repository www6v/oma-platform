"""Workflow-style direct sub-turn: user.message must carry session_thread_id."""

from __future__ import annotations

import asyncio
import tempfile
from typing import Any

import pytest

pytest.importorskip("pi_coding_agent")

from pi_ai.providers.faux import (
    faux_assistant_message,
    register_faux_provider,
    set_faux_responses,
)

from oma_adapter.subagent_bridge import _oma_run_sub_turn
from oma_adapter.types import AgentSnapshot
from pi_subagent.types import SubAgentSnapshot

FAUX_MODEL = "faux/workflow-sub-turn"
WORKER_REPLY = "WORKFLOW-SMOKE-OK"


def test_oma_run_sub_turn_emits_agent_message_on_sub_thread() -> None:
    """Direct sub-turn (workflow path) runs LLM and tags sub-thread events."""
    registration = register_faux_provider(
        models=[{"id": "workflow-sub-turn", "name": "workflow-sub-turn"}],
    )
    set_faux_responses([faux_assistant_message(WORKER_REPLY)])

    emitted: list[dict[str, Any]] = []

    async def on_event(event: dict[str, Any]) -> None:
        emitted.append(event)

    parent = SubAgentSnapshot(
        id="agt_coord",
        name="Coordinator",
        model=FAUX_MODEL,
    )
    worker = SubAgentSnapshot(
        id="agt_worker",
        name="workflow:worker-step1",
        model=FAUX_MODEL,
        system_prompt="Reply concisely.",
    )

    async def run() -> None:
        with tempfile.TemporaryDirectory() as workdir:
            result = await _oma_run_sub_turn(
                session_id="sess_workflow_sub",
                tenant_id="default",
                agent=worker,
                message="Reply with exactly WORKFLOW-SMOKE-OK.",
                workdir=workdir,
                model=None,
                aux_model=None,
                environment=None,
                thread_id="sthr_sub_test",
                mcp_proxy_base=None,
                mcp_proxy_api_key=None,
                outbound_proxy_addr=None,
                outbound_proxy_api_key=None,
                sub_agents={},
                parent_agent=parent,
                depth=1,
                on_event=on_event,
            )
        assert result.events

    try:
        asyncio.run(run())
    finally:
        registration.dispose()

    types = [ev.get("type") for ev in emitted]
    sub_msgs = [
        ev
        for ev in emitted
        if ev.get("type") == "agent.message"
        and ev.get("session_thread_id") == "sthr_sub_test"
        and _text_of(ev) == WORKER_REPLY
    ]
    assert "agent.message" in types, f"expected agent.message, got {types}"
    assert sub_msgs, f"missing sub-thread reply; events={types}"


def _text_of(event: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in event.get("content") or []:
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "".join(parts).strip()
