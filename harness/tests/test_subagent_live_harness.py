"""Real piPy harness tests: LLM issues call_agent_* and delegation runs."""

from __future__ import annotations

import asyncio
import tempfile
from typing import Any

import pytest

pytest.importorskip("pi_coding_agent")

from pi_ai.providers.faux import (
    faux_assistant_message,
    faux_tool_call,
    register_faux_provider,
    set_faux_responses,
)

from oma_adapter.turn import run_turn_stream
from oma_adapter.types import AgentSnapshot, CallableAgentRef

WORKER_ID = "agt_worker_live"
FAUX_MODEL = "faux/subagent-live"
WORKER_REPLY = "worker-live-ok"
COORD_REPLY = "coord-done"


def _tool_name(worker_id: str) -> str:
    return f"call_agent_{worker_id}"


def test_subagent_real_harness_model_calls_call_agent() -> None:
    """pi session + call_agent extension: faux LLM emits call_agent_* tool_use."""
    tool_name = _tool_name(WORKER_ID)
    registration = register_faux_provider(
        models=[{"id": "subagent-live", "name": "subagent-live"}],
    )
    set_faux_responses(
        [
            faux_assistant_message(
                [faux_tool_call(tool_name, {"message": "run delegated task"})]
            ),
            faux_assistant_message(WORKER_REPLY),
            faux_assistant_message(COORD_REPLY),
        ]
    )

    emitted: list[dict[str, Any]] = []

    async def on_event(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def run() -> None:
        parent = AgentSnapshot(
            id="agt_coord",
            name="Coordinator",
            model=FAUX_MODEL,
            callable_agents=[CallableAgentRef(id=WORKER_ID)],
        )
        worker = AgentSnapshot(
            id=WORKER_ID,
            name="Worker",
            model=FAUX_MODEL,
            system_prompt=f"Always reply exactly: {WORKER_REPLY}",
        )
        with tempfile.TemporaryDirectory() as workdir:
            resp = await run_turn_stream(
                session_id="sess_subagent_live",
                agent=parent,
                sub_agents={WORKER_ID: worker},
                events=[
                    {
                        "type": "user.message",
                        "content": [{"type": "text", "text": "delegate please"}],
                    }
                ],
                workdir=workdir,
                on_event=on_event,
            )
        assert resp.events

    try:
        asyncio.run(run())
    finally:
        registration.dispose()

    types = [ev.get("type") for ev in emitted]
    tool_uses = [
        ev
        for ev in emitted
        if ev.get("type") == "agent.tool_use"
        and ev.get("name") == tool_name
    ]
    thread_created = [
        ev for ev in emitted if ev.get("type") == "session.thread_created"
    ]
    worker_msgs = [
        ev
        for ev in emitted
        if ev.get("type") == "agent.message"
        and ev.get("session_thread_id")
        and _text_of(ev) == WORKER_REPLY
    ]
    coord_msgs = [
        ev
        for ev in emitted
        if ev.get("type") == "agent.message"
        and not ev.get("session_thread_id")
        and _text_of(ev) == COORD_REPLY
    ]
    tool_results = [
        ev
        for ev in emitted
        if ev.get("type") == "agent.tool_result"
        and WORKER_REPLY in _text_of(ev)
    ]

    assert tool_uses, f"missing {tool_name} tool_use; events={types}"
    assert thread_created, "missing session.thread_created"
    assert worker_msgs, "missing worker reply on sub thread"
    assert tool_results, "missing agent.tool_result with worker text"
    assert coord_msgs, "missing coordinator final message"
    assert thread_created[0].get("agent_id") == WORKER_ID
    assert types.index("session.thread_created") > types.index("agent.tool_use")


def _text_of(event: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in event.get("content") or []:
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "\n".join(parts).strip()
