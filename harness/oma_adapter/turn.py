"""Stateless harness turn via piPy SDK."""

from __future__ import annotations

import os
from pathlib import Path
from typing import Any, Awaitable, Callable

from oma_adapter.emit import emit_oma_events
from oma_adapter.project import project_oma_events
from oma_adapter.types import AgentSnapshot, TurnResponse

CreateSessionFn = Callable[[Any], Awaitable[Any]]


def _assistant_text_from_session(session: Any) -> str | None:
    getter = getattr(session, "get_last_assistant_text", None)
    if callable(getter):
        text = getter()
        if isinstance(text, str) and text.strip():
            return text.strip()

    legacy = getattr(session, "last_assistant_text", None)
    if callable(legacy):
        text = legacy()
        if isinstance(text, str) and text.strip():
            return text.strip()
    if isinstance(legacy, str) and legacy.strip():
        return legacy.strip()
    return None


def _collect_pi_event(buffer: list[dict[str, Any]], event: Any) -> None:
    if hasattr(event, "type"):
        from pi_coding_agent.modes.json_mode import agent_event_to_dict

        buffer.append(agent_event_to_dict(event))
        return
    if isinstance(event, dict):
        buffer.append(event)


def _make_event_listener(
    buffer: list[dict[str, Any]],
) -> Callable[[Any], None]:
    def listener(event: Any) -> None:
        _collect_pi_event(buffer, event)

    return listener


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

    listener = _make_event_listener(buffer)
    if hasattr(session, "subscribe"):
        session.subscribe(listener)
    elif hasattr(session, "on"):
        session.on("event", listener)

    await session.prompt(prompt)
    if hasattr(session, "wait_for_idle"):
        await session.wait_for_idle()

    oma_events = emit_oma_events(buffer)
    if not oma_events:
        text = _assistant_text_from_session(session)
        if text:
            oma_events = [
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": text}],
                }
            ]

    if not oma_events:
        msg = "harness turn produced no assistant output"
        raise RuntimeError(msg)

    return TurnResponse(events=oma_events)
