"""Stateless harness turn via piPy SDK."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Awaitable, Callable

from oma_adapter.emit import emit_oma_events
from oma_adapter.project import project_oma_events
from oma_adapter.types import AgentSnapshot, TurnResponse

CreateSessionFn = Callable[[Any], Awaitable[Any]]


async def _default_create_session(
    *,
    workdir: str,
    model: str,
    system_prompt: str | None,
) -> Any:
    from pi_coding_agent.sdk import CreateAgentSessionOptions, create_agent_session

    opts = CreateAgentSessionOptions(
        cwd=Path(workdir),
        model=model,
        system_prompt=system_prompt,
        tools=["read", "bash", "write"],
        in_memory=True,
    )
    return await create_agent_session(opts)


async def run_turn(
    *,
    session_id: str,
    agent: AgentSnapshot,
    events: list[dict[str, Any]],
    workdir: str,
    create_session: CreateSessionFn | None = None,
) -> TurnResponse:
    del session_id  # stateless MVP

    prompt = project_oma_events(events)
    if not prompt:
        return TurnResponse(events=[])

    model = agent.model
    if not model.startswith("faux/") and os.environ.get("OMA_FAKE_HARNESS") == "1":
        model = "faux/test"

    if create_session is not None:
        result = await create_session(None)
    else:
        result = await _default_create_session(
            workdir=workdir,
            model=model,
            system_prompt=agent.system_prompt,
        )
    session = result.session

    buffer: list[dict[str, Any]] = []

    def _collect(event: dict[str, Any]) -> None:
        buffer.append(event)

    if hasattr(session, "on"):
        session.on("event", _collect)

    await session.prompt(prompt)

    oma_events = emit_oma_events(buffer)
    if not oma_events:
        text = getattr(session, "last_assistant_text", None)
        if callable(text):
            text = text()
        if text:
            oma_events = [
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": str(text)}],
                }
            ]
        else:
            oma_events = [
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "ok"}],
                }
            ]

    return TurnResponse(events=oma_events)
